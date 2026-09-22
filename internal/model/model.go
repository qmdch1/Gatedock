package model

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type Key struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}
type Host struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Environment string `json:"environment"`
	Type        string `json:"type"`
	Address     string `json:"address"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	KeyID       string `json:"key_id"`
	JumpID      string `json:"jump_id"`
	Description string `json:"description"`
	Fingerprint string `json:"fingerprint"`
}
type Tunnel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	LocalHost     string `json:"local_host"`
	LocalPort     int    `json:"local_port"`
	RemoteHost    string `json:"remote_host"`
	RemotePort    int    `json:"remote_port"`
	HostID        string `json:"host_id"`
	AutoReconnect bool   `json:"auto_reconnect"`
	Description   string `json:"description"`
}
type Service struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Environment string `json:"environment"`
	TunnelID    string `json:"tunnel_id"`
	URL         string `json:"url"`
	Description string `json:"description"`
}
type History struct {
	ID       string `json:"id"`
	HostID   string `json:"host_id"`
	HostName string `json:"host_name"`
	Kind     string `json:"kind"`
	Status   string `json:"status"`
	Error    string `json:"error"`
	At       string `json:"at"`
}
type Settings struct {
	KnownHosts string `json:"known_hosts"`
	SSHConfig  string `json:"ssh_config"`
}
type State struct {
	Team     []TeamSubscription `json:"-"`
	Hosts    []Host             `json:"hosts"`
	Keys     []Key              `json:"keys"`
	Tunnels  []Tunnel           `json:"tunnels"`
	Services []Service          `json:"services"`
	History  []History          `json:"history"`
	Settings Settings           `json:"settings"`
}

// Team metadata is local-only and never part of configuration exports.
type TeamSource struct {
	URL       string `json:"url"`
	Feed      string `json:"feed"`
	PublicKey []byte `json:"public_key"`
}
type TeamSubscription struct {
	Source   TeamSource        `json:"source"`
	Items    map[string]string `json:"items"` // kind/id -> current or missing
	LastSync string            `json:"last_sync"`
	Error    string            `json:"error"`
	Paused   bool              `json:"paused"`
}

func Env(s string) bool { return s == "LOCAL" || s == "DEV" || s == "STG" || s == "PROD" }
func Port(p int) bool   { return p > 0 && p <= 65535 }
func Address(s string) bool {
	if s == "" || len(s) > 253 || strings.ContainsAny(s, " \t\r\n/\\@?#%") {
		return false
	}
	if net.ParseIP(s) != nil {
		return true
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
func Name(s string) bool {
	return strings.TrimSpace(s) != "" && len(s) <= 160 && !strings.ContainsAny(s, "\x00\r\n")
}
func ValidateHost(h Host) error {
	if !Name(h.Name) || !Env(h.Environment) || !Address(h.Address) || !Port(h.Port) || !Name(h.Username) || strings.ContainsAny(h.Username, " \t@/\\") {
		return errors.New("이름, 환경, 주소, SSH 포트, 사용자명을 확인하세요")
	}
	if h.Type != "Bastion" && h.Type != "Server" && h.Type != "Database" && h.Type != "Other" {
		return errors.New("올바른 Host Type을 선택하세요")
	}
	if h.KeyID == "" {
		return errors.New("SSH Key가 필요합니다")
	}
	if h.ID != "" && h.JumpID == h.ID {
		return errors.New("자기 자신을 Jump Host로 지정할 수 없습니다")
	}
	if h.Fingerprint != "" && (!strings.HasPrefix(h.Fingerprint, "SHA256:") || len(h.Fingerprint) != 50) {
		return errors.New("서버 지문은 SHA256: 형식이어야 합니다")
	}
	return nil
}
func ValidateTunnel(t Tunnel) error {
	if !Name(t.Name) || t.LocalHost != "127.0.0.1" || !Port(t.LocalPort) || !Address(t.RemoteHost) || !Port(t.RemotePort) || t.HostID == "" {
		return errors.New("터널 이름, 포트, SSH Host를 확인하세요. Local Host는 127.0.0.1만 허용합니다")
	}
	return nil
}
func ValidateService(s Service, t Tunnel) error {
	if !Name(s.Name) || !Env(s.Environment) || s.TunnelID == "" {
		return errors.New("서비스 이름, 환경, 터널이 필요합니다")
	}
	u, e := url.Parse(s.URL)
	if e != nil || u == nil || u.User != nil || u.Opaque != "" || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() != "127.0.0.1" || strings.ContainsAny(s.URL, "\r\n\x00") {
		return errors.New("서비스 URL은 http(s)://127.0.0.1 주소만 허용합니다")
	}
	p := u.Port()
	if p == "" {
		if u.Scheme == "https" {
			p = "443"
		} else {
			p = "80"
		}
	}
	if p != strconv.Itoa(t.LocalPort) {
		return errors.New("서비스 URL 포트는 선택한 터널의 Local Port와 일치해야 합니다")
	}
	return nil
}
func (s State) Host(id string) (Host, error) {
	for _, h := range s.Hosts {
		if h.ID == id {
			return h, nil
		}
	}
	return Host{}, fmt.Errorf("host not found: %s", id)
}
func (s State) Key(id string) (Key, error) {
	for _, k := range s.Keys {
		if k.ID == id {
			return k, nil
		}
	}
	return Key{}, errors.New("SSH Key를 찾을 수 없습니다")
}
func (s State) Tunnel(id string) (Tunnel, error) {
	for _, t := range s.Tunnels {
		if t.ID == id {
			return t, nil
		}
	}
	return Tunnel{}, errors.New("터널을 찾을 수 없습니다")
}

// Chain returns the route in connection order, and rejects cycles or unreasonable depth.
func (s State) Chain(id string) ([]Host, error) {
	var out []Host
	seen := map[string]bool{}
	for id != "" {
		if seen[id] || len(out) >= 16 {
			return nil, errors.New("Jump Host 순환 또는 최대 16 hop 초과")
		}
		seen[id] = true
		h, e := s.Host(id)
		if e != nil {
			return nil, e
		}
		out = append([]Host{h}, out...)
		id = h.JumpID
	}
	return out, nil
}
