package web

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"sshdesk/internal/database"
	"sshdesk/internal/model"
	"sshdesk/internal/provision"
)

func TestTeamPollingOverLoopbackAndOfflineRetention(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:9877")
	if err != nil {
		t.Skip("isolated sync port already in use")
	}
	master, masterHTTP, _ := setup(t)
	server := &http.Server{Handler: master.shareHandler()}
	go server.Serve(listener)
	defer server.Close()
	code, body := request(t, master, masterHTTP, "POST", "/api/sharing/prepare", map[string]any{"key_ids": []string{"key"}})
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1:9877/download/sshdesk.exe", nil)
	payload, err := master.downloadPayload(r)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(payload)
	dir := t.TempDir()
	store, err := database.Open(filepath.Join(dir, "subscriber.db"), model.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = provision.Install(store, dir, payload); err != nil {
		t.Fatal(err)
	}
	// Use the production poller without opening any SSH connections.
	client := &Server{Store: store}
	if err = master.Store.Update(func(s *model.State) error { s.Hosts[0].Description = "applied over HTTP"; return nil }); err != nil {
		t.Fatal(err)
	}
	client.pollTeam(context.Background())
	after, _ := store.Snapshot()
	if after.Hosts[0].Description != "applied over HTTP" || after.Team[0].LastSync == "" || after.Team[0].Error != "" {
		t.Fatalf("poll failed: %+v", after.Team)
	}
	server.Close()
	client.pollTeam(context.Background())
	offline, _ := store.Snapshot()
	if offline.Team[0].Error == "" || !provision.EqualConfig(after, offline) || offline.Team[0].Items["hosts/host"] != "current" {
		t.Fatal("offline changed retained state")
	}
}

func TestSignedFeedBoundaryAndLiveDownload(t *testing.T) {
	app, ts, _ := setup(t)
	code, body := request(t, app, ts, "POST", "/api/sharing/prepare", map[string]any{"key_ids": []string{"key"}})
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	if err := app.Store.Update(func(s *model.State) error { s.Hosts[0].Description = "live change"; return nil }); err != nil {
		t.Fatal(err)
	}
	nonce := database.ID() + database.ID()
	r := httptest.NewRequest("GET", "http://127.0.0.1:9877/sync/"+app.shareFeed+"?nonce="+nonce, nil)
	r.RemoteAddr = "127.0.0.1:2222"
	w := httptest.NewRecorder()
	app.shareHandler().ServeHTTP(w, r)
	source := model.TeamSource{URL: "http://127.0.0.1:9877", Feed: app.shareFeed, PublicKey: app.sharePublic}
	state, err := provision.VerifyFeed(source, nonce, w.Body.Bytes())
	if err != nil || state.Hosts[0].Description != "live change" {
		t.Fatalf("live feed %v", err)
	}
	if len(app.sharePeers) != 1 {
		t.Fatal("missing peer")
	}
	payload, err := app.downloadPayload(r)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(payload)
	var bundle provision.Bundle
	if err = json.Unmarshal(payload, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Source == nil || bundle.Source.URL != source.URL || bundle.State.Hosts[0].Description != "live change" {
		t.Fatal("stale installer")
	}
	for _, raw := range bundle.Keys {
		clear(raw)
	}
	r.RemoteAddr = "203.0.113.10:2222"
	w = httptest.NewRecorder()
	app.shareHandler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("public feed access")
	}
	r.RemoteAddr = "127.0.0.1:2222"
	r.Header.Set("Origin", "http://attacker.example")
	w = httptest.NewRecorder()
	app.shareHandler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin feed")
	}
	code, body = request(t, app, ts, "POST", "/api/backup/export", nil)
	if code != 200 || strings.Contains(string(body), "team-signing-key") || strings.Contains(string(body), app.shareFeed) {
		t.Fatal("exported local identity")
	}
}

func TestSubscriptionControls(t *testing.T) {
	app, ts, _ := setup(t)
	source := model.TeamSource{URL: "http://127.0.0.1:9877", Feed: strings.Repeat("a", 64), PublicKey: make([]byte, 32)}
	if err := app.Store.Update(func(s *model.State) error {
		s.Team = []model.TeamSubscription{{Source: source, Items: map[string]string{"hosts/host": "current"}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"pause", "resume"} {
		code, body := request(t, app, ts, "POST", "/api/team-sync/"+action, map[string]string{"id": source.Feed})
		if code != 200 {
			t.Fatalf("%d %s", code, body)
		}
		state, _ := app.Store.Snapshot()
		if state.Team[0].Paused != (action == "pause") {
			t.Fatal("wrong pause state")
		}
	}
	code, _ := request(t, app, ts, "POST", "/api/team-sync/address", map[string]string{"id": source.Feed, "url": "http://attacker.example:9877"})
	if code == 200 {
		t.Fatal("accepted hostname")
	}
}
