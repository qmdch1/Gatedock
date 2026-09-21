package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestShareDownloadBoundary(t *testing.T) {
	app, _, _ := setup(t)
	cases := []struct {
		method, path, remote, host, site string
		code                             int
	}{
		{"GET", "/", "127.0.0.1:1234", "127.0.0.1:9877", "", 200},
		{"GET", "/download/configuration", "127.0.0.1:1234", "127.0.0.1:9877", "", 200},
		{"GET", "/api/state", "127.0.0.1:1234", "127.0.0.1:9877", "", 404},
		{"GET", "/ws/terminal/host", "127.0.0.1:1234", "127.0.0.1:9877", "", 404},
		{"POST", "/download/configuration", "127.0.0.1:1234", "127.0.0.1:9877", "", 405},
		{"GET", "/", "203.0.113.99:1234", "127.0.0.1:9877", "", 403},
		{"GET", "/", "127.0.0.1:1234", "attacker.example:9877", "", 403},
		{"GET", "/download/configuration", "127.0.0.1:1234", "127.0.0.1:9877", "cross-site", 403},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, "http://"+c.host+c.path, nil)
		r.RemoteAddr = c.remote
		r.Header.Set("Sec-Fetch-Site", c.site)
		w := httptest.NewRecorder()
		app.shareHandler().ServeHTTP(w, r)
		if w.Code != c.code {
			t.Fatalf("%s %s: %d", c.method, c.path, w.Code)
		}
		if c.path == "/download/configuration" && c.code == 200 {
			body := w.Body.String()
			if !strings.Contains(body, `"format": "sshdesk"`) || strings.Contains(body, "PRIVATE KEY") || strings.Contains(body, "history") {
				t.Fatal("invalid export boundary")
			}
		}
	}
}
