package key

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"golang.org/x/crypto/ssh"
	"os"
	"path/filepath"
	"sshdesk/internal/testssh"
	"testing"
)

func TestPrivateKeyValidation(t *testing.T) {
	_, path := testssh.Key(t)
	if e := Validate(path); e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{"relative.pem", filepath.Join(t.TempDir(), "missing"), t.TempDir(), "bad\x00path"} {
		if Validate(p) == nil {
			t.Errorf("accepted %q", p)
		}
	}
	bad := filepath.Join(t.TempDir(), "bad.pem")
	_ = os.WriteFile(bad, []byte("not a key"), 0600)
	if Validate(bad) == nil {
		t.Fatal("accepted invalid key")
	}
	t.Run("RSA PEM", func(t *testing.T) {
		private, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "rsa.pem")
		if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(private)}), 0600); err != nil {
			t.Fatal(err)
		}
		if err := Validate(path); err != nil {
			t.Fatal(err)
		}
		block, err := ssh.MarshalPrivateKeyWithPassphrase(private, "", []byte("test-only-passphrase"))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
			t.Fatal(err)
		}
		if Validate(path) == nil {
			t.Fatal("encrypted key accepted without passphrase provider")
		}
	})
}
