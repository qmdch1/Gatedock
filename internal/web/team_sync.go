package web

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"sshdesk/internal/model"
	"sshdesk/internal/provision"
)

func (s *Server) runTeamSync(ctx context.Context) {
	defer close(s.syncDone)
	s.pollTeam(ctx)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pollTeam(ctx)
		}
	}
}

func (s *Server) pollTeam(ctx context.Context) {
	s.syncRun.Lock()
	defer s.syncRun.Unlock()
	state, err := s.Store.Snapshot()
	if err != nil {
		return
	}
	for _, sub := range state.Team {
		if sub.Paused || ctx.Err() != nil {
			continue
		}
		incoming, fetchErr := provision.FetchFeed(ctx, sub.Source)
		if ctx.Err() != nil {
			return
		}
		s.ops.Lock()
		err := s.Store.Update(func(local *model.State) error {
			for i, current := range local.Team {
				if current.Paused || !provision.SameSource(current.Source, sub.Source) || current.Source.URL != sub.Source.URL {
					continue
				}
				if fetchErr != nil {
					local.Team[i].Error = fetchErr.Error()
					return nil
				}
				return provision.ApplyFeed(local, i, incoming)
			}
			return nil
		})
		if err != nil {
			_ = s.Store.Update(func(local *model.State) error {
				for i, v := range local.Team {
					if provision.SameSource(v.Source, sub.Source) {
						local.Team[i].Error = "변경 사항 충돌 · 기존 설정 유지: " + err.Error()
					}
				}
				return nil
			})
		}
		s.ops.Unlock()
	}
}

func teamStatus(state model.State) []map[string]any {
	result := []map[string]any{}
	for _, sub := range state.Team {
		result = append(result, map[string]any{"id": sub.Source.Feed, "url": sub.Source.URL, "items": sub.Items, "last_sync": sub.LastSync, "error": sub.Error, "paused": sub.Paused})
	}
	return result
}

func (s *Server) teamAction(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := decode(r, &request); err != nil {
		fail(w, err)
		return
	}
	if r.PathValue("action") == "refresh" {
		s.pollTeam(r.Context())
		reply(w, map[string]bool{"ok": true})
		return
	}
	err := s.Store.Update(func(state *model.State) error {
		for i, sub := range state.Team {
			if sub.Source.Feed != request.ID {
				continue
			}
			switch r.PathValue("action") {
			case "pause":
				state.Team[i].Paused = true
			case "resume":
				state.Team[i].Paused = false
			case "address":
				sub.Source.URL = strings.TrimRight(strings.TrimSpace(request.URL), "/")
				if err := provision.ValidateSource(sub.Source); err != nil {
					return err
				}
				state.Team[i].Source.URL = sub.Source.URL
			default:
				return errors.New("unknown sync action")
			}
			return nil
		}
		return errors.New("원본 구독을 찾을 수 없습니다")
	})
	if err != nil {
		fail(w, err)
		return
	}
	reply(w, map[string]bool{"ok": true})
}

func (s *Server) serveFeed(w http.ResponseWriter, r *http.Request) {
	signed, err := provision.SignFeed(s.Store, strings.TrimPrefix(r.URL.Path, "/sync/"), r.URL.Query().Get("nonce"))
	if err != nil {
		http.Error(w, "Feed unavailable", http.StatusServiceUnavailable)
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	s.shareMu.Lock()
	if s.sharePeers == nil {
		s.sharePeers = map[string]time.Time{}
	}
	for peer, last := range s.sharePeers {
		if time.Since(last) > 5*time.Minute {
			delete(s.sharePeers, peer)
		}
	}
	if len(s.sharePeers) < 128 || !s.sharePeers[ip].IsZero() {
		s.sharePeers[ip] = time.Now()
	}
	s.shareMu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.Method != "HEAD" {
		json.NewEncoder(w).Encode(signed)
	}
}

func (s *Server) downloadPayload(r *http.Request) ([]byte, error) {
	s.shareMu.Lock()
	payload := append([]byte{}, s.sharePayload...)
	feed := s.shareFeed
	public := append([]byte{}, s.sharePublic...)
	s.shareMu.Unlock()
	defer clear(payload)
	updated, err := provision.RefreshBundle(payload, s.Store)
	if err != nil || len(updated) == 0 {
		return updated, err
	}
	defer clear(updated)
	return provision.AttachSource(updated, model.TeamSource{URL: "http://" + r.Host, Feed: feed, PublicKey: public})
}
