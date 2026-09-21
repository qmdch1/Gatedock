package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"sshdesk/internal/database"
	"sshdesk/internal/model"
	"sshdesk/internal/platform"
)

// Backup contains configuration only: never key bytes, SSH file contents, or history.
type Backup struct {
	Format     string          `json:"format"`
	Version    int             `json:"version"`
	ExportedAt string          `json:"exported_at"`
	Hosts      []model.Host    `json:"hosts"`
	Keys       []model.Key     `json:"keys"`
	Tunnels    []model.Tunnel  `json:"tunnels"`
	Services   []model.Service `json:"services"`
	Settings   model.Settings  `json:"settings"`
}
type backupRequest struct {
	Backup         Backup `json:"backup"`
	ImportSettings bool   `json:"import_settings"`
}

func (s *Server) exportBackup(w http.ResponseWriter, r *http.Request) {
	state, e := s.Store.Snapshot()
	if e != nil {
		fail(w, e)
		return
	}
	b := Backup{Format: "sshdesk", Version: 1, ExportedAt: time.Now().UTC().Format(time.RFC3339), Hosts: state.Hosts, Keys: state.Keys, Tunnels: state.Tunnels, Services: state.Services, Settings: state.Settings}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="sshdesk-`+time.Now().UTC().Format("20060102-150405")+`.sshdesk.json"`)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(b)
}
func mergeRecords[T any](existing, incoming []T, id func(T) string) ([]T, int, error) {
	result := append([]T{}, existing...)
	byID := map[string]T{}
	for _, v := range existing {
		byID[id(v)] = v
	}
	seen := map[string]bool{}
	added := 0
	for _, v := range incoming {
		k := id(v)
		if k == "" || len(k) > 128 || strings.ContainsAny(k, "/\\\x00\r\n") || seen[k] {
			return nil, 0, errors.New("백업에 비어 있거나 중복된/잘못된 ID가 있습니다")
		}
		seen[k] = true
		if old, ok := byID[k]; ok {
			if !reflect.DeepEqual(old, v) {
				return nil, 0, fmt.Errorf("기존 항목과 ID가 충돌합니다: %s. 기존 설정을 덮어쓰지 않았습니다", k)
			}
			continue
		}
		result = append(result, v)
		added++
	}
	return result, added, nil
}
func mergeBackup(state *model.State, req backupRequest) (map[string]int, error) {
	b := req.Backup
	if b.Format != "sshdesk" || b.Version != 1 {
		return nil, errors.New("지원하지 않는 SSHDesk 백업 형식/버전입니다")
	}
	counts := map[string]int{}
	var e error
	if state.Keys, counts["keys"], e = mergeRecords(state.Keys, b.Keys, func(v model.Key) string { return v.ID }); e != nil {
		return nil, e
	}
	if state.Hosts, counts["hosts"], e = mergeRecords(state.Hosts, b.Hosts, func(v model.Host) string { return v.ID }); e != nil {
		return nil, e
	}
	if state.Tunnels, counts["tunnels"], e = mergeRecords(state.Tunnels, b.Tunnels, func(v model.Tunnel) string { return v.ID }); e != nil {
		return nil, e
	}
	if state.Services, counts["services"], e = mergeRecords(state.Services, b.Services, func(v model.Service) string { return v.ID }); e != nil {
		return nil, e
	}
	// Validate path syntax only. Import never reads a key or another local file.
	for _, k := range b.Keys {
		if _, e = platform.Path(k.Path); e != nil {
			return nil, fmt.Errorf("키 %s: %w", k.Name, e)
		}
	}
	if req.ImportSettings {
		if _, e = platform.Path(b.Settings.KnownHosts); e != nil {
			return nil, e
		}
		if _, e = platform.Path(b.Settings.SSHConfig); e != nil {
			return nil, e
		}
		state.Settings = b.Settings
	}
	if e = database.Validate(*state); e != nil {
		return nil, e
	}
	return counts, nil
}
func (s *Server) importBackup(w http.ResponseWriter, r *http.Request) {
	s.ops.Lock()
	defer s.ops.Unlock()
	var req backupRequest
	if e := decode(r, &req); e != nil {
		fail(w, e)
		return
	}
	var counts map[string]int
	var e error
	if r.PathValue("action") == "preview" {
		var state model.State
		state, e = s.Store.Snapshot()
		if e == nil {
			counts, e = mergeBackup(&state, req)
		}
	} else if r.PathValue("action") == "apply" {
		e = s.Store.Update(func(state *model.State) error { var err error; counts, err = mergeBackup(state, req); return err })
	} else {
		e = errors.New("unknown backup action")
	}
	if e != nil {
		fail(w, e)
		return
	}
	reply(w, map[string]any{"added": counts, "warnings": []string{"키 원문/SSH config/known_hosts 파일은 포함되지 않습니다. 이 PC의 파일 경로를 확인하세요.", "주소·사용자명·키 경로와 서버 지문을 확인한 뒤 연결하세요. 가져오기는 SSH 연결이나 터널을 시작하지 않습니다."}})
}
