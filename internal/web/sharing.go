package web

import (
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"runtime"
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
	s.shareMu.Unlock()
	urls := []string{}
	for _, network := range shareNetworks() {
		if !network.IP.IsLoopback() {
			urls = append(urls, fmt.Sprintf("http://%s:%d", network.IP, SharePort))
		}
	}
	reply(w, map[string]any{"enabled": enabled, "urls": urls, "port": SharePort})
}

func (s *Server) sharingAction(w http.ResponseWriter, r *http.Request) {
	switch r.PathValue("action") {
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
			sharePage.Execute(w, nil)
		case "/download/sshdesk.exe":
			if runtime.GOOS != "windows" {
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
			http.ServeFile(w, r, exe)
		case "/download/configuration":
			s.exportBackup(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

var sharePage = template.Must(template.New("share").Parse(strings.TrimSpace(`<!doctype html>
<html lang="ko"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>SSHDesk 팀 다운로드</title>
<style>body{font:16px/1.7 'Segoe UI',sans-serif;background:#f5f7fa;color:#172b43;margin:0}main{max-width:700px;margin:10vh auto;padding:36px;background:white;border:1px solid #d6dee8;border-radius:16px}a{display:inline-block;background:#087c65;color:white;padding:14px 22px;margin:8px 8px 8px 0;border-radius:8px;text-decoration:none}small{color:#52647a}</style>
<main><h1>SSHDesk 팀 다운로드</h1><p>SSH 접속과 터널 설정을 팀원들과 공유하세요.</p>
<a href="/download/sshdesk.exe" download>Windows 실행파일 다운로드</a>
<a href="/download/configuration" download>팀 설정파일 다운로드</a>
<ol><li>실행파일을 다운로드하고 내 PC에서 실행합니다.</li><li>Settings → Configuration backup에서 팀 설정파일을 가져옵니다.</li><li>내 SSH 키 경로와 계정을 확인한 뒤 연결합니다.</li></ol>
<small>설정에는 호스트·계정·키 경로가 포함됩니다. 개인 키 파일은 공유하지 않습니다. 이 페이지에서는 서버에 접속하거나 설정을 수정할 수 없습니다.</small></main></html>`)))
