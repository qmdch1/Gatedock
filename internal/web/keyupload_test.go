package web

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBrowserKeyUpload(t *testing.T) {
	app, ts, fixture := setup(t)
	content, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	code, body := request(t, app, ts, "POST", "/api/keys/upload", map[string]any{"name": "Browser key", "content": content})
	if code != 200 {
		t.Fatalf("upload: %d %s", code, body)
	}
	if bytes.Contains(body, content) {
		t.Fatal("response leaked key")
	}
	state, err := app.Store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var path string
	for _, k := range state.Keys {
		if k.Name == "Browser key" {
			path = k.Path
		}
	}
	if filepath.Dir(path) != filepath.Join(app.Store.DataDir(), "uploaded-keys") {
		t.Fatalf("unexpected path: %q", path)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatal("key did not persist")
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Fatal("unsafe key permissions")
		}
		info, _ = os.Stat(filepath.Dir(path))
		if info.Mode().Perm() != 0700 {
			t.Fatal("unsafe directory permissions")
		}
	}
	for _, payload := range []map[string]any{
		{"name": "Invalid", "content": []byte("not a key")},
		{"name": "Empty", "content": []byte{}},
		{"name": "Large", "content": make([]byte, 1024*1024+1)},
		{"name": "", "content": content},
		{"id": "missing", "name": "Unknown", "content": content},
	} {
		code, _ = request(t, app, ts, "POST", "/api/keys/upload", payload)
		if code != 400 {
			t.Fatalf("invalid upload accepted: %d", code)
		}
		files, _ := os.ReadDir(filepath.Dir(path))
		if len(files) != 1 {
			t.Fatal("failed upload left key files")
		}
	}
	code, body = request(t, app, ts, "GET", "/api/state", nil)
	if code != 200 || bytes.Contains(body, content) {
		t.Fatal("state leaked key")
	}
}
