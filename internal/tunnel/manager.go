package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"sshdesk/internal/model"
	"sshdesk/internal/sshclient"
	"sync"
	"time"
)

type Status struct {
	ConfigChanged bool   `json:"config_changed,omitempty"`
	State         string `json:"state"`
	LastError     string `json:"last_error"`
	StartedAt     string `json:"started_at"`
	Connections   int    `json:"connections"`
}
type entry struct {
	config   model.Tunnel
	status   Status
	cancel   context.CancelFunc
	listener net.Listener
	conn     sshclient.Connection
	sockets  map[net.Conn]bool
	done     chan struct{}
	stopped  bool
}
type Manager struct {
	containerNetwork  bool
	mu                sync.Mutex
	entries           map[string]*entry
	Connector         sshclient.Connector
	KeepAliveInterval time.Duration
	closed            bool
}

func New(c sshclient.Connector) *Manager {
	return &Manager{entries: map[string]*entry{}, Connector: c, KeepAliveInterval: 30 * time.Second}
}

// NewContainer allows Docker's bridge to reach forwarded ports. The host must
// publish these ports on 127.0.0.1, never on an external interface.
func NewContainer(c sshclient.Connector) *Manager {
	m := New(c)
	m.containerNetwork = true
	return m
}
func (m *Manager) Statuses() map[string]Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]Status{}
	for id, e := range m.entries {
		out[id] = e.status
	}
	return out
}
func (m *Manager) Active(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[id]
	return e != nil && (e.status.State == "running" || e.status.State == "connecting" || e.status.State == "reconnecting")
}

