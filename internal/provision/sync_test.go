package provision

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"sshdesk/internal/database"
	"sshdesk/internal/model"
)

func syncFixture(t *testing.T) (*database.Store, *database.Store, model.TeamSource, []byte) {
	t.Helper()
	master, err := database.Open(filepath.Join(t.TempDir(), "master.db"), model.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { master.Close() })
	state := fixture(t)
	if err = master.Update(func(s *model.State) error { *s = state; return nil }); err != nil {
		t.Fatal(err)
	}
	feed, public, err := PrepareFeed(master, []string{"chosen"})
	if err != nil {
		t.Fatal(err)
	}
	source := model.TeamSource{URL: "http://127.0.0.1:9877", Feed: feed, PublicKey: public}
	payload, err := Create(state, []string{"chosen"})
	if err != nil {
		t.Fatal(err)
	}
	attached, err := AttachSource(payload, source)
	clear(payload)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { clear(attached) })
	dir := t.TempDir()
	client, err := database.Open(filepath.Join(dir, "client.db"), model.Settings{SSHConfig: "keep-local"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	if _, err = Install(client, dir, attached); err != nil {
		t.Fatal(err)
	}
	return master, client, source, attached
}

func pull(t *testing.T, master, client *database.Store, source model.TeamSource) error {
	t.Helper()
	nonce := database.ID() + database.ID()
	signed, err := SignFeed(master, source.Feed, nonce)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(signed)
	incoming, err := VerifyFeed(source, nonce, raw)
	if err != nil {
		return err
	}
	return client.Update(func(s *model.State) error { return ApplyFeed(s, 0, incoming) })
}

func TestSyncLifecycleRetainsDeletedAndLocalRecords(t *testing.T) {
	master, client, source, _ := syncFixture(t)
	initial, _ := client.Snapshot()
	keyPath := initial.Keys[0].Path
	if err := client.Update(func(s *model.State) error {
		h := s.Hosts[0]
		h.ID = "personal"
		h.Name = "My personal host"
		s.Hosts = append(s.Hosts, h)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := master.Update(func(s *model.State) error {
		s.Hosts[0].Address = "192.0.2.77"
		h := s.Hosts[0]
		h.ID = "new"
		h.Name = "New shared host"
		s.Hosts = append(s.Hosts, h)
		s.Tunnels[0].RemotePort = 9090
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := pull(t, master, client, source); err != nil {
		t.Fatal(err)
	}
	updated, _ := client.Snapshot()
	if len(updated.Hosts) != 3 || updated.Hosts[0].Address != "192.0.2.77" || updated.Tunnels[0].RemotePort != 9090 || updated.Keys[0].Path != keyPath || updated.Settings.SSHConfig != "keep-local" {
		t.Fatal("incorrect sync scope")
	}
	if updated.Team[0].Items["hosts/personal"] != "" {
		t.Fatal("adopted personal item")
	}
	beforeDelete, _ := master.Snapshot()
	if err := master.Update(func(s *model.State) error { s.Services = nil; s.Tunnels = nil; s.Hosts = nil; s.Keys = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := pull(t, master, client, source); err != nil {
		t.Fatal(err)
	}
	retained, _ := client.Snapshot()
	if len(retained.Hosts) != 3 || len(retained.Tunnels) != 1 || len(retained.Services) != 1 || retained.Team[0].Items["hosts/host"] != "missing" || retained.Team[0].Items["services/web"] != "missing" {
		t.Fatal("deleted instead of retaining")
	}
	if err := master.Update(func(s *model.State) error { *s = beforeDelete; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := pull(t, master, client, source); err != nil {
		t.Fatal(err)
	}
	restored, _ := client.Snapshot()
	if restored.Team[0].Items["hosts/host"] != "current" {
		t.Fatal("not restored")
	}
}

func TestSyncSignatureNonceAndScope(t *testing.T) {
	master, _, source, _ := syncFixture(t)
	nonce := database.ID() + database.ID()
	signed, err := SignFeed(master, source.Feed, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(signed.Message, []byte("Other host")) || bytes.Contains(signed.Message, []byte("key_material")) || bytes.Contains(signed.Message, []byte("PRIVATE KEY")) {
		t.Fatal("scope or key leak")
	}
	raw, _ := json.Marshal(signed)
	if _, err = VerifyFeed(source, "another-nonce", raw); err == nil {
		t.Fatal("accepted replay")
	}
	signed.Message = append(signed.Message, ' ')
	raw, _ = json.Marshal(signed)
	if _, err = VerifyFeed(source, nonce, raw); err == nil {
		t.Fatal("accepted tampered response")
	}
	for _, address := range []string{"https://127.0.0.1:9877", "http://example.com:9877", "http://203.0.113.1:9877", "http://127.0.0.1:9877/path", "http://127.0.0.1:80", "http://user@127.0.0.1:9877"} {
		source.URL = address
		if ValidateSource(source) == nil {
			t.Fatal("accepted", address)
		}
	}
}

func TestSyncConflictsAreAtomicAndPrivateMetadataNotExported(t *testing.T) {
	master, client, source, _ := syncFixture(t)
	if err := client.Update(func(s *model.State) error {
		h := s.Hosts[0]
		h.ID = "personal"
		h.Name = "Reserved name"
		s.Hosts = append(s.Hosts, h)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, _ := client.Snapshot()
	if err := master.Update(func(s *model.State) error { s.Hosts[0].Name = "Reserved name"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := pull(t, master, client, source); err == nil {
		t.Fatal("accepted colliding name")
	}
	after, _ := client.Snapshot()
	if !EqualConfig(before, after) {
		t.Fatal("partial update on failure")
	}
	raw, _ := json.Marshal(after)
	if bytes.Contains(raw, []byte(source.Feed)) || bytes.Contains(raw, []byte("public_key")) {
		t.Fatal("exported subscription")
	}
}

func TestRedownloadSameMasterUpdatesAndKeepsSubscription(t *testing.T) {
	master, client, source, payload := syncFixture(t)
	if err := master.Update(func(s *model.State) error { s.Hosts[0].Description = "master changed"; return nil }); err != nil {
		t.Fatal(err)
	}
	updated, err := RefreshBundle(payload, master)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(updated)
	if _, err = Install(client, t.TempDir(), updated); err != nil {
		t.Fatal(err)
	}
	after, _ := client.Snapshot()
	if len(after.Team) != 1 || after.Hosts[0].Description != "master changed" || after.Team[0].Source.Feed != source.Feed {
		t.Fatal("redownload failed")
	}
}

func TestMasterDoesNotSubscribeToOwnInstaller(t *testing.T) {
	master, _, _, payload := syncFixture(t)
	installed, err := Install(master, t.TempDir(), payload)
	if err != nil || installed {
		t.Fatalf("self install: %v %v", installed, err)
	}
	state, _ := master.Snapshot()
	if len(state.Team) != 0 {
		t.Fatal("self subscription")
	}
}

func TestSyncIdentityAndSubscriptionPersistAcrossRestart(t *testing.T) {
	master, client, source, _ := syncFixture(t)
	before, _ := client.Snapshot()
	path := filepath.Join(t.TempDir(), "persist.db")
	store, err := database.Open(path, model.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Update(func(s *model.State) error { *s = before; return nil }); err != nil {
		t.Fatal(err)
	}
	secret, err := master.LocalValue("team-signing-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(secret)
	if _, err = store.LocalValue("identity-test", func() ([]byte, error) { return secret, nil }); err != nil {
		t.Fatal(err)
	}
	store.Close()
	reopened, err := database.Open(path, model.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	state, err := reopened.Snapshot()
	if err != nil || len(state.Team) != 1 || !SameSource(state.Team[0].Source, source) {
		t.Fatal("lost provenance")
	}
	actual, err := reopened.LocalValue("identity-test", nil)
	defer clear(actual)
	if err != nil || !bytes.Equal(actual, secret) {
		t.Fatal("lost identity")
	}
}
