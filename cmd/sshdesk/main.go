package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sshdesk/internal/database"
	"sshdesk/internal/model"
	"sshdesk/internal/platform"
	"sshdesk/internal/sshclient"
	"sshdesk/internal/tunnel"
	"sshdesk/internal/web"
	"time"
)

func main() {
	if e := run(); e != nil {
		log.Print(e)
		os.Exit(1)
	}
}
func run() error {
	dir, e := platform.DataDir()
	if e != nil {
		return e
	}
	data := flag.String("data-dir", dir, "application data directory")
	port := flag.Int("port", 9876, "local HTTP port")
	noBrowser := flag.Bool("no-browser", false, "disable automatic browser opening")
	flag.Parse()
	if !model.Port(*port) {
		return errors.New("invalid HTTP port")
	}
	if !filepath.IsAbs(*data) {
		return errors.New("data-dir must be absolute")
	}
	// Acquire the port before opening the database; a second instance fails without touching it.
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	listener, e := net.Listen("tcp4", addr)
	if e != nil {
		return fmt.Errorf("%s 사용 중입니다. 실행 중인 SSHDesk를 확인하세요: %w", addr, e)
	}
	defer listener.Close()
	store, e := database.Open(filepath.Join(*data, "sshdesk.db"), model.Settings{KnownHosts: platform.SSHFile("known_hosts"), SSHConfig: platform.SSHFile("config")})
	if e != nil {
		return e
	}
	defer store.Close()
	ssh := &sshclient.Manager{Store: store}
	tm := tunnel.New(ssh)
	origin := "http://" + addr
	app, e := web.New(store, ssh, tm, origin)
	if e != nil {
		return e
	}
	defer app.Close()
	server := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 * 1024}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	client := http.Client{Timeout: 3 * time.Second}
	res, e := client.Get(origin + "/health")
	if e != nil {
		server.Close()
		return e
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		server.Close()
		return errors.New("health check failed")
	}
	log.Printf("SSHDesk ready: %s | data: %s", origin, *data)
	if !*noBrowser {
		if e = platform.OpenBrowser(origin); e != nil {
			log.Printf("브라우저에서 %s 를 여세요", origin)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	select {
	case e = <-done:
		if errors.Is(e, http.ErrServerClosed) {
			return nil
		}
		return e
	case <-ctx.Done():
		app.Close()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
