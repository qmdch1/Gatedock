package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sshdesk/internal/model"
	"testing"
)

func TestSettingsEditorSaveAndConflicts(t *testing.T) {
	app, ts, keyPath := setup(t)
	path := filepath.Join(t.TempDir(), "config")
	original := "Host demo\n HostName 127.0.0.1\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	app.Store.Update(func(s *model.State) error {
		s.Settings.SSHConfig = path
		s.Settings.KnownHosts = filepath.Join(filepath.Dir(path), "known_hosts")
		return nil
	})
	code, body := request(t, app, ts, "POST", "/api/settings/editor/read", map[string]string{"kind": "ssh_config"})
	if code != 200 {
		t.Fatalf("read: %s", body)
	}
	var result map[string]string
	json.Unmarshal(body, &result)
	if result["content"] != original {
		t.Fatal("incorrect content")
	}
	req := map[string]string{"kind": "ssh_config", "path": path, "revision": result["revision"], "content": "Host demo\n HostName 127.0.0.2\n"}
	code, body = request(t, app, ts, "POST", "/api/settings/editor/save", req)
	if code != 200 {
		t.Fatalf("save: %s", body)
	}
	b, _ := os.ReadFile(path)
	if string(b) != req["content"] {
		t.Fatal("not saved")
	}
	code, _ = request(t, app, ts, "POST", "/api/settings/editor/save", req)
	if code != 409 {
		t.Fatal("accepted stale edit")
	}
	// Reading unrelated paths or private keys is not an editor capability.
	code, _ = request(t, app, ts, "POST", "/api/settings/editor/read", map[string]string{"kind": "private_key", "path": keyPath})
	if code != 400 {
		t.Fatal("accepted unknown kind")
	}
	app.Store.Update(func(s *model.State) error { s.Settings.SSHConfig = keyPath; return nil })
	code, _ = request(t, app, ts, "POST", "/api/settings/editor/read", map[string]string{"kind": "ssh_config"})
	if code != 400 {
		t.Fatal("returned private key")
	}
	code, _ = request(t, app, ts, "POST", "/api/settings/editor/save", req)
	if code == 200 {
		t.Fatal("accepted changed settings path")
	}
}

func TestSettingsEditorKnownHostsValidation(t *testing.T) {
	app, ts, _ := setup(t)
	path := filepath.Join(t.TempDir(), "known_hosts")
	app.Store.Update(func(s *model.State) error { s.Settings.KnownHosts = path; return nil })
	req := map[string]string{"kind": "known_hosts", "path": path, "revision": "missing", "content": "not a host entry"}
	code, _ := request(t, app, ts, "POST", "/api/settings/editor/save", req)
	if code != 400 {
		t.Fatal("invalid known_hosts accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("invalid file created")
	}
	req["content"] = "# trusted hosts\n"
	code, body := request(t, app, ts, "POST", "/api/settings/editor/save", req)
	if code != 200 {
		t.Fatalf("create: %s", body)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatal("temporary file retained")
	}
}
