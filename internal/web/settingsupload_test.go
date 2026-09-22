package web

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestBrowserSettingsUploadAtomic(t *testing.T) {
	app, ts, _ := setup(t)
	config := []byte("Host demo\n HostName 127.0.0.1\n User test\n IdentityFile ~/.ssh/id_ed25519\n")
	code, body := request(t, app, ts, "POST", "/api/settings/files", map[string]any{"config_content": config, "hosts_content": []byte("# known hosts\n")})
	if code != 200 {
		t.Fatalf("save: %d %s", code, body)
	}
	state, _ := app.Store.Snapshot()
	old := state.Settings
	if filepath.Dir(old.SSHConfig) != filepath.Join(app.Store.DataDir(), "uploaded-settings") {
		t.Fatal("wrong directory")
	}
	data, err := os.ReadFile(old.SSHConfig)
	if err != nil || !bytes.Equal(data, config) {
		t.Fatal("config not persisted")
	}
	for _, payload := range []map[string]any{
		{"config_content": config, "hosts_content": []byte("invalid host entry")},
		{"config_content": config, "known_hosts": "relative-path"},
		{"config_content": make([]byte, 1024*1024+1), "known_hosts": old.KnownHosts},
	} {
		code, _ = request(t, app, ts, "POST", "/api/settings/files", payload)
		if code != 400 {
			t.Fatalf("bad upload accepted: %d", code)
		}
		current, _ := app.Store.Snapshot()
		if current.Settings != old {
			t.Fatal("failed upload changed settings")
		}
		files, _ := os.ReadDir(filepath.Dir(old.SSHConfig))
		if len(files) != 2 {
			t.Fatal("failed upload retained files")
		}
	}
	code, _ = request(t, app, ts, "POST", "/api/import/preview", map[string]any{"path": old.SSHConfig})
	if code != 200 {
		t.Fatal("uploaded config preview failed")
	}
}
