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
	s.shareFeed = ""
	s.sharePublic = nil
	s.sharePeers = nil
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
	peers := []map[string]string{}
	for ip, last := range s.sharePeers {
		if time.Since(last) < 5*time.Minute {
			peers = append(peers, map[string]string{"ip": ip, "last_seen": last.UTC().Format(time.RFC3339)})
		}
	}
	s.shareMu.Unlock()
	urls := []string{}
	for _, network := range shareNetworks() {
		if !network.IP.IsLoopback() {
			urls = append(urls, fmt.Sprintf("http://%s:%d", network.IP, SharePort))
		}
	}
	reply(w, map[string]any{"enabled": enabled, "urls": urls, "primary_url": preferredShareURL(urls), "port": SharePort, "selected_keys": selected, "prepared": prepared, "peers": peers})
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
		feed, public, err := provision.PrepareFeed(s.Store, req.KeyIDs)
		if err != nil {
			clear(payload)
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
		s.shareFeed = feed
		s.sharePublic = public
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
		if strings.HasPrefix(r.URL.Path, "/sync/") {
			s.serveFeed(w, r)
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
			payload, payloadErr := s.downloadPayload(r)
			if payloadErr != nil {
				fail(w, payloadErr)
				return
			}
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
			payload, payloadErr := s.downloadPayload(r)
			if payloadErr != nil {
				fail(w, payloadErr)
				return
			}
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
				payload, err := s.downloadPayload(r)
				if err != nil {
					fail(w, err)
					return
				}
				defer clear(payload)
				var bundle provision.Bundle
				if err = json.Unmarshal(payload, &bundle); err != nil {
					fail(w, err)
					return
				}
				defer func() {
					for _, key := range bundle.Keys {
						clear(key)
					}
				}()
				config = &Backup{Format: "sshdesk", Version: 1, ExportedAt: time.Now().UTC().Format(time.RFC3339), Hosts: bundle.State.Hosts, Keys: bundle.State.Keys, Tunnels: bundle.State.Tunnels, Services: bundle.State.Services}
				for i := range config.Keys {
					config.Keys[i].Path = "~/SSHDesk-team-key"
				}
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
<style>body{font:16px/1.7 'Segoe UI',sans-serif;background:#f5f7fa;color:#172b43;margin:0}main{max-width:700px;margin:10vh auto;padding:36px;background:white;border:1px solid #d6dee8;border-radius:16px}a{display:inline-block;background:#087c65;color:white;padding:14px 22px;margin:8px 8px 8px 0;border-radius:8px;text-decoration:none}small{color:#52647a}.download-option{border:1px solid #d6dee8;border-radius:12px;padding:20px;margin:16px 0}.download-option h3{margin:0 0 8px;font-size:18px}.recommended{border-color:#8ccbbb;background:#f2faf7}a.secondary{background:#e9eef4;color:#172b43}@media(max-width:600px){main{margin:16px;padding:20px}a{box-sizing:border-box;max-width:100%}}</style>
<main><h1>SSHDesk 팀 다운로드</h1><p>SSH 접속과 터널 설정을 팀원들과 공유하세요.</p>
{{if .Mac}}<p><strong>Mac에서 접속하셨습니다. 아래 macOS 설정 묶음을 사용하세요.</strong></p>{{end}}
<h2>macOS SSH 설정</h2>
<a href="/download/macos.zip" download>macOS 설정 묶음 다운로드 (.zip)</a>
<a href="/download/ssh-config" download>SSH 설정 텍스트</a>
<a href="/download/macos-guide" download>설치·접속 명령 안내</a>
<p>ZIP을 풀고 해당 폴더에서 <code>bash install.command</code>를 실행하세요. 기존 SSH 설정을 보존하며, README.txt에 호스트 접속·터널 연결 명령이 들어 있습니다.</p>
<p>{{if .Prepared}}선택한 개인 키가 ZIP에도 포함됩니다. 안전하게 보관하세요.{{else}}개인 키는 포함되지 않습니다. README.txt의 안내대로 본인의 키를 넣어 주세요.{{end}} Mac용 앱이 아닌 macOS 기본 SSH용 설정입니다.</p>
<h2>Windows</h2>
<div class="download-option recommended">
<h3>{{if .Prepared}}처음이라면 이 파일 하나로 시작하세요{{else}}먼저 프로그램을 받으세요{{end}}</h3>
<a href="/download/sshdesk.exe" download>{{if .Prepared}}프로그램 + 팀 접속 설정 받기 (추천){{else}}Windows 프로그램 받기{{end}}</a>
{{if .Prepared}}<p>프로그램, 서버 목록, 터널 설정, 서버 접속에 필요한 키가 함께 들어 있습니다. <strong>다운로드한 파일을 실행하면 자동으로 설정됩니다.</strong> 아래 설정파일은 따로 받지 않아도 됩니다.</p>
<p>원본 PC가 공유 중이면 서버 목록과 연결 설정의 변경 사항도 자동으로 받습니다.</p>
<small>접속용 개인 키가 포함되어 있으니 팀 외부에 전달하지 마세요. 처음 서버에 접속할 때 서버 확인이 필요할 수 있습니다.</small>
{{else}}<p>SSHDesk 프로그램만 들어 있습니다. 실행한 뒤 아래 설정파일을 가져오고, 본인의 서버 접속용 키를 등록하세요.</p>{{end}}
</div>
<div class="download-option">
<h3>이미 프로그램이 있고, 설정만 가져오고 싶다면</h3>
<a class="secondary" href="/download/configuration" download>서버 목록·연결 설정만 받기</a>
<p>서버 목록과 터널 연결 정보만 들어 있습니다. <strong>프로그램과 서버 접속용 키는 포함되지 않습니다.</strong></p>
<p>SSHDesk의 <strong>Settings → Configuration backup</strong>에서 파일을 선택하고 <strong>Preview → Import configuration</strong>을 눌러 가져오세요. 본인의 키 파일 경로를 확인해야 합니다.</p>
<small>이 파일을 가져오는 것만으로는 자동 갱신되지 않습니다. 원본 설정이 바뀌면 다시 받아 가져와야 합니다.</small>
</div>
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
