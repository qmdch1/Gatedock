// Package provision handles explicitly selected, local-only team installers.
package provision

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"golang.org/x/crypto/ssh"
	"sshdesk/internal/database"
	"sshdesk/internal/key"
	"sshdesk/internal/model"
)

const magic = "SSHDesk-Team-Bundle-v1"
const MaxPayload = 16 << 20

type Bundle struct {
	Source  *model.TeamSource `json:"source,omitempty"`
	Version int               `json:"version"`
	State   model.State       `json:"state"`
	Keys    map[string][]byte `json:"key_material"`
}

// Select does not read private keys. Jump dependencies must be selected explicitly.
func Select(source model.State, ids []string) (model.State, error) {
	result := model.State{}
	selected := map[string]bool{}
	for _, id := range ids {
		k, err := source.Key(id)
		if err != nil {
			return result, err
		}
		if selected[id] {
			continue
		}
		selected[id] = true
		result.Keys = append(result.Keys, k)
	}
	if len(selected) == 0 {
		return result, errors.New("공유할 키를 선택하세요")
	}
	hosts := map[string]bool{}
	for _, h := range source.Hosts {
		if selected[h.KeyID] {
			hosts[h.ID] = true
			result.Hosts = append(result.Hosts, h)
		}
	}
	for _, h := range result.Hosts {
		chain, err := source.Chain(h.ID)
		if err != nil {
			return result, err
		}
		for _, hop := range chain {
			if !selected[hop.KeyID] {
				k, _ := source.Key(hop.KeyID)
				return result, fmt.Errorf("%s 경유 연결에 필요한 키도 선택하세요: %s", h.Name, k.Name)
			}
		}
	}
	tunnels := map[string]bool{}
	for _, t := range source.Tunnels {
		if hosts[t.HostID] {
			result.Tunnels = append(result.Tunnels, t)
			tunnels[t.ID] = true
		}
	}
	for _, s := range source.Services {
		if tunnels[s.TunnelID] {
			result.Services = append(result.Services, s)
		}
	}
	return result, database.Validate(result)
}

func Create(source model.State, ids []string) ([]byte, error) {
	state, err := Select(source, ids)
	if err != nil {
		return nil, err
	}
	bundle := Bundle{Version: 1, State: state, Keys: map[string][]byte{}}
	defer func() {
		for _, b := range bundle.Keys {
			clear(b)
		}
	}()
	for i, k := range state.Keys {
		b, err := key.Read(k.Path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", k.Name, err)
		}
		if _, err = ssh.ParsePrivateKey(b); err != nil {
			clear(b)
			return nil, fmt.Errorf("%s: 사용할 수 있는 암호 없는 개인 키가 필요합니다", k.Name)
		}
		bundle.Keys[k.ID] = b
		bundle.State.Keys[i].Path = "bundled-key"
	}
	data, err := json.Marshal(bundle)
	if len(data) > MaxPayload {
		clear(data)
		return nil, errors.New("팀 배포본 설정은 16 MiB 이하여야 합니다")
	}
	return data, err
}

// BaseSize strips any previously attached package so it can never be re-shared accidentally.
func BaseSize(file *os.File) (int64, error) {
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	size := info.Size()
	footer := int64(len(magic) + 8)
	if size < footer {
		return size, nil
	}
	tail := make([]byte, footer)
	if _, err = file.ReadAt(tail, size-footer); err != nil {
		return 0, err
	}
	if string(tail[8:]) != magic {
		return size, nil
	}
	length := binary.LittleEndian.Uint64(tail[:8])
	if length > MaxPayload || length > uint64(size-footer) {
		return 0, errors.New("잘못된 팀 배포본입니다")
	}
	return size - footer - int64(length), nil
}

func WriteExecutable(w io.Writer, file *os.File, payload []byte) error {
	size, err := BaseSize(file)
	if err != nil {
		return err
	}
	if _, err = io.Copy(w, io.NewSectionReader(file, 0, size)); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	if _, err = w.Write(payload); err != nil {
		return err
	}
	if err = binary.Write(w, binary.LittleEndian, uint64(len(payload))); err != nil {
		return err
	}
	_, err = io.WriteString(w, magic)
	return err
}

func ReadExecutable(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	size, err := BaseSize(f)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if size == info.Size() {
		return nil, nil
	}
	return io.ReadAll(io.NewSectionReader(f, size, info.Size()-size-int64(len(magic)+8)))
}

func merge[T any](old, incoming []T, id func(T) string) ([]T, error) {
	records := map[string]T{}
	for _, v := range old {
		records[id(v)] = v
	}
	for _, v := range incoming {
		if existing, ok := records[id(v)]; ok {
			if !reflect.DeepEqual(existing, v) {
				return nil, errors.New("기존 설정과 배포본이 충돌합니다. 별도의 -data-dir로 실행하세요")
			}
		} else {
			old = append(old, v)
			records[id(v)] = v
		}
	}
	return old, nil
}

