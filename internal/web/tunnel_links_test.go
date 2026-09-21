package web

import (
	"sshdesk/internal/model"
	"testing"
)

func TestTunnelWebLinks(t *testing.T) {
	app, ts, _ := setup(t)
	payload := map[string]any{"name": "Web tunnel", "host_id": "host", "local_host": "127.0.0.1", "local_port": 18080, "remote_host": "127.0.0.1", "remote_port": 8080,
		"web_links": []model.Service{{Name: "Portal", URL: "http://127.0.0.1:18080"}, {Name: "Admin", URL: "http://127.0.0.1:18080/admin"}}}
	code, body := request(t, app, ts, "POST", "/api/tunnels", payload)
	if code != 200 {
		t.Fatalf("save %d %s", code, body)
	}
	state, _ := app.Store.Snapshot()
	if len(state.Tunnels) != 1 || len(state.Services) != 2 {
		t.Fatal("missing links")
	}
	id := state.Tunnels[0].ID
	if app.Tunnels.Active(id) {
		t.Fatal("save connected")
	}
	payload["id"] = id
	payload["web_links"] = []model.Service{{ID: state.Services[0].ID, Name: "Changed", URL: "http://127.0.0.1:19090"}}
	code, _ = request(t, app, ts, "POST", "/api/tunnels", payload)
	if code == 200 {
		t.Fatal("accepted wrong URL port")
	}
	state, _ = app.Store.Snapshot()
	if len(state.Services) != 2 || state.Services[0].Name != "Portal" {
		t.Fatal("rollback failed")
	}
	delete(payload, "web_links")
	code, _ = request(t, app, ts, "POST", "/api/tunnels", payload)
	if code != 200 {
		t.Fatal("legacy tunnel update failed")
	}
	state, _ = app.Store.Snapshot()
	if len(state.Services) != 2 {
		t.Fatal("legacy update lost links")
	}
	code, _ = request(t, app, ts, "DELETE", "/api/tunnels/"+id, nil)
	state, _ = app.Store.Snapshot()
	if code != 200 || len(state.Services) != 0 || len(state.Tunnels) != 0 {
		t.Fatal("delete failed")
	}
}
