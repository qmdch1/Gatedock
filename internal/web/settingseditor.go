package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"unicode/utf8"

	"golang.org/x/crypto/ssh/knownhosts"
	"sshdesk/internal/platform"
	"sshdesk/internal/sshconfig"
)

func settingsText(b []byte) bool {
	return utf8.Valid(b) && !bytes.ContainsRune(b, 0) && !bytes.Contains(b, []byte("PRIVATE KEY"))
}
func readSettingsText(path string) ([]byte, string, os.FileMode, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return []byte{}, "missing", 0600, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1024*1024 {
		return nil, "", 0, errors.New("1 MiB 이하의 일반 텍스트 파일만 편집할 수 있습니다. 경로와 권한을 확인하세요")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "", 0, errors.New("파일을 열 수 없습니다. 경로와 권한을 확인하세요")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if err != nil || len(b) > 1024*1024 || !settingsText(b) {
		return nil, "", 0, errors.New("텍스트 설정 파일만 편집할 수 있습니다. 개인 키는 열 수 없습니다")
	}
	sum := sha256.Sum256(b)
	return b, hex.EncodeToString(sum[:]), info.Mode().Perm(), nil
}

func (s *Server) settingsEditor(w http.ResponseWriter, r *http.Request) {
	s.ops.Lock()
	defer s.ops.Unlock()
	var req struct {
		Kind     string `json:"kind"`
		Path     string `json:"path"`
		Revision string `json:"revision"`
		Content  string `json:"content"`
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
	var path string
	switch req.Kind {
	case "ssh_config":
		path = state.Settings.SSHConfig
	case "known_hosts":
		path = state.Settings.KnownHosts
	default:
		fail(w, errors.New("지원하지 않는 설정 파일입니다"))
		return
	}
	path, err = platform.Path(path)
	if err != nil {
		fail(w, err)
		return
	}
	content, revision, mode, err := readSettingsText(path)
	if err != nil {
		fail(w, err)
		return
	}
	switch r.PathValue("action") {
	case "read":
		reply(w, map[string]any{"path": path, "content": string(content), "revision": revision})
		return
	case "save":
		if req.Path != path || req.Revision != revision {
			http.Error(w, "다른 곳에서 파일 또는 경로가 변경되었습니다. 다시 열어 확인하세요", http.StatusConflict)
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
	b := []byte(req.Content)
	if len(b) > 1024*1024 || !settingsText(b) {
		fail(w, errors.New("개인 키가 아닌 1 MiB 이하의 UTF-8 텍스트만 저장할 수 있습니다"))
		return
	}
	if req.Kind == "ssh_config" {
		if _, err = sshconfig.Parse(bytes.NewReader(b)); err != nil {
			fail(w, errors.New("SSH config 형식을 확인하세요"))
			return
		}
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".sshdesk-edit-*")
	if err != nil {
		fail(w, errors.New("파일을 저장할 수 없습니다. 폴더 쓰기 권한을 확인하세요. Docker의 읽기 전용 파일은 파일 선택으로 가져온 뒤 편집하세요"))
		return
	}
	temp := f.Name()
	defer os.Remove(temp)
	_, err = f.Write(b)
	if err == nil {
		err = f.Chmod(mode)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		fail(w, errors.New("파일 저장에 실패했습니다. 원본은 유지됩니다"))
		return
	}
	if req.Kind == "known_hosts" {
		if _, err = knownhosts.New(temp); err != nil {
			fail(w, errors.New("known_hosts 형식을 확인하세요. 원본은 유지됩니다"))
			return
		}
	}
	// Recheck after preparing the replacement to catch external edits during validation.
	_, latest, _, err := readSettingsText(path)
	if err != nil || latest != revision {
		http.Error(w, "파일이 변경되었습니다. 다시 열어 확인하세요", http.StatusConflict)
		return
	}
	if err = os.Rename(temp, path); err != nil {
		fail(w, errors.New("파일을 교체할 수 없습니다. 쓰기 권한을 확인하세요. 원본은 유지됩니다"))
		return
	}
	sum := sha256.Sum256(b)
	reply(w, map[string]string{"path": path, "revision": hex.EncodeToString(sum[:])})
}
