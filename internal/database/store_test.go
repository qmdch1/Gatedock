package database

import (
	"path/filepath"
	"sshdesk/internal/model"
	"testing"
)

func TestCRUDPersistenceReferencesAndRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, e := Open(path, model.Settings{KnownHosts: "test"})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	e = s.Update(func(v *model.State) error {
		v.Keys = append(v.Keys, model.Key{ID: "k", Name: "key", Path: "/key"})
		v.Hosts = append(v.Hosts, model.Host{ID: "h", Name: "host", Environment: "DEV", Type: "Server", Address: "127.0.0.1", Port: 22, Username: "dev", KeyID: "k"})
		v.Tunnels = append(v.Tunnels, model.Tunnel{ID: "t", Name: "API", LocalHost: "127.0.0.1", LocalPort: 18080, RemoteHost: "localhost", RemotePort: 80, HostID: "h"})
		v.Services = append(v.Services, model.Service{ID: "s", Name: "web", Environment: "DEV", TunnelID: "t", URL: "http://127.0.0.1:18080"})
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Update(func(v *model.State) error { v.Keys = nil; return nil }); e == nil {
		t.Fatal("deleted referenced key")
	}
	v, e := s.Snapshot()
	if e != nil || len(v.Keys) != 1 {
		t.Fatal("rollback failed")
	}
	if e = s.Update(func(v *model.State) error { v.Hosts[0].Name = "changed"; return nil }); e != nil {
		t.Fatal(e)
	}
	s.Record(v.Hosts[0], "ssh", nil)
	s.Close()
	s, e = Open(path, model.Settings{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	v, e = s.Snapshot()
	if e != nil || v.Hosts[0].Name != "changed" || len(v.History) != 1 {
		t.Fatal("persistence failed")
	}
	if e = s.Update(func(v *model.State) error { v.Services = nil; v.Tunnels = nil; v.Hosts = nil; v.Keys = nil; return nil }); e != nil {
		t.Fatal(e)
	}
	v, _ = s.Snapshot()
	if len(v.Hosts) != 0 {
		t.Fatal("delete failed")
	}
}
func TestHistoryBounded(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "state.db"), model.Settings{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for i := 0; i < 210; i++ {
		s.Record(model.Host{ID: "h", Name: "h"}, "ssh", nil)
	}
	v, e := s.Snapshot()
	if e != nil || len(v.History) != 200 {
		t.Fatalf("history count %d %v", len(v.History), e)
	}
}
