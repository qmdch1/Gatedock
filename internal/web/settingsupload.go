package web

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh/knownhosts"
	"sshdesk/internal/database"
	"sshdesk/internal/model"
	"sshdesk/internal/platform"
	"sshdesk/internal/sshconfig"
)

// Browser-selected settings are copied only when the user saves the form.
func (s *Server) uploadSettings(w http.ResponseWriter, r *http.Request) {
	s.ops.Lock()
	defer s.ops.Unlock()
	var req struct {
		model.Settings
		Config []byte `json:"config_content"`
		Hosts  []byte `json:"hosts_content"`
	}
	if err := decode(r, &req); err != nil {
		fail(w, err)
		return
	}
	defer clear(req.Config)
	defer clear(req.Hosts)
	if len(req.Config)+len(req.Hosts) == 0 || len(req.Config)+len(req.Hosts) > 1024*1024 {
		fail(w, errors.New("선택한 설정 파일은 합계 1 MiB 이하이어야 합니다"))
		return
	}
	dir := filepath.Join(s.Store.DataDir(), "uploaded-settings")
	created := []string{}
	committed := false
	defer func() {
		if !committed {
			for _, path := range created {
				_ = os.Remove(path)
			}
		}
	}()
	save := func(content []byte, current string, isConfig bool) (string, error) {
		if content == nil {
			return platform.Path(current)
		}
		if len(content) == 0 {
			return "", errors.New("비어 있는 설정 파일입니다")
		}
		if isConfig {
			if _, err := sshconfig.Parse(bytes.NewReader(content)); err != nil {
				return "", errors.New("SSH config 형식을 확인하세요")
			}
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", errors.New("설정 저장 폴더를 만들 수 없습니다")
		}
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("유효하지 않은 설정 저장 폴더입니다")
		}
		if err = platform.SecureDirectory(dir); err != nil {
			return "", errors.New("설정 폴더 권한을 설정할 수 없습니다")
		}
		path, err := filepath.Abs(filepath.Join(dir, database.ID()))
		if err != nil {
			return "", err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return "", errors.New("설정 파일을 저장할 수 없습니다")
		}
		created = append(created, path)
		_, err = f.Write(content)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			return "", errors.New("설정 파일을 저장할 수 없습니다")
		}
		if !isConfig {
			if _, err = knownhosts.New(path); err != nil {
				return "", errors.New("known_hosts 형식을 확인하세요")
			}
		}
		return path, nil
	}
	var err error
	req.SSHConfig, err = save(req.Config, req.SSHConfig, true)
	if err == nil {
		req.KnownHosts, err = save(req.Hosts, req.KnownHosts, false)
	}
	if err == nil {
		err = s.Store.Update(func(state *model.State) error { state.Settings = req.Settings; return nil })
	}
	if err != nil {
		fail(w, err)
		return
	}
	committed = true
	reply(w, req.Settings)
}
