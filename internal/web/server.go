package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"sshdesk/internal/database"
	"sshdesk/internal/key"
	"sshdesk/internal/model"
	"sshdesk/internal/platform"
	"sshdesk/internal/sshclient"
	"sshdesk/internal/sshconfig"
	"sshdesk/internal/tunnel"
	assets "sshdesk/web"
	"strings"
	"sync"
	"time"
)

type Server struct {
	StartupImport *sshconfig.StartupResult
	OpenURL       func(string) error
	Store         *database.Store
	SSH           sshclient.Connector
	Tunnels       *tunnel.Manager
	Origin        string
	token         string
	templates     *template.Template
	mu            sync.Mutex
	terminals     map[string]context.CancelFunc
	closed        bool
	ops           sync.Mutex
}

func New(store *database.Store, ssh sshclient.Connector, tm *tunnel.Manager, origin string) (*Server, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return nil, e
	}
	t, e := template.ParseFS(assets.FS, "templates/*.html")
	if e != nil {
		return nil, e
	}
	return &Server{OpenURL: platform.OpenBrowser, Store: store, SSH: ssh, Tunnels: tm, Origin: origin, token: hex.EncodeToString(b), templates: t, terminals: map[string]context.CancelFunc{}}, nil
}
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	for _, cancel := range s.terminals {
		cancel()
	}
	s.mu.Unlock()
	s.Tunnels.Close()
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(assets.FS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]string{"status": "ok", "app": "SSHDesk"})
	})
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		state, e := s.Store.Snapshot()
		if e != nil {
			fail(w, e)
			return
		}
		reply(w, map[string]any{"data": state, "tunnel_status": s.Tunnels.Statuses(), "startup_import": s.StartupImport})
	})
	mux.HandleFunc("POST /api/{kind}", s.save)
	mux.HandleFunc("DELETE /api/{kind}/{id}", s.delete)
	mux.HandleFunc("POST /api/tunnels/{id}/{action}", s.tunnelAction)
	mux.HandleFunc("POST /api/hosts/{id}/check", s.checkHost)
	mux.HandleFunc("POST /api/keys/{id}/check", s.checkKey)
	mux.HandleFunc("POST /api/services/{id}/open", s.openService)
	mux.HandleFunc("POST /api/import/preview", s.preview)
	mux.HandleFunc("POST /api/import/apply", s.importConfig)
	mux.HandleFunc("POST /api/backup/export", s.exportBackup)
	mux.HandleFunc("POST /api/backup/{action}", s.importBackup)
	mux.HandleFunc("GET /ws/terminal/{id}", s.terminal)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = s.templates.ExecuteTemplate(w, "index.html", map[string]string{"Token": s.token})
	})
	return s.security(mux)
}
func (s *Server) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; font-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if r.Host != strings.TrimPrefix(s.Origin, "http://") {
			http.Error(w, "Invalid Host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != s.Origin {
			http.Error(w, "Invalid Origin", http.StatusForbidden)
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
			http.Error(w, "Cross-site request blocked", http.StatusForbidden)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.token)) != 1 {
				http.Error(w, "Invalid CSRF token", http.StatusForbidden)
				return
			}
			if r.Method == "POST" && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				http.Error(w, "JSON required", 415)
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
		next.ServeHTTP(w, r)
	})
}
func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, e error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": e.Error()})
}
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return errors.New("only one JSON object allowed")
	}
	return nil
}
func (s *Server) save(w http.ResponseWriter, r *http.Request) {
	s.ops.Lock()
	defer s.ops.Unlock()
	kind := r.PathValue("kind")
	e := s.Store.Update(func(state *model.State) error {
		var e error
		switch kind {
		case "hosts":
			var v model.Host
			if e = decode(r, &v); e != nil {
				return e
			}
			old := v.ID
			if v.ID == "" {
				v.ID = database.ID()
			}
			state.Hosts, e = database.Replace(state.Hosts, old, v, func(v model.Host) string { return v.ID })
		case "keys":
			var v model.Key
			if e = decode(r, &v); e != nil {
				return e
			}
			p, e := platform.Path(v.Path)
			if e != nil {
				return e
			}
			v.Path = p
			if e = key.Validate(v.Path); e != nil {
				return e
			}
			old := v.ID
			if v.ID == "" {
				v.ID = database.ID()
			}
			state.Keys, e = database.Replace(state.Keys, old, v, func(v model.Key) string { return v.ID })
			return e
		case "tunnels":
			var v model.Tunnel
			if e = decode(r, &v); e != nil {
				return e
			}
			if s.Tunnels.Active(v.ID) {
				return errors.New("편집하기 전에 터널을 Stop하세요")
			}
			old := v.ID
			if v.ID == "" {
				v.ID = database.ID()
			}
			state.Tunnels, e = database.Replace(state.Tunnels, old, v, func(v model.Tunnel) string { return v.ID })
		case "services":
			var v model.Service
			if e = decode(r, &v); e != nil {
				return e
			}
			old := v.ID
			if v.ID == "" {
				v.ID = database.ID()
			}
			state.Services, e = database.Replace(state.Services, old, v, func(v model.Service) string { return v.ID })
		case "settings":
			var v model.Settings
			if e = decode(r, &v); e != nil {
				return e
			}
			if v.KnownHosts, e = platform.Path(v.KnownHosts); e != nil {
				return e
			}
			if v.SSHConfig, e = platform.Path(v.SSHConfig); e != nil {
				return e
			}
			state.Settings = v
		default:
			return errors.New("unknown collection")
		}
		return e
	})
	if e != nil {
		fail(w, e)
		return
	}
	reply(w, map[string]bool{"ok": true})
}
func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	s.ops.Lock()
	defer s.ops.Unlock()
	id := r.PathValue("id")
	e := s.Store.Update(func(state *model.State) error {
		var e error
		switch r.PathValue("kind") {
		case "hosts":
			state.Hosts, e = database.Delete(state.Hosts, id, func(v model.Host) string { return v.ID })
		case "keys":
			state.Keys, e = database.Delete(state.Keys, id, func(v model.Key) string { return v.ID })
		case "tunnels":
			if s.Tunnels.Active(id) {
				return errors.New("삭제하기 전에 터널을 Stop하세요")
			}
			state.Tunnels, e = database.Delete(state.Tunnels, id, func(v model.Tunnel) string { return v.ID })
		case "services":
			state.Services, e = database.Delete(state.Services, id, func(v model.Service) string { return v.ID })
		default:
			return errors.New("unknown collection")
		}
		return e
	})
	if e != nil {
		fail(w, e)
		return
	}
	reply(w, map[string]bool{"ok": true})
}
func (s *Server) tunnelAction(w http.ResponseWriter, r *http.Request) {
	// A stop request must be able to cancel a slow SSH handshake immediately.
	if r.PathValue("action") == "stop" {
		s.Tunnels.Stop(r.PathValue("id"))
		reply(w, s.Tunnels.Statuses())
		return
	}
	s.ops.Lock()
	defer s.ops.Unlock()
	state, e := s.Store.Snapshot()
	if e != nil {
		fail(w, e)
		return
	}
	t, e := state.Tunnel(r.PathValue("id"))
	if e == nil {
		switch r.PathValue("action") {
		case "start":
			e = s.Tunnels.Start(r.Context(), t)
		case "stop":
			s.Tunnels.Stop(t.ID)
		default:
			e = errors.New("unknown action")
		}
	}
	if e != nil {
		fail(w, e)
		return
	}
	reply(w, s.Tunnels.Statuses())
}
func (s *Server) checkHost(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	c, e := s.SSH.Connect(ctx, r.PathValue("id"))
	if e != nil {
		fail(w, e)
		return
	}
	c.Close()
	reply(w, map[string]bool{"ok": true})
}
func (s *Server) checkKey(w http.ResponseWriter, r *http.Request) {
	state, e := s.Store.Snapshot()
	if e != nil {
		fail(w, e)
		return
	}
	k, e := state.Key(r.PathValue("id"))
	if e == nil {
		e = key.Validate(k.Path)
	}
	if e != nil {
		fail(w, e)
		return
	}
	reply(w, map[string]bool{"ok": true})
}
func (s *Server) openService(w http.ResponseWriter, r *http.Request) {
	s.ops.Lock()
	defer s.ops.Unlock()
	state, e := s.Store.Snapshot()
	if e != nil {
		fail(w, e)
		return
	}
	for _, v := range state.Services {
		if v.ID == r.PathValue("id") {
			t, e := state.Tunnel(v.TunnelID)
			if e == nil {
				e = model.ValidateService(v, t)
			}
			if e == nil {
				e = s.Tunnels.Start(r.Context(), t)
			}
			if e == nil {
				ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
				e = s.Tunnels.Ready(ctx, t.ID)
				cancel()
			}
			if e != nil {
				fail(w, e)
				return
			}
			if e = s.OpenURL(v.URL); e != nil {
				fail(w, errors.New("터널은 준비됐지만 기본 브라우저를 열지 못했습니다: "+v.URL))
				return
			}
			reply(w, map[string]string{"url": v.URL})
			return
		}
	}
	fail(w, errors.New("service not found"))
}

type importRequest struct {
	Path    string   `json:"path"`
	Aliases []string `json:"aliases"`
}

func readConfig(path string) (sshconfig.Preview, error) { return sshconfig.Read(path) }
func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if e := decode(r, &req); e != nil {
		fail(w, e)
		return
	}
	p, e := readConfig(req.Path)
	if e != nil {
		fail(w, e)
		return
	}
	reply(w, p)
}
func (s *Server) importConfig(w http.ResponseWriter, r *http.Request) {
	s.ops.Lock()
	defer s.ops.Unlock()
	var req importRequest
	if e := decode(r, &req); e != nil {
		fail(w, e)
		return
	}
	p, e := readConfig(req.Path)
	if e != nil {
		fail(w, e)
		return
	}
	if len(req.Aliases) == 0 {
		fail(w, errors.New("가져올 Host를 선택하세요"))
		return
	}
	selected := map[string]bool{}
	for _, a := range req.Aliases {
		selected[a] = true
	}
	e = s.Store.Update(func(state *model.State) error {
		return sshconfig.ApplySelected(state, p, selected)
	})
	if e != nil {
		fail(w, e)
		return
	}
	reply(w, map[string]int{"imported": len(selected)})
}