func Install(store *database.Store, dir string, payload []byte) (bool, error) {
	if len(payload) == 0 {
		return false, nil
	}
	if len(payload) > MaxPayload {
		return false, errors.New("팀 배포본 크기 초과")
	}
	digest := sha256.Sum256(payload)
	tag := hex.EncodeToString(digest[:])
	root := filepath.Join(dir, "team-bundles", tag)
	marker := filepath.Join(root, "installed")
	if _, err := os.Stat(marker); err == nil {
		return false, nil
	}
	var b Bundle
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&b); err != nil {
		return false, errors.New("팀 배포본 해석 실패")
	}
	defer func() {
		for _, v := range b.Keys {
			clear(v)
		}
	}()
	if b.Version != 1 || len(b.Keys) != len(b.State.Keys) {
		return false, errors.New("팀 배포본 형식 오류")
	}
	if b.Source != nil {
		if err := ValidateSource(*b.Source); err != nil {
			return false, err
		}
		// A master opening its own installer must not subscribe to itself.
		secret, err := store.LocalValue("team-signing-key", nil)
		self := err == nil && len(secret) == ed25519.PrivateKeySize && bytes.Equal(ed25519.PrivateKey(secret).Public().(ed25519.PublicKey), b.Source.PublicKey)
		clear(secret)
		if self {
			return false, nil
		}
	}
	if err := database.Validate(b.State); err != nil {
		return false, err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return false, err
	}
	if err := secureDirectory(root); err != nil {
		return false, err
	}
	created := []string{}
	installed := false
	defer func() {
		if !installed {
			for _, p := range created {
				os.Remove(p)
			}
		}
	}()
	for i, k := range b.State.Keys {
		raw, ok := b.Keys[k.ID]
		if !ok || len(raw) > 1<<20 {
			return false, errors.New("키 데이터 누락 또는 크기 초과")
		}
		if _, err := ssh.ParsePrivateKey(raw); err != nil {
			return false, errors.New("배포본 키 형식 오류")
		}
		// A fresh download may contain the same key at a new package path.
		if b.Source != nil {
			local, err := store.Snapshot()
			if err != nil {
				return false, err
			}
			if existing, err := local.Key(k.ID); err == nil {
				old, err := key.Read(existing.Path)
				equal := err == nil && bytes.Equal(old, raw)
				clear(old)
				if equal {
					b.State.Keys[i].Path = existing.Path
					continue
				}
			}
		}
		// Never use a sender-provided filename or ID as a filesystem path.
		path := filepath.Join(root, fmt.Sprintf("key-%d", i))
		existing, err := os.ReadFile(path)
		if err == nil {
			if !bytes.Equal(existing, raw) {
				clear(existing)
				return false, errors.New("기존 키 파일 충돌")
			}
			clear(existing)
		} else {
			if !os.IsNotExist(err) {
				return false, err
			}
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return false, err
			}
			created = append(created, path)
			_, err = f.Write(raw)
			closeErr := f.Close()
			if err != nil {
				return false, err
			}
			if closeErr != nil {
				return false, closeErr
			}
		}
		b.State.Keys[i].Path = path
	}
	err := store.Update(func(s *model.State) error {
		var err error
		if b.Source != nil {
			for i, sub := range s.Team {
				if !bytes.Equal(sub.Source.PublicKey, b.Source.PublicKey) {
					continue
				}
				// Re-downloading from this master also permits explicitly selected new keys.
				for _, k := range b.State.Keys {
					if _, e := s.Key(k.ID); e == nil && sub.Items["keys/"+k.ID] == "" {
						return errors.New("개인 키 ID 충돌")
					}
					s.Keys, err = syncRecords(s.Keys, []model.Key{k}, "keys", sub.Items, func(v model.Key) string { return v.ID })
					if err != nil {
						return err
					}
					s.Team[i].Items["keys/"+k.ID] = "current"
				}
				s.Team[i].Source = *b.Source
				return ApplyFeed(s, i, b.State)
			}
		}
		if s.Keys, err = merge(s.Keys, b.State.Keys, func(v model.Key) string { return v.ID }); err != nil {
			return err
		}
		if s.Hosts, err = merge(s.Hosts, b.State.Hosts, func(v model.Host) string { return v.ID }); err != nil {
			return err
		}
		if s.Tunnels, err = merge(s.Tunnels, b.State.Tunnels, func(v model.Tunnel) string { return v.ID }); err != nil {
			return err
		}
		if s.Services, err = merge(s.Services, b.State.Services, func(v model.Service) string { return v.ID }); err != nil {
			return err
		}
		if b.Source != nil {
			return registerSource(s, *b.Source, b.State)
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	installed = true // DB is committed; retain keys even if writing the marker fails.
	if err = os.WriteFile(marker, []byte("installed\n"), 0600); err != nil {
		return true, err
	}
	return true, nil
}
