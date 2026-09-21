package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/coder/websocket"
	"golang.org/x/crypto/ssh"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sshdesk/internal/database"
	"sshdesk/internal/model"
	"sshdesk/internal/sshclient"
	"sshdesk/internal/testssh"
	"sshdesk/internal/tunnel"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T) (*Server, *httptest.Server, string) {
	t.Helper()
	signer, keyPath := testssh.Key(t)
	fixture := testssh.Start(t, signer.PublicKey())
	store, e := database.Open(filepath.Join(t.TempDir(), "app.db"), model.Settings{})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { store.Close() })
	e = store.Update(func(s *model.State) error {
		s.Keys = []model.Key{{ID: "key", Name: "Test key", Path: keyPath}}
		s.Hosts = []model.Host{{ID: "host", Name: "Test host", Environment: "LOCAL", Type: "Server", Address: fixture.Address, Port: fixture.Port, Username: "test", KeyID: "key", Fingerprint: ssh.FingerprintSHA256(fixture.Signer.PublicKey())}}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	client := &sshclient.Manager{Store: store}
	manager := tunnel.New(client)
	app, e := New(store, client, manager, "http://127.0.0.1:9876")
	if e != nil {
		t.Fatal(e)
	}
	ts := httptest.NewServer(app.Handler())
	app.Origin = ts.URL
	t.Cleanup(ts.Close)
	t.Cleanup(app.Close)
	return app, ts, keyPath
}
func request(t *testing.T, app *Server, ts *httptest.Server, method, path string, value any) (int, []byte) {
	t.Helper()
	b, _ := json.Marshal(value)
	req, e := http.NewRequest(method, ts.URL+path, bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", app.token)
	req.Header.Set("Origin", ts.URL)
	res, e := ts.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, body
}
func TestHTTPOriginHostCSRFAndAssets(t *testing.T) {
	app, ts, _ := setup(t)
	for _, path := range []string{"/", "/health", "/api/state", "/static/app.js", "/static/vendor/xterm.js", "/static/vendor/xterm.css"} {
		code, b := request(t, app, ts, "GET", path, nil)
		if code != 200 || len(b) == 0 {
			t.Fatalf("%s %d", path, code)
		}
	}
	for _, attack := range []string{"origin", "host", "csrf", "fetch"} {
		req, _ := http.NewRequest("POST", ts.URL+"/api/settings", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", app.token)
		switch attack {
		case "origin":
			req.Header.Set("Origin", "https://evil.test")
		case "host":
			req.Host = "evil.test"
		case "csrf":
			req.Header.Del("X-CSRF-Token")
		case "fetch":
			req.Header.Set("Sec-Fetch-Site", "cross-site")
		}
		res, e := ts.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		res.Body.Close()
		if res.StatusCode != 403 {
			t.Errorf("allowed %s", attack)
		}
	}
}
func TestHTTPCRUDReferencesAndValidation(t *testing.T) {
	app, ts, _ := setup(t)
	code, _ := request(t, app, ts, "DELETE", "/api/keys/key", nil)
	if code != 400 {
		t.Fatal("deleted key in use")
	}
	v := model.Tunnel{Name: "API", LocalHost: "127.0.0.1", LocalPort: 18080, RemoteHost: "127.0.0.1", RemotePort: 80, HostID: "host"}
	code, b := request(t, app, ts, "POST", "/api/tunnels", v)
	if code != 200 {
		t.Fatalf("save %s", b)
	}
	state, _ := app.Store.Snapshot()
	id := state.Tunnels[0].ID
	v = state.Tunnels[0]
	v.Name = "updated"
	code, _ = request(t, app, ts, "POST", "/api/tunnels", v)
	if code != 200 {
		t.Fatal("edit failed")
	}
	code, _ = request(t, app, ts, "POST", "/api/services", model.Service{Name: "unsafe", Environment: "DEV", TunnelID: id, URL: "https://evil.test"})
	if code != 400 {
		t.Fatal("unsafe URL accepted")
	}
	code, _ = request(t, app, ts, "DELETE", "/api/tunnels/"+id, nil)
	if code != 200 {
		t.Fatal("delete failed")
	}
}
func TestImportSelectedAndAtomicFailure(t *testing.T) {
	app, ts, keyPath := setup(t)
	path := filepath.Join(t.TempDir(), "config")
	content := fmt.Sprintf("Host jump\n HostName 127.0.0.1\n User test\n IdentityFile %q\nHost target\n HostName 127.0.0.1\n User test\n IdentityFile %q\n ProxyJump jump\n", keyPath, keyPath)
	if e := os.WriteFile(path, []byte(content), 0600); e != nil {
		t.Fatal(e)
	}
	code, b := request(t, app, ts, "POST", "/api/import/preview", importRequest{Path: path})
	if code != 200 || !bytes.Contains(b, []byte("target")) {
		t.Fatalf("preview %s", b)
	}
	code, _ = request(t, app, ts, "POST", "/api/import/apply", importRequest{Path: path, Aliases: []string{"target"}})
	if code != 400 {
		t.Fatal("missing jump allowed")
	}
	state, _ := app.Store.Snapshot()
	if len(state.Hosts) != 1 {
		t.Fatal("partial import persisted")
	}
	code, b = request(t, app, ts, "POST", "/api/import/apply", importRequest{Path: path, Aliases: []string{"target", "jump"}})
	if code != 200 {
		t.Fatalf("import %s", b)
	}
	state, _ = app.Store.Snapshot()
	if len(state.Hosts) != 3 {
		t.Fatal("import count")
	}
	after, _ := os.ReadFile(path)
	if string(after) != content {
		t.Fatal("config modified")
	}
}
func TestWebSocketOriginAuthAndRealTerminal(t *testing.T) {
	app, ts, _ := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/terminal/host"
	bad, _, e := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://evil.test"}}})
	if e == nil {
		bad.CloseNow()
		t.Fatal("cross origin accepted")
	}
	ws, _, e := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{ts.URL}}})
	if e != nil {
		t.Fatal(e)
	}
	defer ws.CloseNow()
	send := func(v any) {
		b, _ := json.Marshal(v)
		if e := ws.Write(ctx, websocket.MessageText, b); e != nil {
			t.Fatal(e)
		}
	}
	send(terminalInput{Type: "auth", Token: app.token, Cols: 100, Rows: 30})
	all := ""
	for !strings.Contains(all, "테스트 SSH ready") || !strings.Contains(all, "stderr-ready") {
		_, b, e := ws.Read(ctx)
		if e != nil {
			t.Fatal(e)
		}
		all += string(b)
	}
	send(terminalInput{Type: "resize", Cols: 120, Rows: 40})
	send(terminalInput{Type: "input", Data: "WebSocket 한글\x03"})
	all = ""
	for !strings.Contains(all, "WebSocket 한글\x03") {
		_, b, e := ws.Read(ctx)
		if e != nil {
			t.Fatal(e)
		}
		all += string(b)
	}
	send(terminalInput{Type: "input", Data: "\x04"})
	for {
		_, b, e := ws.Read(ctx)
		if e != nil {
			break
		}
		if bytes.Contains(b, []byte(`"type":"exit"`)) {
			break
		}
	}
	unauth, _, e := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{ts.URL}}})
	if e != nil {
		t.Fatal(e)
	}
	defer unauth.CloseNow()
	_ = unauth.Write(ctx, websocket.MessageText, []byte(`{"type":"auth","token":"wrong"}`))
	if _, _, e = unauth.Read(ctx); e == nil {
		t.Fatal("invalid token accepted")
	}
}
func TestServiceAutoStartAndReachability(t *testing.T) {
	app, ts, _ := setup(t)
	remote, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer remote.Close()
	go func() {
		for {
			c, e := remote.Accept()
			if e != nil {
				return
			}
			c.Close()
		}
	}()
	local, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := local.Addr().(*net.TCPAddr).Port
	local.Close()
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	e = app.Store.Update(func(s *model.State) error {
		s.Tunnels = []model.Tunnel{{ID: "tun", Name: "tunnel", LocalHost: "127.0.0.1", LocalPort: port, RemoteHost: "127.0.0.1", RemotePort: remote.Addr().(*net.TCPAddr).Port, HostID: "host"}}
		s.Services = []model.Service{{ID: "svc", Name: "service", Environment: "DEV", TunnelID: "tun", URL: url}}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	opened := ""
	app.OpenURL = func(u string) error { opened = u; return nil }
	code, b := request(t, app, ts, "POST", "/api/services/svc/open", nil)
	if code != 200 || opened != url || !app.Tunnels.Active("tun") {
		t.Fatalf("open %d %s %s", code, b, opened)
	}
	remote.Close()
	opened = ""
	code, _ = request(t, app, ts, "POST", "/api/services/svc/open", nil)
	if code != 400 || opened != "" {
		t.Fatal("opened unreachable service")
	}
}
