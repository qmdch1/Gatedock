package sshclient

import (
	"context"
	"fmt"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"io"
	"net"
	"os"
	"path/filepath"
	"sshdesk/internal/database"
	"sshdesk/internal/model"
	"sshdesk/internal/testssh"
	"testing"
	"time"
)

func TestRealSSHMultiJumpShellAndForward(t *testing.T) {
	signer, path := testssh.Key(t)
	a := testssh.Start(t, signer.PublicKey())
	b := testssh.Start(t, signer.PublicKey())
	c := testssh.Start(t, signer.PublicKey())
	store, e := database.Open(filepath.Join(t.TempDir(), "db"), model.Settings{})
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	e = store.Update(func(s *model.State) error {
		s.Keys = []model.Key{{ID: "k", Name: "test", Path: path}}
		for i, v := range []*testssh.Server{a, b, c} {
			jump := ""
			if i > 0 {
				jump = fmt.Sprint(i - 1)
			}
			s.Hosts = append(s.Hosts, model.Host{ID: fmt.Sprint(i), Name: fmt.Sprint(i), Environment: "LOCAL", Type: "Server", Address: v.Address, Port: v.Port, Username: "test", KeyID: "k", JumpID: jump, Fingerprint: ssh.FingerprintSHA256(v.Signer.PublicKey())})
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	manager := Manager{Store: store}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, e := manager.Connect(ctx, "2")
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	if e = conn.KeepAlive(); e != nil {
		t.Fatal(e)
	}
	session, e := conn.Session()
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close()
	stdout, e := session.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	stdin, e := session.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = session.RequestPty("xterm-256color", 30, 100, ssh.TerminalModes{}); e != nil {
		t.Fatal(e)
	}
	if e = session.Shell(); e != nil {
		t.Fatal(e)
	}
	if e = session.WindowChange(40, 120); e != nil {
		t.Fatal(e)
	}
	select {
	case size := <-c.Resize:
		if size != [2]uint32{120, 40} {
			t.Fatal(size)
		}
	case <-ctx.Done():
		t.Fatal("resize missing")
	}
	buf := make([]byte, len("\x1b[32m테스트 SSH ready\x1b[0m\r\n"))
	if _, e = io.ReadFull(stdout, buf); e != nil {
		t.Fatal(e)
	}
	input := "UTF-8 한글\x03"
	_, _ = io.WriteString(stdin, input)
	buf = make([]byte, len(input))
	if _, e = io.ReadFull(stdout, buf); e != nil || string(buf) != input {
		t.Fatalf("shell echo %q %v", buf, e)
	}
	echo, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer echo.Close()
	go func() {
		remote, e := echo.Accept()
		if e == nil {
			defer remote.Close()
			_, _ = io.Copy(remote, remote)
		}
	}()
	forward, e := conn.Dial(ctx, "tcp", echo.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer forward.Close()
	_ = forward.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = io.WriteString(forward, "jump-forward-ok")
	buf = make([]byte, 15)
	if _, e = io.ReadFull(forward, buf); e != nil || string(buf) != "jump-forward-ok" {
		t.Fatalf("forward %q %v", buf, e)
	}
}
func TestHostKeyVerification(t *testing.T) {
	signer, _ := testssh.Key(t)
	other, _ := testssh.Key(t)
	address := "127.0.0.1:2222"
	remote := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2222}
	pin, e := HostKeyCallback("", ssh.FingerprintSHA256(signer.PublicKey()))
	if e != nil {
		t.Fatal(e)
	}
	if pin(address, remote, other.PublicKey()) == nil {
		t.Fatal("mismatch allowed")
	}
	path := filepath.Join(t.TempDir(), "known_hosts")
	if e = os.WriteFile(path, []byte(knownhosts.Line([]string{address}, signer.PublicKey())+"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	callback, e := HostKeyCallback(path, "")
	if e != nil {
		t.Fatal(e)
	}
	if e = callback(address, remote, signer.PublicKey()); e != nil {
		t.Fatal(e)
	}
	if callback(address, remote, other.PublicKey()) == nil {
		t.Fatal("changed key allowed")
	}
	if callback("127.0.0.1:2223", &net.TCPAddr{IP: remote.IP, Port: 2223}, signer.PublicKey()) == nil {
		t.Fatal("unknown key allowed")
	}
}
