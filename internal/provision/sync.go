package provision

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"sshdesk/internal/database"
	"sshdesk/internal/model"
)

// Each feed is permanently scoped to the keys explicitly chosen by its owner.
func PrepareFeed(store *database.Store, ids []string) (string, []byte, error) {
	secret, err := store.LocalValue("team-signing-key", func() ([]byte, error) {
		_, key, err := ed25519.GenerateKey(rand.Reader)
		return key, err
	})
	if err != nil {
		return "", nil, err
	}
	defer clear(secret)
	if len(secret) != ed25519.PrivateKeySize {
		return "", nil, errors.New("invalid sharing identity")
	}
	feed := database.ID() + database.ID()
	_, err = store.LocalValue("feed/"+feed, func() ([]byte, error) { return json.Marshal(ids) })
	return feed, append([]byte{}, ed25519.PrivateKey(secret).Public().(ed25519.PublicKey)...), err
}

type SyncMessage struct {
	Feed  string      `json:"feed"`
	Nonce string      `json:"nonce"`
	State model.State `json:"state"`
}
type SignedSync struct {
	Message   []byte `json:"message"`
	Signature []byte `json:"signature"`
}

func validHex(s string) bool { b, err := hex.DecodeString(s); return err == nil && len(b) == 32 }

func ValidateSource(source model.TeamSource) error {
	u, err := url.Parse(source.URL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Port() != "9877" {
		return errors.New("잘못된 원본 PC 주소")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || ip.To4() == nil || !(ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()) {
		return errors.New("원본은 로컬망 IPv4 주소여야 합니다")
	}
	if !validHex(source.Feed) || len(source.PublicKey) != ed25519.PublicKeySize {
		return errors.New("잘못된 동기화 정보")
	}
	return nil
}

func SignFeed(store *database.Store, feed, nonce string) (SignedSync, error) {
	var out SignedSync
	if !validHex(feed) || !validHex(nonce) {
		return out, errors.New("invalid feed request")
	}
	data, err := store.LocalValue("feed/"+feed, nil)
	if err != nil {
		return out, errors.New("공유 구독을 찾을 수 없습니다")
	}
	var ids []string
	if err = json.Unmarshal(data, &ids); err != nil {
		return out, err
	}
	state, err := store.Snapshot()
	if err != nil {
		return out, err
	}
	// Missing keys indicate removed scope, not a failed or empty network response.
	var existing []string
	for _, id := range ids {
		if _, err := state.Key(id); err == nil {
			existing = append(existing, id)
		}
	}
	scoped := model.State{}
	if len(existing) > 0 {
		scoped, err = Select(state, existing)
		if err != nil {
			return out, err
		}
	}
	for i := range scoped.Keys {
		scoped.Keys[i].Path = "local-key-preserved"
	}
	out.Message, err = json.Marshal(SyncMessage{Feed: feed, Nonce: nonce, State: scoped})
	if err != nil {
		return out, err
	}
	secret, err := store.LocalValue("team-signing-key", nil)
	if err != nil {
		return out, err
	}
	defer clear(secret)
	if len(secret) != ed25519.PrivateKeySize {
		return out, errors.New("invalid sharing identity")
	}
	out.Signature = ed25519.Sign(ed25519.PrivateKey(secret), out.Message)
	return out, nil
}

func FetchFeed(ctx context.Context, source model.TeamSource) (model.State, error) {
	if err := ValidateSource(source); err != nil {
		return model.State{}, err
	}
	nonce := database.ID() + database.ID()
	request, err := http.NewRequestWithContext(ctx, "GET", source.URL+"/sync/"+source.Feed+"?nonce="+nonce, nil)
	if err != nil {
		return model.State{}, err
	}
	// Literal local IP only, no proxy or redirects to other hosts.
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return model.State{}, errors.New("원본 PC에 연결할 수 없습니다. 마지막 설정을 유지합니다")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return model.State{}, fmt.Errorf("원본 응답 %d · 마지막 설정 유지", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, MaxPayload+1))
	if err != nil || len(raw) > MaxPayload {
		return model.State{}, errors.New("동기화 응답 크기/읽기 오류")
	}
	return VerifyFeed(source, nonce, raw)
}

func VerifyFeed(source model.TeamSource, nonce string, raw []byte) (model.State, error) {
	var signed SignedSync
	if json.Unmarshal(raw, &signed) != nil || len(source.PublicKey) != ed25519.PublicKeySize || !ed25519.Verify(source.PublicKey, signed.Message, signed.Signature) {
		return model.State{}, errors.New("원본 서명 검증 실패 · 기존 설정 유지")
	}
	var message SyncMessage
	if json.Unmarshal(signed.Message, &message) != nil || message.Feed != source.Feed || message.Nonce != nonce {
		return model.State{}, errors.New("동기화 응답 불일치")
	}
	if err := database.Validate(message.State); err != nil {
		return model.State{}, err
	}
	return message.State, nil
}

func track[T any](items map[string]string, kind string, values []T, id func(T) string) {
	for _, v := range values {
		items[kind+"/"+id(v)] = "current"
	}
}
func Track(state model.State) map[string]string {
	items := map[string]string{}
	track(items, "hosts", state.Hosts, func(v model.Host) string { return v.ID })
	track(items, "keys", state.Keys, func(v model.Key) string { return v.ID })
	track(items, "tunnels", state.Tunnels, func(v model.Tunnel) string { return v.ID })
	track(items, "services", state.Services, func(v model.Service) string { return v.ID })
	return items
}

func syncRecords[T any](old []T, incoming []T, kind string, owned map[string]string, id func(T) string) ([]T, error) {
	seen := map[string]bool{}
	for _, v := range incoming {
		key := id(v)
		if key == "" || len(key) > 128 || strings.ContainsAny(key, "/\\\x00\r\n") || seen[key] {
			return nil, errors.New("동기화 ID 오류")
		}
		seen[key] = true
		found := false
		for i, local := range old {
			if id(local) != key {
				continue
			}
			if _, managed := owned[kind+"/"+key]; !managed {
				return nil, errors.New("개인 설정과 ID 충돌 · 기존 설정 유지")
			}
			old[i] = v
			found = true
			break
		}
		if !found {
			old = append(old, v)
		}
	}
	return old, nil
}

// ApplyFeed changes saved settings only. Live SSH sessions and tunnels are untouched.
func ApplyFeed(state *model.State, index int, incoming model.State) error {
	sub := &state.Team[index]
	owned := sub.Items
	for i, key := range incoming.Keys {
		local, err := state.Key(key.ID)
		if err != nil || owned["keys/"+key.ID] == "" {
			return errors.New("새 SSH 키는 새 배포본으로 받아야 합니다")
		}
		incoming.Keys[i].Path = local.Path
	}
	var err error
	if state.Keys, err = syncRecords(state.Keys, incoming.Keys, "keys", owned, func(v model.Key) string { return v.ID }); err != nil {
		return err
	}
	if state.Hosts, err = syncRecords(state.Hosts, incoming.Hosts, "hosts", owned, func(v model.Host) string { return v.ID }); err != nil {
		return err
	}
	if state.Tunnels, err = syncRecords(state.Tunnels, incoming.Tunnels, "tunnels", owned, func(v model.Tunnel) string { return v.ID }); err != nil {
		return err
	}
	if state.Services, err = syncRecords(state.Services, incoming.Services, "services", owned, func(v model.Service) string { return v.ID }); err != nil {
		return err
	}
	current := Track(incoming)
	for item := range owned {
		if current[item] == "" {
			current[item] = "missing"
		}
	}
	// Keep removed web links usable if their retained parent tunnel changes its local port.
	for i, service := range state.Services {
		if current["services/"+service.ID] != "missing" {
			continue
		}
		tunnel, err := state.Tunnel(service.TunnelID)
		if err != nil {
			return err
		}
		u, err := url.Parse(service.URL)
		if err != nil {
			return err
		}
		u.Host = net.JoinHostPort("127.0.0.1", strconv.Itoa(tunnel.LocalPort))
		state.Services[i].URL = u.String()
	}
	sub.Items = current
	sub.LastSync = time.Now().UTC().Format(time.RFC3339)
	sub.Error = ""
	return nil
}

func SameSource(a, b model.TeamSource) bool {
	return a.Feed == b.Feed && bytes.Equal(a.PublicKey, b.PublicKey)
}

func AttachSource(payload []byte, source model.TeamSource) ([]byte, error) {
	if err := ValidateSource(source); err != nil {
		return nil, err
	}
	var bundle Bundle
	if err := json.Unmarshal(payload, &bundle); err != nil {
		return nil, err
	}
	defer func() {
		for _, key := range bundle.Keys {
			clear(key)
		}
	}()
	bundle.Source = &source
	return marshalBundle(bundle)
}

func marshalBundle(bundle Bundle) ([]byte, error) {
	data, err := json.Marshal(bundle)
	if len(data) > MaxPayload {
		clear(data)
		return nil, errors.New("팀 배포본 크기 초과")
	}
	return data, err
}

// RefreshBundle updates configuration without re-reading or broadening the selected key material.
func RefreshBundle(payload []byte, store *database.Store) ([]byte, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	var bundle Bundle
	if err := json.Unmarshal(payload, &bundle); err != nil {
		return nil, err
	}
	originalKeys := bundle.Keys
	defer func() {
		for _, key := range originalKeys {
			clear(key)
		}
	}()
	state, err := store.Snapshot()
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, key := range bundle.State.Keys {
		if _, e := state.Key(key.ID); e == nil {
			ids = append(ids, key.ID)
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("공유할 키가 없습니다. 배포본을 다시 준비하세요")
	}
	bundle.State, err = Select(state, ids)
	if err != nil {
		return nil, err
	}
	retained := map[string][]byte{}
	for i, key := range bundle.State.Keys {
		bundle.State.Keys[i].Path = "bundled-key"
		retained[key.ID] = bundle.Keys[key.ID]
	}
	bundle.Keys = retained
	return marshalBundle(bundle)
}

// Adopt source metadata only after a successful, conflict-free initial installation.
func registerSource(state *model.State, source model.TeamSource, incoming model.State) error {
	for i, sub := range state.Team {
		if SameSource(sub.Source, source) {
			state.Team[i].Source.URL = source.URL
			return nil
		}
		for item := range Track(incoming) {
			if sub.Items[item] != "" {
				return errors.New("다른 원본이 관리하는 설정과 충돌합니다")
			}
		}
	}
	state.Team = append(state.Team, model.TeamSubscription{Source: source, Items: Track(incoming)})
	return nil
}

// EqualConfig is useful when detecting whether a poll actually changed configuration.
func EqualConfig(a, b model.State) bool {
	return reflect.DeepEqual(a.Hosts, b.Hosts) && reflect.DeepEqual(a.Keys, b.Keys) && reflect.DeepEqual(a.Tunnels, b.Tunnels) && reflect.DeepEqual(a.Services, b.Services)
}
