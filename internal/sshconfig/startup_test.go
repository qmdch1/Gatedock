package sshconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"sshdesk/internal/database"
	"sshdesk/internal/model"
	"sshdesk/internal/testssh"
)

func autoFixture(t *testing.T) (*database.Store, string, string) {
	t.Helper()
	_, keyPath := testssh.Key(t)
	dir := t.TempDir()
	store, e := database.Open(filepath.Join(dir, "db"), model.Settings{})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { store.Close() })
	return store, filepath.Join(dir, "config"), keyPath
}
func configHost(name, key, jump string) string {
	return fmt.Sprintf("Host %s\n HostName 127.0.0.1\n User test\n IdentityFile %q\n%s", name, key, func() string {
		if jump != "" {
			return " ProxyJump " + jump + "\n"
		}
		return ""
	}())
}
func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if e := os.WriteFile(path, []byte(content), 0600); e != nil {
		t.Fatal(e)
	}
}

func TestAutoImportMultiJumpIdempotentAndPreserve(t *testing.T) {
	s, path, key := autoFixture(t)
	text := "\ufeff" + configHost("target", key, "first,second") + configHost("second", key, "") + configHost("first", key, "")
	writeConfig(t, path, text)
	r := AutoImport(s, path)
	if r.Imported != 3 || len(r.Skipped) != 0 {
		t.Fatalf("%+v", r)
	}
	state, _ := s.Snapshot()
	if len(state.Keys) != 1 {
		t.Fatal("key path not reused")
	}
	var target model.Host
	for _, h := range state.Hosts {
		if h.Name == "target" {
			target = h
		}
	}
	chain, e := state.Chain(target.ID)
	if e != nil || len(chain) != 3 || chain[0].Name != "first" || chain[1].Name != "second" {
		t.Fatalf("%+v %v", chain, e)
	}
	if e = s.Update(func(v *model.State) error {
		v.Hosts[0].Environment = "PROD"
		v.Hosts[0].Description = "User changes"
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	before, _ := s.Snapshot()
	r = AutoImport(s, path)
	after, _ := s.Snapshot()
	if r.Imported != 0 || r.Existing != 3 || !reflect.DeepEqual(before, after) {
		t.Fatal("restart changed existing configuration")
	}
	actual, _ := os.ReadFile(path)
	if string(actual) != text {
		t.Fatal("modified original SSH config")
	}
	if len(after.History) != 0 {
		t.Fatal("auto import connected to a host")
	}
}
func TestAutoImportSkipsInvalidDependenciesAndKeepsGoodHosts(t *testing.T) {
	s, path, key := autoFixture(t)
	text := configHost("good", key, "") + configHost("bad-key", filepath.Join(t.TempDir(), "missing-key"), "") + configHost("child", key, "bad-key") + configHost("unknown-hop", key, "absent") + configHost("unsafe", key, "") + " ProxyCommand unsafe-command\n"
	writeConfig(t, path, text)
	r := AutoImport(s, path)
	if r.Imported != 1 || len(r.Skipped) != 4 {
		t.Fatalf("%+v", r)
	}
	state, _ := s.Snapshot()
	if len(state.Hosts) != 1 || state.Hosts[0].Name != "good" || len(state.Keys) != 1 {
		t.Fatal("unexpected state")
	}
}
func TestAutoImportCycleRollback(t *testing.T) {
	s, path, key := autoFixture(t)
	writeConfig(t, path, configHost("a", key, "b")+configHost("b", key, "a")+configHost("good", key, ""))
	r := AutoImport(s, path)
	if r.Imported != 1 || len(r.Skipped) != 2 {
		t.Fatalf("%+v", r)
	}
	state, _ := s.Snapshot()
	if len(state.Keys) != 1 || len(state.Hosts) != 1 {
		t.Fatal("partial group saved")
	}
}
func TestAutoImportExistingJumpIsPreserved(t *testing.T) {
	s, path, key := autoFixture(t)
	writeConfig(t, path, configHost("jump", key, ""))
	r := AutoImport(s, path)
	if r.Imported != 1 {
		t.Fatal(r)
	}
	writeConfig(t, path, configHost("jump", key, "")+configHost("target", key, "jump"))
	r = AutoImport(s, path)
	if r.Imported != 1 || r.Existing != 1 {
		t.Fatalf("%+v", r)
	}
	state, _ := s.Snapshot()
	chain, e := state.Chain(state.Hosts[1].ID)
	if e != nil || len(chain) != 2 {
		t.Fatal("missing existing jump")
	}
}
func TestAutoImportMissingMalformedAndUnsupported(t *testing.T) {
	s, path, key := autoFixture(t)
	if r := AutoImport(s, path); !r.Missing {
		t.Fatal(r)
	}
	writeConfig(t, path, "Host \"unterminated")
	if r := AutoImport(s, path); len(r.Warnings) == 0 {
		t.Fatal("missing parse warning")
	}
	writeConfig(t, path, "Include other-file\n"+configHost("app", key, ""))
	r := AutoImport(s, path)
	if r.Imported != 0 || len(r.Skipped) != 1 {
		t.Fatal("unsupported include imported")
	}
}
