package model

import "testing"

func validHost() Host {
	return Host{ID: "h", Name: "test", Environment: "DEV", Type: "Server", Address: "10.0.0.1", Port: 22, Username: "dev", KeyID: "k"}
}
func TestHostValidation(t *testing.T) {
	h := validHost()
	if e := ValidateHost(h); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*Host){func(h *Host) { h.Port = 0 }, func(h *Host) { h.Port = 65536 }, func(h *Host) { h.Environment = "TEST" }, func(h *Host) { h.Address = "host;whoami" }, func(h *Host) { h.Username = "x y" }, func(h *Host) { h.JumpID = h.ID }, func(h *Host) { h.KeyID = "" }, func(h *Host) { h.Fingerprint = "insecure" }} {
		h = validHost()
		change(&h)
		if ValidateHost(h) == nil {
			t.Errorf("accepted invalid host: %+v", h)
		}
	}
}
func TestTunnelValidation(t *testing.T) {
	v := Tunnel{Name: "API", LocalHost: "127.0.0.1", LocalPort: 18080, RemoteHost: "internal.local", RemotePort: 80, HostID: "h"}
	if e := ValidateTunnel(v); e != nil {
		t.Fatal(e)
	}
	for _, bind := range []string{"0.0.0.0", "localhost", "::", "192.168.0.1"} {
		v.LocalHost = bind
		if ValidateTunnel(v) == nil {
			t.Fatal("external bind allowed")
		}
	}
	v.LocalHost = "127.0.0.1"
	v.RemotePort = -1
	if ValidateTunnel(v) == nil {
		t.Fatal("invalid remote port allowed")
	}
}
func TestJumpConfiguration(t *testing.T) {
	a, b, c := validHost(), validHost(), validHost()
	a.ID = "a"
	b.ID = "b"
	b.JumpID = "a"
	c.ID = "c"
	c.JumpID = "b"
	s := State{Hosts: []Host{a, b, c}}
	chain, e := s.Chain("c")
	if e != nil || len(chain) != 3 || chain[0].ID != "a" || chain[2].ID != "c" {
		t.Fatalf("%v %v", chain, e)
	}
	s.Hosts[0].JumpID = "c"
	if _, e = s.Chain("c"); e == nil {
		t.Fatal("cycle accepted")
	}
	s.Hosts[0].JumpID = "missing"
	if _, e = s.Chain("c"); e == nil {
		t.Fatal("missing jump accepted")
	}
}
func TestServiceURLValidation(t *testing.T) {
	tunnel := Tunnel{LocalPort: 18080}
	v := Service{Name: "app", Environment: "STG", TunnelID: "t", URL: "http://127.0.0.1:18080/path?q=x"}
	if e := ValidateService(v, tunnel); e != nil {
		t.Fatal(e)
	}
	for _, url := range []string{"javascript:alert(1)", "file:///etc/passwd", "http://evil.test:18080", "http://127.0.0.1:8080", "http://user:pass@127.0.0.1:18080", "http://127.0.0.1:18080\r\nX:1", "http://127.0.0.1.evil:18080"} {
		v.URL = url
		if ValidateService(v, tunnel) == nil {
			t.Errorf("allowed %s", url)
		}
	}
}
