package web

import (
	"net/http"
	"sshdesk/internal/platform"
)

func (s *Server) pickKeyFile(w http.ResponseWriter, r *http.Request) {
	if !s.pickerMu.TryLock() {
		http.Error(w, "파일 선택 창이 이미 열려 있습니다", http.StatusConflict)
		return
	}
	defer s.pickerMu.Unlock()
	pick := s.PickKeyFile
	if pick == nil {
		pick = platform.PickKeyFile
	}
	path, err := pick()
	if err != nil {
		fail(w, err)
		return
	}
	reply(w, map[string]any{"path": path, "cancelled": path == ""})
}
