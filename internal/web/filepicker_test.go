package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKeyFilePicker(t *testing.T) {
	app, ts, _ := setup(t)
	calls := 0
	app.PickKeyFile = func() (string, error) { calls++; return "C:\\keys\\한글 key.pem", nil }
	// An unauthenticated page must not be able to open native dialogs.
	req := httptest.NewRequest("POST", app.Origin+"/api/keys/pick-file", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden || calls != 0 {
		t.Fatal("picker bypassed CSRF protection")
	}
	code, body := request(t, app, ts, "POST", "/api/keys/pick-file", nil)
	if code != 200 || !strings.Contains(string(body), "한글 key.pem") || calls != 1 {
		t.Fatalf("selection: %d %s", code, body)
	}
	app.PickKeyFile = func() (string, error) { return "", nil }
	code, body = request(t, app, ts, "POST", "/api/keys/pick-file", nil)
	if code != 200 || !strings.Contains(string(body), `"cancelled":true`) {
		t.Fatalf("cancel: %d %s", code, body)
	}
	app.PickKeyFile = func() (string, error) { return "", errors.New("picker unavailable") }
	code, _ = request(t, app, ts, "POST", "/api/keys/pick-file", nil)
	if code != 400 {
		t.Fatal("missing picker error")
	}
	app.pickerMu.Lock()
	code, _ = request(t, app, ts, "POST", "/api/keys/pick-file", nil)
	app.pickerMu.Unlock()
	if code != 409 {
		t.Fatal("duplicate picker allowed")
	}
}
