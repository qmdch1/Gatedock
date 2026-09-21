package sshclient

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"net"
	"os"
	"sshdesk/internal/database"
	"sshdesk/internal/key"
	"sshdesk/internal/model"
	"sshdesk/internal/platform"
	"sync"
	"time"
)

type Connection interface {
	Dial(context.Context, string, string) (net.Conn, error)
	Session() (*ssh.Session, error)
	KeepAlive() error
	Close() error
}
type Connector interface {
	Connect(context.Context, string) (Connection, error)
}
type Manager struct {
	Store       *database.Store
	Passphrases key.PassphraseProvider
	Timeout     time.Duration
}
type connection struct {
	clients []*ssh.Client
	once    sync.Once
}

func (c *connection) Close() error {
	c.once.Do(func() {
		for i := len(c.clients) - 1; i >= 0; i-- {
			c.clients[i].Close()
		}
	})
	return nil
}
func (c *connection) Session() (*ssh.Session, error) { return c.clients[len(c.clients)-1].NewSession() }
func (c *connection) Dial(ctx context.Context, n, a string) (net.Conn, error) {
	return c.clients[len(c.clients)-1].DialContext(ctx, n, a)
}
func (c *connection) KeepAlive() error {
	for _, client := range c.clients {
		_, _, e := client.SendRequest("keepalive@openssh.com", true, nil)
		if e != nil {
			return e
		}
	}
	return nil
}
func (m *Manager) Connect(ctx context.Context, id string) (Connection, error) {
	state, e := m.Store.Snapshot()
	if e != nil {
		return nil, e
	}
	chain, e := state.Chain(id)
	if e != nil || len(chain) == 0 {
		return nil, errors.New("유효한 SSH 경로가 없습니다")
	}
	target := chain[len(chain)-1]
	conn, e := m.connect(ctx, state, chain)
	m.Store.Record(target, "ssh", e)
	return conn, e
}
func (m *Manager) connect(ctx context.Context, state model.State, chain []model.Host) (Connection, error) {
	timeout := m.Timeout
	if timeout == 0 {
		timeout = 12 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout*time.Duration(len(chain)))
	defer cancel()
	out := &connection{}
	for _, h := range chain {
		k, e := state.Key(h.KeyID)
		if e != nil {
			out.Close()
			return nil, e
		}
		signer, e := key.Signer(k.Path, m.Passphrases)
		if e != nil {
			out.Close()
			return nil, e
		}
		callback, e := HostKeyCallback(state.Settings.KnownHosts, h.Fingerprint)
		if e != nil {
			out.Close()
			return nil, e
		}
		address := net.JoinHostPort(h.Address, fmt.Sprint(h.Port))
		var raw net.Conn
		hopCtx, hopCancel := context.WithTimeout(ctx, timeout)
		if len(out.clients) == 0 {
			raw, e = (&net.Dialer{}).DialContext(hopCtx, "tcp", address)
		} else {
			raw, e = out.clients[len(out.clients)-1].DialContext(hopCtx, "tcp", address)
		}
		if e != nil {
			hopCancel()
			out.Close()
			return nil, fmt.Errorf("%s: SSH TCP 연결 실패: %w", h.Name, e)
		}
		deadline, _ := hopCtx.Deadline()
		_ = raw.SetDeadline(deadline)
		stop := context.AfterFunc(hopCtx, func() { raw.Close() })
		cc, ch, req, e := ssh.NewClientConn(raw, address, &ssh.ClientConfig{User: h.Username, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: callback, Timeout: timeout})
		stopped := stop()
		hopCancel()
		if e != nil || !stopped {
			raw.Close()
			out.Close()
			if e == nil {
				e = context.DeadlineExceeded
			}
			return nil, fmt.Errorf("%s: %w", h.Name, e)
		}
		_ = raw.SetDeadline(time.Time{})
		out.clients = append(out.clients, ssh.NewClient(cc, ch, req))
	}
	return out, nil
}
func HostKeyCallback(path, pin string) (ssh.HostKeyCallback, error) {
	if pin != "" {
		return func(host string, remote net.Addr, k ssh.PublicKey) error {
			actual := ssh.FingerprintSHA256(k)
			if subtle.ConstantTimeCompare([]byte(pin), []byte(actual)) != 1 {
				return fmt.Errorf("SSH 서버 지문 불일치 (%s). 연결을 차단했습니다", host)
			}
			return nil
		}, nil
	}
	p, e := platform.Path(path)
	if e != nil {
		return nil, e
	}
	if _, e = os.Stat(p); e != nil {
		return nil, errors.New("known_hosts 파일이 없습니다. Settings에서 지정하거나 관리자에게 확인한 SHA256 서버 지문을 Host에 입력하세요")
	}
	verify, e := knownhosts.New(p)
	if e != nil {
		return nil, errors.New("known_hosts 파일을 해석할 수 없습니다")
	}
	return func(host string, remote net.Addr, k ssh.PublicKey) error {
		if e := verify(host, remote, k); e != nil {
			var ke *knownhosts.KeyError
			if errors.As(e, &ke) && len(ke.Want) == 0 {
				return fmt.Errorf("미등록 SSH 서버 %s, 지문 %s. 관리자에게 별도로 확인한 후 Host에 지문을 등록하세요", host, ssh.FingerprintSHA256(k))
			}
			return fmt.Errorf("SSH 서버 키 검증 실패 (%s). known_hosts를 확인하세요", host)
		}
		return nil
	}, nil
}
