package web

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"sshdesk/internal/database"
	"sshdesk/internal/key"
	"sshdesk/internal/model"
	"sshdesk/internal/platform"
)

// uploadKey is available only on the protected management listener.
func (s *Server) uploadKey(w http.ResponseWriter, r *http.Request) {
	s.ops.Lock()
	defer s.ops.Unlock()
	var req struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Content []byte `json:"content"`
	}
	if err := decode(r, &req); err != nil {
		fail(w, err)
		return
	}
	defer clear(req.Content)
	if len(req.Content) == 0 || len(req.Content) > 1024*1024 {
		fail(w, errors.New("키는 1 MiB 이하의 비어 있지 않은 파일이어야 합니다"))
		return
	}
	dir := filepath.Join(s.Store.DataDir(), "uploaded-keys")
	if err := os.MkdirAll(dir, 0700); err != nil {
		fail(w, errors.New("키 저장 폴더를 만들 수 없습니다"))
		return
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		fail(w, errors.New("유효하지 않은 키 저장 폴더입니다"))
		return
	}
	if err = platform.SecureDirectory(dir); err != nil {
		fail(w, errors.New("키 저장 폴더의 접근 권한을 설정할 수 없습니다"))
		return
	}
	path, err := filepath.Abs(filepath.Join(dir, database.ID()))
	if err != nil {
		fail(w, err)
		return
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		fail(w, errors.New("키 파일을 저장할 수 없습니다"))
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(path)
		}
	}()
	_, err = f.Write(req.Content)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		fail(w, errors.New("키 파일을 저장할 수 없습니다"))
		return
	}
	if err = key.Validate(path); err != nil {
		fail(w, err)
		return
	}
	err = s.Store.Update(func(state *model.State) error {
		v := model.Key{ID: req.ID, Name: req.Name, Path: path}
		if v.ID == "" {
			v.ID = database.ID()
		}
		var e error
		state.Keys, e = database.Replace(state.Keys, req.ID, v, func(k model.Key) string { return k.ID })
		return e
	})
	if err != nil {
		fail(w, err)
		return
	}
	committed = true
	reply(w, map[string]bool{"ok": true})
}
