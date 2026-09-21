package web

import (
	"sshdesk/internal/model"
	"testing"
)

func TestServiceCreatesTunnelAtomically(t *testing.T) {
	app, ts, _ := setup(t)
	requestBody := map[string]any{
		"name": "Local app", "environment": "DEV", "url": "http://127.0.0.1:18080",
		"new_tunnel": model.Tunnel{HostID: "host", LocalPort: 18080, RemoteHost: "127.0.0.1", RemotePort: 8080},
	}
	code, body := request(t, app, ts, "POST", "/api/services", requestBody)
	if code != 200 {
		t.Fatalf("save: %d %s", code, body)
	}
	state, err := app.Store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Services) != 1 || len(state.Tunnels) != 1 || state.Services[0].TunnelID != state.Tunnels[0].ID {
		t.Fatal("service/tunnel not saved together")
	}
	if app.Tunnels.Active(state.Tunnels[0].ID) {
		t.Fatal("saving started a connection")
	}
	requestBody["url"] = "http://127.0.0.1:19090"
	code, _ = request(t, app, ts, "POST", "/api/services", requestBody)
	if code == 200 {
		t.Fatal("accepted mismatched URL")
	}
	state, _ = app.Store.Snapshot()
	if len(state.Services) != 1 || len(state.Tunnels) != 1 {
		t.Fatal("failed save left orphan tunnel")
	}
	requestBody["url"] = "http://127.0.0.1:18080"
	requestBody["new_tunnel"] = model.Tunnel{HostID: "missing", LocalPort: 18080, RemoteHost: "127.0.0.1", RemotePort: 8080}
	code, _ = request(t, app, ts, "POST", "/api/services", requestBody)
	if code == 200 {
		t.Fatal("accepted nonexistent host")
	}
}
