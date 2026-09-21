package web

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/coder/websocket"
	"golang.org/x/crypto/ssh"
	"io"
	"net/http"
	"sshdesk/internal/database"
	"sync"
	"time"
)

type terminalInput struct {
	Type  string `json:"type"`
	Data  string `json:"data"`
	Cols  int    `json:"cols"`
	Rows  int    `json:"rows"`
	Token string `json:"token"`
}

func (s *Server) terminal(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.Origin {
		http.Error(w, "Invalid WebSocket origin", 403)
		return
	}
	ws, e := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if e != nil {
		return
	}
	defer ws.CloseNow()
	ws.SetReadLimit(65536)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	s.mu.Lock()
	if s.closed || len(s.terminals) >= 16 {
		s.mu.Unlock()
		_ = ws.Close(websocket.StatusPolicyViolation, "terminal capacity reached")
		return
	}
	id := database.ID()
	s.terminals[id] = cancel
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.terminals, id); s.mu.Unlock() }()
	var writeMu sync.Mutex
	send := func(v any) error {
		b, _ := json.Marshal(v)
		writeMu.Lock()
		defer writeMu.Unlock()
		c, done := context.WithTimeout(ctx, 10*time.Second)
		defer done()
		return ws.Write(c, websocket.MessageText, b)
	}
	authCtx, authCancel := context.WithTimeout(ctx, 5*time.Second)
	_, raw, e := ws.Read(authCtx)
	authCancel()
	var initial terminalInput
	if e != nil || json.Unmarshal(raw, &initial) != nil || initial.Type != "auth" || subtle.ConstantTimeCompare([]byte(initial.Token), []byte(s.token)) != 1 {
		_ = ws.Close(websocket.StatusPolicyViolation, "authentication required")
		return
	}
	connectCtx, connectCancel := context.WithTimeout(ctx, 45*time.Second)
	conn, e := s.SSH.Connect(connectCtx, r.PathValue("id"))
	connectCancel()
	if e != nil {
		_ = send(map[string]string{"type": "error", "message": e.Error()})
		return
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	session, e := conn.Session()
	if e != nil {
		_ = send(map[string]string{"type": "error", "message": e.Error()})
		return
	}
	defer session.Close()
	if initial.Rows < 2 || initial.Rows > 500 {
		initial.Rows = 30
	}
	if initial.Cols < 2 || initial.Cols > 500 {
		initial.Cols = 100
	}
	if e = session.RequestPty("xterm-256color", initial.Rows, initial.Cols, ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}); e != nil {
		_ = send(map[string]string{"type": "error", "message": e.Error()})
		return
	}
	stdin, e := session.StdinPipe()
	if e != nil {
		return
	}
	stdout, e := session.StdoutPipe()
	if e != nil {
		return
	}
	stderr, e := session.StderrPipe()
	if e != nil {
		return
	}
	if e = session.Shell(); e != nil {
		_ = send(map[string]string{"type": "error", "message": e.Error()})
		return
	}
	_ = send(map[string]string{"type": "status", "status": "connected", "at": time.Now().UTC().Format(time.RFC3339)})
	var output sync.WaitGroup
	output.Add(2)
	copyOutput := func(src io.Reader) {
		defer output.Done()
		buf := make([]byte, 16384)
		for {
			n, e := src.Read(buf)
			if n > 0 {
				writeMu.Lock()
				writeCtx, done := context.WithTimeout(ctx, 10*time.Second)
				err := ws.Write(writeCtx, websocket.MessageBinary, buf[:n])
				done()
				writeMu.Unlock()
				if err != nil {
					cancel()
					return
				}
			}
			if e != nil {
				return
			}
		}
	}
	go copyOutput(stdout)
	go copyOutput(stderr)
	go func() {
		err := session.Wait()
		output.Wait()
		message := "Session ended"
		if err != nil {
			message = err.Error()
		}
		_ = send(map[string]string{"type": "exit", "message": message})
		cancel()
	}()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				result := make(chan error, 1)
				go func() { result <- conn.KeepAlive() }()
				select {
				case <-ctx.Done():
					return
				case err := <-result:
					if err != nil {
						cancel()
						return
					}
				case <-time.After(10 * time.Second):
					cancel()
					return
				}
			}
		}
	}()
	for {
		_, raw, e := ws.Read(ctx)
		if e != nil {
			return
		}
		var in terminalInput
		if json.Unmarshal(raw, &in) != nil {
			return
		}
		switch in.Type {
		case "input":
			if _, e = io.WriteString(stdin, in.Data); e != nil {
				return
			}
		case "resize":
			if in.Cols >= 2 && in.Cols <= 500 && in.Rows >= 2 && in.Rows <= 500 {
				if e = session.WindowChange(in.Rows, in.Cols); e != nil {
					return
				}
			}
		case "disconnect":
			return
		default:
			_ = send(map[string]string{"type": "error", "message": errors.New("unknown terminal message").Error()})
		}
	}
}
