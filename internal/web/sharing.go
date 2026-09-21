package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"runtime"
	"sshdesk/internal/provision"
	"strings"
	"time"
)

const SharePort = 9877

func (s *Server) stopSharing() {
	s.shareMu.Lock()
	defer s.shareMu.Unlock()
	if s.shareServer != nil {
		s.shareServer.Close()
		s.shareServer = nil
	}
	clear(s.sharePayload)
	s.sharePayload = nil
	s.shareSelection = nil
	s.shareConfig = nil
}

func (s *Server) StartSharing() error {
	s.shareMu.Lock()
	defer s.shareMu.Unlock()
	if s.shareServer != nil {
		return nil
	}
	listener, err := net.Listen("tcp4", fmt.Sprintf(":%d", SharePort))
	if err != nil {
		return err
	}
	server := &http.Server{Handler: s.shareHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	s.shareServer = server
	go func() {
		server.Serve(listener)
		s.shareMu.Lock()
		defer s.shareMu.Unlock()
		if s.shareServer == server {
			s.shareServer = nil
		}
	}()
	return nil
}

func shareNetworks() []*net.IPNet {
	var nets []*net.IPNet
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addresses, _ := iface.Addrs()
		for _, address := range addresses {
			if network, ok := address.(*net.IPNet); ok && network.IP.To4() != nil {
				nets = append(nets, network)
			}
		}
	}
	return nets
}

func (s *Server) sharingStatus(w http.ResponseWriter, r *http.Request) {
	s.shareMu.Lock()
	enabled := s.shareServer != nil
	selected := append([]string{}, s.shareSelection...)
	prepared := len(s.sharePayload) > 0
	s.shareMu.Unlock()
	urls := []string{}
	for _, network := range shareNetworks() {
		if !network.IP.IsLoopback() {
			urls = append(urls, fmt.Sprintf("http://%s:%d", network.IP, SharePort))
		}
	}
	reply(w, map[string]any{"enabled": enabled, "urls": urls, "primary_url": preferredShareURL(urls), "port": SharePort, "selected_keys": selected, "prepared": prepared})
}

func (s *Server) sharingAction(w http.ResponseWriter, r *http.Request) {
	switch r.PathValue("action") {
	case "prepare":
		var req struct {
			KeyIDs []string `json:"key_ids"`
		}
		if err := decode(r, &req); err != nil {
			fail(w, err)
			return
		}
		state, err := s.Store.Snapshot()
		if err != nil {
			fail(w, err)
			return
		}
		selected, err := provision.Select(state, req.KeyIDs)
		if err != nil {
			fail(w, err)
			return
		}
		payload, err := provision.Create(state, req.KeyIDs)
		if err != nil {
			fail(w, err)
			return
		}
		for i := range selected.Keys {
			selected.Keys[i].Path = "~/SSHDesk-team-key"
		}
		backup := &Backup{Format: "sshdesk", Version: 1, ExportedAt: time.Now().UTC().Format(time.RFC3339), Hosts: selected.Hosts, Keys: selected.Keys, Tunnels: selected.Tunnels, Services: selected.Services}
		s.shareMu.Lock()
		clear(s.sharePayload)
		s.sharePayload = payload
		s.shareSelection = append([]string{}, req.KeyIDs...)
		s.shareConfig = backup
		s.shareMu.Unlock()
	case "start":
		if err := s.StartSharing(); err != nil {
			fail(w, err)
			return
		}
	case "stop":
		s.stopSharing()
	default:
		http.NotFound(w, r)
		return
	}
	s.sharingStatus(w, r)
}

