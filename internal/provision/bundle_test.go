package provision

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"sshdesk/internal/database"
	"sshdesk/internal/model"
	"sshdesk/internal/testssh"
)

func fixture(t *testing.T) model.State {
	t.Helper()
	_, path := testssh.Key(t)
	return model.State{
		Keys:     []model.Key{{ID: "chosen", Name: "Chosen key", Path: path}, {ID: "other", Name: "Other key", Path: path}},
		Hosts:    []model.Host{{ID: "host", Name: "Selected host", Environment: "DEV", Type: "Server", Address: "192.0.2.1", Port: 22, Username: "demo", KeyID: "chosen"}, {ID: "other-host", Name: "Other host", Environment: "STG", Type: "Server", Address: "192.0.2.2", Port: 22, Username: "demo", KeyID: "other"}},
		Tunnels:  []model.Tunnel{{ID: "tunnel", Name: "App", HostID: "host", LocalHost: "127.0.0.1", LocalPort: 18080, RemoteHost: "127.0.0.1", RemotePort: 8080}},
		Services: []model.Service{{ID: "web", Name: "App", Environment: "DEV", TunnelID: "tunnel", URL: "http://127.0.0.1:18080"}},
	}
}

func TestSelectedPackageInstallAndRepeat(t *testing.T) {
	source := fixture(t)
	payload, err := Create(source, []string{"chosen"})
	if err != nil {
		t.Fatal(err)
	}
	defer clear(payload)
	if bytes.Contains(payload, []byte("Other host")) || bytes.Contains(payload, []byte(source.Keys[0].Path)) {
		t.Fatal("unselected data or sender path leaked")
	}
	dir := t.TempDir()
	store, err := database.Open(filepath.Join(dir, "db"), model.Settings{SSHConfig: "preserved"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	installed, err := Install(store, dir, payload)
	if err != nil || !installed {
		t.Fatalf("install: %v %v", installed, err)
	}
	state, _ := store.Snapshot()
	if len(state.Keys) != 1 || len(state.Hosts) != 1 || len(state.Tunnels) != 1 || len(state.Services) != 1 || state.Settings.SSHConfig != "preserved" {
		t.Fatal("wrong imported scope")
	}
	actual, err := os.ReadFile(state.Keys[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(actual)
	original, _ := os.ReadFile(source.Keys[0].Path)
	defer clear(original)
	if !bytes.Equal(actual, original) {
		t.Fatal("wrong key bytes")
	}
	if err = store.Update(func(s *model.State) error { s.Hosts[0].Name = "Edited locally"; return nil }); err != nil {
		t.Fatal(err)
	}
	installed, err = Install(store, dir, payload)
	if err != nil || installed {
		t.Fatal("repeated installation")
	}
	state, _ = store.Snapshot()
	if state.Hosts[0].Name != "Edited locally" {
		t.Fatal("overwrote local edit")
	}
}

func TestJumpNeedsExplicitKeySelection(t *testing.T) {
	source := fixture(t)
	source.Hosts[0].JumpID = "other-host"
	if _, err := Create(source, []string{"chosen"}); err == nil {
		t.Fatal("silently bundled dependency key")
	}
	payload, err := Create(source, []string{"chosen", "other"})
	if err != nil {
		t.Fatal(err)
	}
	clear(payload)
}

func TestExecutableRoundTripAndStrip(t *testing.T) {
	source := fixture(t)
	payload, err := Create(source, []string{"chosen"})
	if err != nil {
		t.Fatal(err)
	}
	defer clear(payload)
	path := filepath.Join(t.TempDir(), "example.exe")
	base := []byte("synthetic executable")
	if err = os.WriteFile(path, base, 0600); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(path)
	var out bytes.Buffer
	if err = WriteExecutable(&out, f, payload); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err = os.WriteFile(path, out.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	extracted, err := ReadExecutable(path)
	if err != nil || !bytes.Equal(extracted, payload) {
		t.Fatal("bundle roundtrip")
	}
	clear(extracted)
	f, _ = os.Open(path)
	var clean bytes.Buffer
	if err = WriteExecutable(&clean, f, nil); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if !bytes.Equal(clean.Bytes(), base) {
		t.Fatal("original package leaked during re-share")
	}
	var invalid bytes.Buffer
	invalid.Write(base)
	binary.Write(&invalid, binary.LittleEndian, uint64(MaxPayload+1))
	invalid.WriteString(magic)
	os.WriteFile(path, invalid.Bytes(), 0600)
	if _, err = ReadExecutable(path); err == nil {
		t.Fatal("accepted invalid footer")
	}
}

func TestConflictRollsBackKeyFiles(t *testing.T) {
	source := fixture(t)
	payload, err := Create(source, []string{"chosen"})
	if err != nil {
		t.Fatal(err)
	}
	defer clear(payload)
	dir := t.TempDir()
	store, err := database.Open(filepath.Join(dir, "db"), model.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Update(func(s *model.State) error {
		s.Keys = []model.Key{{ID: "chosen", Name: "Existing", Path: "existing"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = Install(store, dir, payload); err == nil {
		t.Fatal("overwrote existing record")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "team-bundles", "*", "key-*"))
	if len(files) != 0 {
		t.Fatal("failed import left key files")
	}
	state, _ := store.Snapshot()
	if state.Keys[0].Name != "Existing" || len(state.Hosts) != 0 {
		t.Fatal("changed existing data")
	}
}
