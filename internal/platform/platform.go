package platform

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func DataDir() (string, error) {
	var base string
	var e error
	if runtime.GOOS == "windows" {
		base = os.Getenv("LOCALAPPDATA")
	}
	if base == "" {
		base, e = os.UserConfigDir()
	}
	if e != nil {
		return "", e
	}
	return filepath.Join(base, "SSHDesk"), nil
}
func SSHFile(name string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", name)
}
func Path(raw string) (string, error) {
	if strings.ContainsAny(raw, "\x00\r\n") {
		return "", errors.New("invalid file path")
	}
	if strings.HasPrefix(raw, "~/") || strings.HasPrefix(raw, "~\\") {
		home, e := os.UserHomeDir()
		if e != nil {
			return "", e
		}
		raw = filepath.Join(home, raw[2:])
	}
	if !filepath.IsAbs(raw) {
		return "", errors.New("파일 경로는 절대 경로 또는 ~/ 형식이어야 합니다")
	}
	return filepath.Clean(raw), nil
}
func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if e := cmd.Start(); e != nil {
		return e
	}
	go cmd.Wait()
	return nil
}
