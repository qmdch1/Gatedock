//go:build !windows

package provision

import "os"

func secureDirectory(path string) error { return os.Chmod(path, 0700) }
