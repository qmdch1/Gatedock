package key

import (
	"errors"
	"fmt"
	"golang.org/x/crypto/ssh"
	"io"
	"os"
	"sshdesk/internal/platform"
)

// PassphraseProvider is intentionally session-scoped. Implementations must never persist secrets.
type PassphraseProvider interface {
	Passphrase(path string) ([]byte, error)
}

func Read(path string) ([]byte, error) {
	p, e := platform.Path(path)
	if e != nil {
		return nil, e
	}
	f, e := os.Open(p)
	if e != nil {
		return nil, errors.New("키 파일을 읽을 수 없습니다. 경로와 권한을 확인하세요")
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > 1024*1024 {
		return nil, errors.New("키는 1 MiB 이하의 일반 파일이어야 합니다")
	}
	return io.ReadAll(io.LimitReader(f, 1024*1024+1))
}
func Signer(path string, provider PassphraseProvider) (ssh.Signer, error) {
	b, e := Read(path)
	if e != nil {
		return nil, e
	}
	defer clear(b)
	s, e := ssh.ParsePrivateKey(b)
	var missing *ssh.PassphraseMissingError
	if errors.As(e, &missing) {
		if provider == nil {
			return nil, errors.New("암호화된 키입니다. MVP는 passphrase 입력을 지원하지 않습니다")
		}
		pass, e := provider.Passphrase(path)
		if e != nil {
			return nil, e
		}
		defer clear(pass)
		return ssh.ParsePrivateKeyWithPassphrase(b, pass)
	}
	if e != nil {
		return nil, fmt.Errorf("지원되는 PEM/RSA/ED25519 private key 형식이 아닙니다")
	}
	return s, nil
}
func Validate(path string) error { _, e := Signer(path, nil); return e }
