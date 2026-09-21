package provision

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"sshdesk/internal/database"
	"sshdesk/internal/model"
)

type MacPackage struct {
	Config, Instructions string
	Archive              []byte
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func comment(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ", "\x00", "").Replace(s)
}

// MacExport builds native OpenSSH files; it never reads keys unless already
// explicitly included in the prepared team bundle.
func MacExport(state model.State, payload []byte) (MacPackage, error) {
	result := MacPackage{}
	keys := map[string][]byte{}
	if len(payload) > 0 {
		var bundle Bundle
		if len(payload) > MaxPayload {
			return result, fmt.Errorf("팀 배포본 크기 초과")
		}
		if err := json.Unmarshal(payload, &bundle); err != nil {
			return result, err
		}
		state = bundle.State
		keys = bundle.Keys
		defer func() {
			for _, key := range keys {
				clear(key)
			}
		}()
	}
	// No sender-specific settings, history or key paths are exported.
	state.Settings = model.Settings{}
	state.History = nil
	state.Keys = append([]model.Key(nil), state.Keys...)
	for i := range state.Keys {
		state.Keys[i].Path = "recipient-key"
	}
	if err := database.Validate(state); err != nil {
		return result, err
	}
	raw, _ := json.Marshal(state)
	fingerprintInput := append(raw, payload...)
	digest := sha256.Sum256(fingerprintInput)
	clear(fingerprintInput)
	tag := fmt.Sprintf("%x", digest[:6])
	base := "~/.ssh/sshdesk/" + tag
	names := map[string]string{}
	aliases := map[string]string{}
	for i, k := range state.Keys {
		names[k.ID] = fmt.Sprintf("key-%02d", i+1)
	}
	for i, h := range state.Hosts {
		aliases[h.ID] = fmt.Sprintf("sshdesk-%s-host-%02d", tag, i+1)
	}
	var config, guide, install strings.Builder
	config.WriteString("# SSHDesk macOS OpenSSH configuration\n# Use with ssh -F; existing ~/.ssh/config is preserved.\n\n")
	fmt.Fprintf(&guide, "SSHDesk macOS 설정 안내\n\n1. ZIP 파일을 풀고 sshdesk-macos 폴더를 엽니다.\n2. 터미널에서 해당 폴더로 이동한 뒤 실행합니다:\n   bash install.command\n3. 아래 연결 명령을 복사해 사용하세요.\n\n설치 위치: %s\n기존 ~/.ssh/config와 known_hosts는 변경하지 않습니다. 설치는 SSH에 접속하지 않습니다.\n서버의 처음 접속 지문은 관리자에게 확인하세요. 자동 신뢰하지 않습니다.\n\n", base)
	if len(keys) > 0 {
		guide.WriteString("이 ZIP에는 선택한 개인 키가 포함됩니다. 팀 외부나 공개 저장소에 전달하지 마세요.\n\n")
	} else {
		guide.WriteString("개인 키는 포함되지 않았습니다. 아래 파일명으로 본인의 키를 keys 폴더에 넣은 다음 설치하세요.\n\n")
	}
	for _, k := range state.Keys {
		fmt.Fprintf(&guide, "키: %s → keys/%s\n", comment(k.Name), names[k.ID])
	}
	guide.WriteString("\nSSH 접속 명령\n")
	hostBlock := func(h model.Host, alias string) error {
		fmt.Fprintf(&config, "# %s\nHost %s\n  HostName %s\n  Port %d\n  User %s\n  IdentityFile %s\n  IdentitiesOnly yes\n  ServerAliveInterval 30\n  ServerAliveCountMax 3\n", comment(h.Name), alias, h.Address, h.Port, strconv.Quote(strings.ReplaceAll(h.Username, "%", "%%")), strconv.Quote(base+"/"+names[h.KeyID]))
		chain, err := state.Chain(h.ID)
		if err != nil {
			return err
		}
		hops := []string{}
		for _, hop := range chain[:len(chain)-1] {
			hops = append(hops, aliases[hop.ID])
		}
		if len(hops) > 0 {
			fmt.Fprintf(&config, "  ProxyJump %s\n", strings.Join(hops, ","))
		}
		return nil
	}
	for _, h := range state.Hosts {
		if err := hostBlock(h, aliases[h.ID]); err != nil {
			return result, err
		}
		config.WriteString("\n")
		fmt.Fprintf(&guide, "# %s\nssh -F \"$HOME/.ssh/sshdesk/%s/config\" %s\n", comment(h.Name), tag, aliases[h.ID])
		if h.Fingerprint != "" {
			fmt.Fprintf(&guide, "확인할 서버 지문: %s\n", h.Fingerprint)
		}
	}
	guide.WriteString("\n터널 시작 명령 (종료: Ctrl+C, 자동 재연결은 제공하지 않습니다)\n")
	for i, t := range state.Tunnels {
		h, err := state.Host(t.HostID)
		if err != nil {
			return result, err
		}
		alias := fmt.Sprintf("sshdesk-%s-tunnel-%02d", tag, i+1)
		if err = hostBlock(h, alias); err != nil {
			return result, err
		}
		fmt.Fprintf(&config, "  LocalForward 127.0.0.1:%d %s\n  ExitOnForwardFailure yes\n\n", t.LocalPort, net.JoinHostPort(t.RemoteHost, strconv.Itoa(t.RemotePort)))
		fmt.Fprintf(&guide, "# %s\nssh -F \"$HOME/.ssh/sshdesk/%s/config\" -N -T %s\n", comment(t.Name), tag, alias)
		for _, s := range state.Services {
			if s.TunnelID == t.ID {
				fmt.Fprintf(&guide, "# 터널 연결 후 별도 터미널에서 %s 열기\nopen %s\n", comment(s.Name), shellQuote(s.URL))
			}
		}
	}
	install.WriteString("#!/bin/bash\nset -eu\numask 077\nsource_dir=\"$(cd -- \"$(dirname -- \"$0\")\" && pwd)\"\n")
	fmt.Fprintf(&install, "target=\"$HOME/.ssh/sshdesk/%s\"\n", tag)
	install.WriteString("if [ -L \"$target\" ]; then echo '설치 경로가 심볼릭 링크입니다. 중단합니다.'; exit 1; fi\n")
	for _, k := range state.Keys {
		n := names[k.ID]
		fmt.Fprintf(&install, "if [ ! -f \"$source_dir/keys/%s\" ]; then echo '%s 키 파일을 keys 폴더에 넣으세요.'; exit 1; fi\n", n, n)
	}
	// Check all collisions before writing. Never overwrite recipient keys/config.
	install.WriteString("if [ -e \"$target/config\" ] && ! cmp -s \"$source_dir/ssh_config\" \"$target/config\"; then echo '기존 설정과 충돌합니다.'; exit 1; fi\n")
	for _, k := range state.Keys {
		n := names[k.ID]
		fmt.Fprintf(&install, "if [ -e \"$target/%s\" ] && ! cmp -s \"$source_dir/keys/%s\" \"$target/%s\"; then echo '기존 키와 충돌합니다.'; exit 1; fi\n", n, n, n)
	}
	install.WriteString("mkdir -p \"$target\"\nchmod 700 \"$HOME/.ssh\" \"$HOME/.ssh/sshdesk\" \"$target\"\nif [ ! -e \"$target/config\" ]; then cp \"$source_dir/ssh_config\" \"$target/config\"; fi\nchmod 600 \"$target/config\"\n")
	for _, k := range state.Keys {
		n := names[k.ID]
		fmt.Fprintf(&install, "if [ ! -e \"$target/%s\" ]; then cp \"$source_dir/keys/%s\" \"$target/%s\"; fi\nchmod 600 \"$target/%s\"\n", n, n, n, n)
	}
	install.WriteString("echo '설정 완료. README.txt의 SSH/터널 명령을 사용하세요.'\n")
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	add := func(name string, data []byte, mode os.FileMode) error {
		header := &zip.FileHeader{Name: "sshdesk-macos/" + name, Method: zip.Deflate}
		header.SetMode(mode)
		file, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		_, err = file.Write(data)
		return err
	}
	for _, file := range []struct {
		name, data string
		mode       os.FileMode
	}{{"ssh_config", config.String(), 0600}, {"README.txt", guide.String(), 0600}, {"install.command", install.String(), 0700}} {
		if err := add(file.name, []byte(file.data), file.mode); err != nil {
			return result, err
		}
	}
	for _, k := range state.Keys {
		if data, ok := keys[k.ID]; ok {
			if err := add("keys/"+names[k.ID], data, 0600); err != nil {
				return result, err
			}
		}
	}
	if len(keys) == 0 {
		if err := add("keys/README.txt", []byte("상위 README.txt의 대응표에 따라 본인의 SSH 개인 키를 이 폴더에 넣으세요.\n"), 0600); err != nil {
			return result, err
		}
	}
	if err := archive.Close(); err != nil {
		return result, err
	}
	return MacPackage{Config: config.String(), Instructions: guide.String(), Archive: buffer.Bytes()}, nil
}
