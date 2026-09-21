package tunnel

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/crypto/ssh"
	"io"
	"net"
	"sshdesk/internal/model"
	"sshdesk/internal/sshclient"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeConn struct {
	closed  atomic.Bool
	fail    atomic.Bool
	mu      sync.Mutex
	sockets []net.Conn
}

func (c *fakeConn) Dial(ctx context.Context, n, a string) (net.Conn, error) {
	v, e := (&net.Dialer{}).DialContext(ctx, n, a)
	if e == nil {
		c.mu.Lock()
		if c.closed.Load() {
			v.Close()
			c.mu.Unlock()
			return nil, errors.New("closed")
		}
		c.sockets = append(c.sockets, v)
		c.mu.Unlock()
	}
	return v, e
}
func (c *fakeConn) Session() (*ssh.Session, error) { return nil, errors.New("unused") }
func (c *fakeConn) KeepAlive() error {
	if c.closed.Load() || c.fail.Load() {
		return errors.New("lost connection")
	}
	return nil
}
func (c *fakeConn) Close() error {
	c.closed.Store(true)
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, v := range c.sockets {
		v.Close()
	}
	return nil
}

type fakeConnector struct {
	count  atomic.Int32
	mu     sync.Mutex
	latest *fakeConn
	err    error
	block  bool
}

func (f *fakeConnector) Connect(ctx context.Context, id string) (sshclient.Connection, error) {
	f.count.Add(1)
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if f.err != nil {
		return nil, f.err
	}
	c := &fakeConn{}
	f.mu.Lock()
	f.latest = c
	f.mu.Unlock()
	return c, nil
}
func (f *fakeConnector) current() *fakeConn { f.mu.Lock(); defer f.mu.Unlock(); return f.latest }
func freePort(t *testing.T) int {
	t.Helper()
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	p := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return p
}
func tunnelFixture(t *testing.T) model.Tunnel {
	return model.Tunnel{ID: "t", Name: "API", LocalHost: "127.0.0.1", LocalPort: freePort(t), RemoteHost: "127.0.0.1", RemotePort: 80, HostID: "h", AutoReconnect: true}
}
func waitFor(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition timed out")
}
func TestOccupiedLocalPort(t *testing.T) {
	ln, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	f := &fakeConnector{}
	m := New(f)
	defer m.Close()
	v := tunnelFixture(t)
	v.LocalPort = ln.Addr().(*net.TCPAddr).Port
	if m.Start(context.Background(), v) == nil {
		t.Fatal("occupied port accepted")
	}
	if f.count.Load() != 0 || m.Statuses()[v.ID].State != "error" {
		t.Fatal("connected before reserving local port")
	}
}
func TestStartFailureReleasesPort(t *testing.T) {
	m := New(&fakeConnector{err: errors.New("authentication failed")})
	defer m.Close()
	v := tunnelFixture(t)
	if m.Start(context.Background(), v) == nil {
		t.Fatal("expected failure")
	}
	ln, e := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", fmtPort(v.LocalPort)))
	if e != nil {
		t.Fatal("port not released", e)
	}
	ln.Close()
}
func fmtPort(p int) string { return fmt.Sprint(p) }
func TestForwardReconnectStop(t *testing.T) {
	echo, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer echo.Close()
	go func() {
		for {
			c, e := echo.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	v := tunnelFixture(t)
	v.RemotePort = echo.Addr().(*net.TCPAddr).Port
	f := &fakeConnector{}
	m := New(f)
	m.KeepAliveInterval = 10 * time.Millisecond
	defer m.Close()
	if e = m.Start(context.Background(), v); e != nil {
		t.Fatal(e)
	}
	if e = m.Start(context.Background(), v); e != nil {
		t.Fatal(e)
	}
	if f.count.Load() != 1 {
		t.Fatal("duplicate connection")
	}
	if e = m.Ready(context.Background(), v.ID); e != nil {
		t.Fatal(e)
	}
	c, e := net.Dial("tcp", net.JoinHostPort("127.0.0.1", fmtPort(v.LocalPort)))
	if e != nil {
		t.Fatal(e)
	}
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = io.WriteString(c, "hello")
	buf := make([]byte, 5)
	if _, e = io.ReadFull(c, buf); e != nil || string(buf) != "hello" {
		t.Fatalf("forward failed %q %v", buf, e)
	}
	c.Close()
	f.current().fail.Store(true)
	waitFor(t, func() bool { return f.count.Load() >= 2 && m.Statuses()[v.ID].State == "running" })
	m.Stop(v.ID)
	if m.Statuses()[v.ID].State != "stopped" {
		t.Fatal("stop failed")
	}
	if _, e = net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", fmtPort(v.LocalPort)), time.Second); e == nil {
		t.Fatal("listener still open")
	}
}
func TestStopDuringConnect(t *testing.T) {
	f := &fakeConnector{block: true}
	m := New(f)
	defer m.Close()
	v := tunnelFixture(t)
	done := make(chan error, 1)
	go func() { done <- m.Start(context.Background(), v) }()
	waitFor(t, func() bool { return f.count.Load() > 0 })
	m.Stop(v.ID)
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("expected canceled connect")
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel connect")
	}
	if m.Statuses()[v.ID].State != "stopped" {
		t.Fatal("canceled connect must stay stopped")
	}
}
func TestNoReconnectWhenDisabled(t *testing.T) {
	f := &fakeConnector{}
	m := New(f)
	m.KeepAliveInterval = 5 * time.Millisecond
	defer m.Close()
	v := tunnelFixture(t)
	v.AutoReconnect = false
	if e := m.Start(context.Background(), v); e != nil {
		t.Fatal(e)
	}
	f.current().fail.Store(true)
	waitFor(t, func() bool { return m.Statuses()[v.ID].State == "error" })
	if f.count.Load() != 1 {
		t.Fatal("unexpected reconnect")
	}
}
