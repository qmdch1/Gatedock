package web

import (
	"bytes"
	"encoding/json"
	"sshdesk/internal/model"
	"testing"
)

func TestBackupRoundtripPreviewAndNoSecrets(t *testing.T) {
	app, ts, _ := setup(t)
	state, _ := app.Store.Snapshot()
	app.Store.Record(state.Hosts[0], "ssh", nil)
	code, raw := request(t, app, ts, "POST", "/api/backup/export", nil)
	if code != 200 {
		t.Fatal(string(raw))
	}
	for _, forbidden := range []string{"PRIVATE KEY", "history", "token", "password"} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatalf("export contains %s", forbidden)
		}
	}
	var b Backup
	if e := json.Unmarshal(raw, &b); e != nil {
		t.Fatal(e)
	}
	if len(b.Hosts) != 1 || len(b.Keys) != 1 || b.Version != 1 {
		t.Fatal("missing configuration")
	}
	b.Hosts[0].ID = "new-host"
	b.Hosts[0].Name = "Imported host"
	req := backupRequest{Backup: b}
	code, raw = request(t, app, ts, "POST", "/api/backup/preview", req)
	if code != 200 {
		t.Fatal(string(raw))
	}
	before, _ := app.Store.Snapshot()
	if len(before.Hosts) != 1 {
		t.Fatal("preview wrote data")
	}
	code, raw = request(t, app, ts, "POST", "/api/backup/apply", req)
	if code != 200 {
		t.Fatal(string(raw))
	}
	after, _ := app.Store.Snapshot()
	if len(after.Hosts) != 2 || len(after.Keys) != 1 || len(after.History) != 1 {
		t.Fatal("bad merge")
	}
	code, raw = request(t, app, ts, "POST", "/api/backup/apply", req)
	if code != 200 {
		t.Fatal("not idempotent", string(raw))
	}
	after, _ = app.Store.Snapshot()
	if len(after.Hosts) != 2 {
		t.Fatal("duplicates")
	}
}
func TestBackupInvalidAndConflictAreAtomic(t *testing.T) {
	app, ts, _ := setup(t)
	_, raw := request(t, app, ts, "POST", "/api/backup/export", nil)
	var base Backup
	_ = json.Unmarshal(raw, &base)
	for _, scenario := range []string{"version", "duplicate", "conflict", "missing-key", "cycle", "unsafe-bind", "unsafe-url"} {
		t.Run(scenario, func(t *testing.T) {
			var b Backup
			_ = json.Unmarshal(raw, &b)
			switch scenario {
			case "version":
				b.Version = 9
			case "duplicate":
				b.Hosts = append(b.Hosts, b.Hosts[0])
			case "conflict":
				b.Hosts[0].Name = "Changed"
			case "missing-key":
				b.Hosts[0].ID = "new"
				b.Hosts[0].Name = "New"
				b.Hosts[0].KeyID = "missing"
			case "cycle":
				b.Hosts[0].ID = "new"
				b.Hosts[0].Name = "New"
				b.Hosts[0].JumpID = "new"
			case "unsafe-bind":
				b.Tunnels = []model.Tunnel{{ID: "t", Name: "t", LocalHost: "0.0.0.0", LocalPort: 18080, RemoteHost: "127.0.0.1", RemotePort: 80, HostID: "host"}}
			case "unsafe-url":
				b.Services = []model.Service{{ID: "s", Name: "s", Environment: "DEV", TunnelID: "missing", URL: "https://evil.test"}}
			}
			code, _ := request(t, app, ts, "POST", "/api/backup/apply", backupRequest{Backup: b})
			if code != 400 {
				t.Fatalf("accepted %s", scenario)
			}
			s, _ := app.Store.Snapshot()
			if len(s.Hosts) != 1 || s.Hosts[0].Name != base.Hosts[0].Name || len(s.Tunnels) != 0 {
				t.Fatal("partial mutation")
			}
		})
	}
	code, _ := request(t, app, ts, "POST", "/api/backup/apply", map[string]any{"backup": map[string]any{"format": "sshdesk", "version": 1, "private_key": "forbidden"}})
	if code != 400 {
		t.Fatal("unknown secret field accepted")
	}
}
func TestBackupMachineSettingsOptIn(t *testing.T) {
	app, ts, path := setup(t)
	b := Backup{Format: "sshdesk", Version: 1, Settings: model.Settings{KnownHosts: path, SSHConfig: path}}
	code, _ := request(t, app, ts, "POST", "/api/backup/apply", backupRequest{Backup: b})
	if code != 200 {
		t.Fatal(code)
	}
	state, _ := app.Store.Snapshot()
	if state.Settings.KnownHosts != "" {
		t.Fatal("settings changed without opt in")
	}
	code, _ = request(t, app, ts, "POST", "/api/backup/apply", backupRequest{Backup: b, ImportSettings: true})
	if code != 200 {
		t.Fatal(code)
	}
	state, _ = app.Store.Snapshot()
	if state.Settings.KnownHosts != path {
		t.Fatal("settings missing")
	}
}
