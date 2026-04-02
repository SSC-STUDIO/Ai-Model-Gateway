//go:build !windows

package config

import "os"

func osReplaceFile(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}
