//go:build !windows

package platform

import "os"

func SecureDirectory(path string) error { return os.Chmod(path, 0700) }