func (m *Manager) ConfigChanged(t model.Tunnel) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[t.ID]
	return e != nil && (e.status.State == "running" || e.status.State == "connecting" || e.status.State == "reconnecting") && e.config != t
}
func (m *Manager) Start(ctx context.Context, t model.Tunnel) error {
	if e := model.ValidateTunnel(t); e != nil {
		return e
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errors.New("agent is shutting down")
	}
	if old := m.entries[t.ID]; old != nil && old.status.State != "stopped" && old.status.State != "error" {
		state := old.status.State
		m.mu.Unlock()
		if state == "running" {
			return nil
		}
		return errors.New("터널 연결이 진행 중입니다")
	}
	bindHost := t.LocalHost
	if m.containerNetwork {
		bindHost = "0.0.0.0"
	}
	listener, e := net.Listen("tcp4", net.JoinHostPort(bindHost, fmt.Sprint(t.LocalPort)))
	if e != nil {
		m.entries[t.ID] = &entry{status: Status{State: "error", LastError: "Local Port 사용 중 또는 bind 실패: " + e.Error()}}
		m.mu.Unlock()
		return e
	}
	life, cancel := context.WithCancel(context.Background())
	v := &entry{config: t, status: Status{State: "connecting"}, cancel: cancel, listener: listener, sockets: map[net.Conn]bool{}, done: make(chan struct{})}
	m.entries[t.ID] = v
	m.mu.Unlock()
	// Stop and application shutdown cancel a connection attempt as well.
	attempt, attemptCancel := context.WithTimeout(life, 45*time.Second)
	stop := context.AfterFunc(ctx, attemptCancel)
	conn, e := m.Connector.Connect(attempt, t.HostID)
	stop()
	attemptCancel()
	if e != nil {
		if life.Err() != nil {
			m.finish(v, nil)
		} else {
			m.finish(v, e)
		}
		close(v.done)
		return e
	}
	m.mu.Lock()
	if life.Err() != nil {
		m.mu.Unlock()
		conn.Close()
		m.finish(v, nil)
		close(v.done)
		return context.Canceled
	}
	v.conn = conn
	v.status = Status{State: "running", StartedAt: time.Now().UTC().Format(time.RFC3339)}
	m.mu.Unlock()
	go m.accept(life, v)
	go m.monitor(life, v)
	return nil
}
func (m *Manager) finish(v *entry, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v.cancel != nil {
		v.cancel()
	}
	if v.listener != nil {
		v.listener.Close()
	}
	if v.conn != nil {
		v.conn.Close()
		v.conn = nil
	}
	for c := range v.sockets {
		c.Close()
	}
	v.status.State = "stopped"
	if err == nil {
		v.stopped = true
	}
	if err != nil && !v.stopped {
		v.status.State = "error"
		v.status.LastError = err.Error()
	}
}
func (m *Manager) Stop(id string) {
	m.mu.Lock()
	v := m.entries[id]
	m.mu.Unlock()
	if v != nil {
		m.finish(v, nil)
		if v.done != nil {
			<-v.done
		}
	}
}
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	ids := []string{}
	for id := range m.entries {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Stop(id)
	}
}
func (m *Manager) accept(ctx context.Context, v *entry) {
	for {
		local, e := v.listener.Accept()
		if e != nil {
			return
		}
		m.mu.Lock()
		conn := v.conn
		if ctx.Err() != nil || v.status.State != "running" || conn == nil || len(v.sockets) >= 128 {
			m.mu.Unlock()
			local.Close()
			continue
		}
		v.sockets[local] = true
		v.status.Connections++
		m.mu.Unlock()
		go m.forward(ctx, v, conn, local)
	}
}
func (m *Manager) forward(ctx context.Context, v *entry, conn sshclient.Connection, local net.Conn) {
	defer func() { local.Close(); m.mu.Lock(); delete(v.sockets, local); v.status.Connections--; m.mu.Unlock() }()
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	remote, e := conn.Dial(dialCtx, "tcp", net.JoinHostPort(v.config.RemoteHost, fmt.Sprint(v.config.RemotePort)))
	cancel()
	if e != nil {
		m.mu.Lock()
		v.status.LastError = "Remote 연결 실패: " + e.Error()
		m.mu.Unlock()
		return
	}
	defer remote.Close()
	done := make(chan struct{}, 1)
	go func() {
		_, _ = io.Copy(remote, local)
		if cw, ok := remote.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		done <- struct{}{}
	}()
	_, _ = io.Copy(local, remote)
	local.Close()
	remote.Close()
	<-done
}
func (m *Manager) monitor(ctx context.Context, v *entry) {
	defer close(v.done)
	interval := m.KeepAliveInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	misses := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		m.mu.Lock()
		conn := v.conn
		m.mu.Unlock()
		if conn == nil {
			return
		}
		result := make(chan error, 1)
		go func() { result <- conn.KeepAlive() }()
		var err error
		select {
		case <-ctx.Done():
			return
		case err = <-result:
		case <-time.After(10 * time.Second):
			err = errors.New("SSH keepalive timeout")
		}
		if err == nil {
			misses = 0
			continue
		}
		misses++
		if misses < 3 {
			continue
		}
		m.mu.Lock()
		if ctx.Err() != nil {
			m.mu.Unlock()
			return
		}
		v.status.LastError = err.Error()
		v.status.State = "reconnecting"
		v.conn = nil
		for c := range v.sockets {
			c.Close()
		}
		m.mu.Unlock()
		conn.Close()
		if !v.config.AutoReconnect {
			m.finish(v, err)
			return
		}
		for backoff := time.Second; ; {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff + time.Duration(rand.IntN(500))*time.Millisecond):
			}
			attempt, cancel := context.WithTimeout(ctx, 45*time.Second)
			next, e := m.Connector.Connect(attempt, v.config.HostID)
			cancel()
			if e == nil {
				m.mu.Lock()
				if ctx.Err() != nil {
					m.mu.Unlock()
					next.Close()
					return
				}
				v.conn = next
				v.status.State = "running"
				v.status.StartedAt = time.Now().UTC().Format(time.RFC3339)
				m.mu.Unlock()
				misses = 0
				break
			}
			m.mu.Lock()
			v.status.LastError = e.Error()
			m.mu.Unlock()
			if backoff < 30*time.Second {
				backoff *= 2
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
			}
		}
	}
}

// Ready checks the entire forwarding route, not just the local listener.
func (m *Manager) Ready(ctx context.Context, id string) error {
	m.mu.Lock()
	v := m.entries[id]
	if v == nil || v.status.State != "running" || v.conn == nil {
		m.mu.Unlock()
		return errors.New("터널이 실행 중이 아닙니다")
	}
	conn := v.conn
	address := net.JoinHostPort(v.config.RemoteHost, fmt.Sprint(v.config.RemotePort))
	m.mu.Unlock()
	c, e := conn.Dial(ctx, "tcp", address)
	if e != nil {
		m.mu.Lock()
		v.status.LastError = "서비스 연결 실패: " + e.Error()
		m.mu.Unlock()
		return e
	}
	// A peer can close immediately after accepting a probe. Dial success already
	// proves TCP reachability; a close race is not a connection failure.
	_ = c.Close()
	return nil
}
