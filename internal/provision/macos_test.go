package provision

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacPackageAndOpenSSHConfig(t *testing.T) {
	state := fixture(t)
	state.Hosts[0].JumpID = "other-host"
	payload, err := Create(state, []string{"chosen", "other"})
	if err != nil {
		t.Fatal(err)
	}
	defer clear(payload)
	pack, err := MacExport(state, payload)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(pack.Archive)
	if strings.Contains(pack.Config, state.Keys[0].Path) || strings.Contains(pack.Config, "StrictHostKeyChecking no") || !strings.Contains(pack.Config, "ProxyJump sshdesk-") || !strings.Contains(pack.Config, "LocalForward 127.0.0.1:18080 127.0.0.1:8080") {
		t.Fatal("unsafe or incomplete config")
	}
	archive, err := zip.NewReader(bytes.NewReader(pack.Archive), int64(len(pack.Archive)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	defer func() {
		for _, data := range files {
			clear(data)
		}
	}()
	for _, file := range archive.File {
		if strings.Contains(file.Name, "..") || !strings.HasPrefix(file.Name, "sshdesk-macos/") {
			t.Fatal("invalid archive path")
		}
		r, _ := file.Open()
		data, _ := io.ReadAll(r)
		r.Close()
		files[file.Name] = data
		if strings.Contains(file.Name, "/keys/key-") && file.Mode().Perm() != 0600 {
			t.Fatal("key permissions")
		}
	}
	if len(files) != 5 {
		t.Fatalf("expected config, instructions, installer, 2 keys; got %d", len(files))
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "install.command")
	os.WriteFile(script, files["sshdesk-macos/install.command"], 0700)
	if bash, err := exec.LookPath("bash"); err == nil {
		if output, err := exec.Command(bash, "-n", script).CombinedOutput(); err != nil {
			t.Fatalf("installer syntax: %s", output)
		}
	}
	config := filepath.Join(dir, "config")
	os.WriteFile(config, []byte(pack.Config), 0600)
	alias := ""
	for _, line := range strings.Split(pack.Config, "\n") {
		if strings.HasPrefix(line, "Host ") {
			alias = strings.TrimPrefix(line, "Host ")
			break
		}
	}
	if ssh, err := exec.LookPath("ssh"); err == nil {
		output, err := exec.Command(ssh, "-G", "-F", config, alias).CombinedOutput()
		if err != nil {
			t.Fatalf("OpenSSH config parse: %s", output)
		}
		if !bytes.Contains(output, []byte("hostname 192.0.2.1")) || !bytes.Contains(output, []byte("proxyjump sshdesk-")) {
			t.Fatal("wrong OpenSSH route")
		}
	}
}

func TestMacTextDoesNotReadKeys(t *testing.T) {
	state := fixture(t)
	state.Keys[0].Path = "/nonexistent/not-read"
	pack, err := MacExport(state, nil)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(pack.Archive), int64(len(pack.Archive)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range archive.File {
		if strings.Contains(file.Name, "keys/key-") {
			t.Fatal("unselected key included")
		}
	}
	if !strings.Contains(pack.Instructions, "개인 키는 포함되지 않았습니다") || strings.Contains(pack.Config, "/nonexistent/") {
		t.Fatal("incorrect guidance or sender path exposed")
	}
}