func (s *Server) shareHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'")
		remote, _, err := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(remote)
		allowed := ip != nil && ip.IsLoopback()
		for _, network := range shareNetworks() {
			if network.Contains(ip) {
				allowed = true
			}
		}
		if err != nil || !allowed {
			http.Error(w, "Local network only", 403)
			return
		}
		host, port, err := net.SplitHostPort(r.Host)
		validHost := false
		for _, network := range shareNetworks() {
			if network.IP.Equal(net.ParseIP(host)) {
				validHost = true
			}
		}
		if err != nil || port != fmt.Sprint(SharePort) || !validHost {
			http.Error(w, "Invalid Host", 403)
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "Cross-site request blocked", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			http.Error(w, "Invalid Origin", 403)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "Read only", 405)
			return
		}
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			s.shareMu.Lock()
			prepared := len(s.sharePayload) > 0
			s.shareMu.Unlock()
			sharePage.Execute(w, map[string]bool{"Prepared": prepared, "Mac": strings.Contains(r.UserAgent(), "Macintosh")})
		case "/download/macos.zip", "/download/ssh-config", "/download/macos-guide":
			s.shareMu.Lock()
			payload := append([]byte{}, s.sharePayload...)
			s.shareMu.Unlock()
			defer clear(payload)
			state, err := s.Store.Snapshot()
			if err != nil {
				fail(w, err)
				return
			}
			pack, err := provision.MacExport(state, payload)
			if err != nil {
				fail(w, err)
				return
			}
			defer clear(pack.Archive)
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			switch r.URL.Path {
			case "/download/macos.zip":
				w.Header().Set("Content-Type", "application/zip")
				w.Header().Set("Content-Disposition", `attachment; filename="sshdesk-macos.zip"`)
				if r.Method != "HEAD" {
					w.Write(pack.Archive)
				}
			case "/download/ssh-config":
				w.Header().Set("Content-Disposition", `attachment; filename="ssh_config"`)
				if r.Method != "HEAD" {
					fmt.Fprint(w, pack.Config)
				}
			case "/download/macos-guide":
				w.Header().Set("Content-Disposition", `attachment; filename="macos-setup.txt"`)
				if r.Method != "HEAD" {
					fmt.Fprint(w, pack.Instructions)
				}
			}
		case "/download/sshdesk.exe":
			s.shareMu.Lock()
			payload := append([]byte{}, s.sharePayload...)
			s.shareMu.Unlock()
			defer clear(payload)
			if runtime.GOOS != "windows" {
				if len(payload) > 0 {
					http.Error(w, "키 포함 실행파일 배포는 Windows에서 실행하세요", 503)
					return
				}
				http.Redirect(w, r, "https://github.com/qmdch1/Gatedock/releases/latest/download/sshdesk.exe", 302)
				return
			}
			exe, err := os.Executable()
			if err != nil {
				http.Error(w, "Download unavailable", 500)
				return
			}
			w.Header().Set("Content-Disposition", `attachment; filename="sshdesk.exe"`)
			w.Header().Set("Content-Type", "application/octet-stream")
			file, err := os.Open(exe)
			if err != nil {
				http.Error(w, "Download unavailable", 500)
				return
			}
			defer file.Close()
			if _, err = provision.BaseSize(file); err != nil {
				http.Error(w, "Invalid executable", 500)
				return
			}
			if r.Method != "HEAD" {
				provision.WriteExecutable(w, file, payload)
			}
		case "/download/configuration":
			s.shareMu.Lock()
			config := s.shareConfig
			s.shareMu.Unlock()
			if config != nil {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("Content-Disposition", `attachment; filename="sshdesk-team.sshdesk.json"`)
				json.NewEncoder(w).Encode(config)
			} else {
				s.exportBackup(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})
}

var sharePage = template.Must(template.New("share").Parse(strings.TrimSpace(`<!doctype html>
<html lang="ko"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>SSHDesk 팀 다운로드</title>
<style>body{font:16px/1.7 'Segoe UI',sans-serif;background:#f5f7fa;color:#172b43;margin:0}main{max-width:700px;margin:10vh auto;padding:36px;background:white;border:1px solid #d6dee8;border-radius:16px}a{display:inline-block;background:#087c65;color:white;padding:14px 22px;margin:8px 8px 8px 0;border-radius:8px;text-decoration:none}small{color:#52647a}</style>
<main><h1>SSHDesk 팀 다운로드</h1><p>SSH 접속과 터널 설정을 팀원들과 공유하세요.</p>
{{if .Mac}}<p><strong>Mac에서 접속하셨습니다. 아래 macOS 설정 묶음을 사용하세요.</strong></p>{{end}}
<h2>macOS SSH 설정</h2>
<a href="/download/macos.zip" download>macOS 설정 묶음 다운로드 (.zip)</a>
<a href="/download/ssh-config" download>SSH 설정 텍스트</a>
<a href="/download/macos-guide" download>설치·접속 명령 안내</a>
<p>ZIP을 풀고 해당 폴더에서 <code>bash install.command</code>를 실행하세요. 기존 SSH 설정을 보존하며, README.txt에 호스트 접속·터널 연결 명령이 들어 있습니다.</p>
<p>{{if .Prepared}}선택한 개인 키가 ZIP에도 포함됩니다. 안전하게 보관하세요.{{else}}개인 키는 포함되지 않습니다. README.txt의 안내대로 본인의 키를 넣어 주세요.{{end}} Mac용 앱이 아닌 macOS 기본 SSH용 설정입니다.</p>
<h2>Windows</h2>
<a href="/download/sshdesk.exe" download>{{if .Prepared}}키·호스트 포함 실행파일 다운로드{{else}}Windows 실행파일 다운로드{{end}}</a>
<a href="/download/configuration" download>팀 설정파일 다운로드</a>
{{if .Prepared}}<p>처음 실행하면 포함된 키·호스트·터널·웹 바로가기가 내 PC에 자동 등록됩니다. 기존 설정은 덮어쓰지 않습니다.</p><p>이 실행파일에는 개인 키가 들어 있습니다. 팀 외부나 공개 저장소에 전달하지 마세요. 서버 지문/known_hosts는 별도로 확인해야 할 수 있습니다.</p>
{{else}}<ol><li>실행파일을 다운로드하고 내 PC에서 실행합니다.</li><li>Settings → Configuration backup에서 팀 설정파일을 가져옵니다.</li><li>내 SSH 키 경로와 계정을 확인한 뒤 연결합니다.</li></ol>{{end}}
<small>JSON 설정파일에는 개인 키가 포함되지 않습니다. 이 페이지에서는 서버에 접속하거나 설정을 수정할 수 없습니다.</small></main></html>`)))

// A UDP route lookup selects the outbound interface without sending packets.
func preferredShareURL(urls []string) string {
	conn, err := net.Dial("udp4", "192.0.2.1:9")
	if err == nil {
		address := conn.LocalAddr().(*net.UDPAddr).IP.String()
		conn.Close()
		candidate := fmt.Sprintf("http://%s:%d", address, SharePort)
		for _, url := range urls {
			if url == candidate {
				return url
			}
		}
	}
	if len(urls) > 0 {
		return urls[0]
	}
	return ""
}
