// Package testssh provides isolated SSH fixtures. It never contacts company infrastructure.
package testssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"golang.org/x/crypto/ssh"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type Server struct {
	listener    net.Listener
	Signer      ssh.Signer
	Address     string
	Port        int
	mu          sync.Mutex
	connections map[net.Conn]bool
	closed      bool
	Resize      chan [2]uint32
}

func Key(t testing.TB) (ssh.Signer, string) {
	t.Helper()
	_, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	signer, e := ssh.NewSignerFromKey(key)
	if e != nil {
		t.Fatal(e)
	}
	block, e := ssh.MarshalPrivateKey(key, "")
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if e = os.WriteFile(path, pem.EncodeToMemory(block), 0600); e != nil {
		t.Fatal(e)
	}
	return signer, path
}
func Start(t testing.TB, clientKey ssh.PublicKey) *Server {
	t.Helper()
	signer, _ := Key(t)
	ln, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	s := &Server{listener: ln, Signer: signer, Address: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, connections: map[net.Conn]bool{}, Resize: make(chan [2]uint32, 10)}
	config := &ssh.ServerConfig{PublicKeyCallback: func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if string(key.Marshal()) != string(clientKey.Marshal()) {
			return nil, fmt.Errorf("invalid test key")
		}
		return nil, nil
	}}
	config.AddHostKey(signer)
	go func() {
		for {
			raw, e := ln.Accept()
			if e != nil {
				return
			}
			s.mu.Lock()
			if s.closed {
				s.mu.Unlock()
				raw.Close()
				return
			}
			s.connections[raw] = true
			s.mu.Unlock()
			go s.serve(raw, config)
		}
	}()
	t.Cleanup(s.Close)
	return s
}
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.listener.Close()
	for c := range s.connections {
		c.Close()
	}
}
func (s *Server) DropConnections() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.connections {
		c.Close()
	}
}
func (s *Server) serve(raw net.Conn, config *ssh.ServerConfig) {
	defer func() { raw.Close(); s.mu.Lock(); delete(s.connections, raw); s.mu.Unlock() }()
	conn, channels, requests, e := ssh.NewServerConn(raw, config)
	if e != nil {
		return
	}
	defer conn.Close()
	go func() {
		for r := range requests {
			_ = r.Reply(r.Type == "keepalive@openssh.com", nil)
		}
	}()
	for channel := range channels {
		switch channel.ChannelType() {
		case "direct-tcpip":
			go s.forward(channel)
		case "session":
			go s.session(channel)
		default:
			_ = channel.Reject(ssh.UnknownChannelType, "unsupported")
		}
	}
}
func (s *Server) forward(n ssh.NewChannel) {
	var payload struct {
		Host       string
		Port       uint32
		Origin     string
		OriginPort uint32
	}
	if ssh.Unmarshal(n.ExtraData(), &payload) != nil || payload.Host != "127.0.0.1" {
		_ = n.Reject(ssh.Prohibited, "only loopback tests")
		return
	}
	remote, e := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", payload.Port))
	if e != nil {
		_ = n.Reject(ssh.ConnectionFailed, e.Error())
		return
	}
	defer remote.Close()
	channel, requests, e := n.Accept()
	if e != nil {
		return
	}
	defer channel.Close()
	go ssh.DiscardRequests(requests)
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(remote, channel)
		if tcp, ok := remote.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		close(done)
	}()
	_, _ = io.Copy(channel, remote)
	channel.Close()
	remote.Close()
	<-done
}
func (s *Server) session(n ssh.NewChannel) {
	ch, requests, e := n.Accept()
	if e != nil {
		return
	}
	defer ch.Close()
	for r := range requests {
		switch r.Type {
		case "pty-req":
			_ = r.Reply(true, nil)
		case "window-change":
			var size struct{ Cols, Rows, Width, Height uint32 }
			_ = ssh.Unmarshal(r.Payload, &size)
			select {
			case s.Resize <- [2]uint32{size.Cols, size.Rows}:
			default:
			}
		case "shell":
			_ = r.Reply(true, nil)
			go func() {
				_, _ = io.WriteString(ch, "\x1b[32m테스트 SSH ready\x1b[0m\r\n")
				_, _ = io.WriteString(ch.Stderr(), "stderr-ready\r\n")
				buf := make([]byte, 4096)
				for {
					n, e := ch.Read(buf)
					if n > 0 {
						_, _ = ch.Write(buf[:n])
						for _, b := range buf[:n] {
							if b == 4 {
								_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
								ch.Close()
								return
							}
						}
					}
					if e != nil {
						return
					}
				}
			}()
		default:
			_ = r.Reply(false, nil)
		}
	}
}
