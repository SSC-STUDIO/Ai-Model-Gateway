//go:build windows

package config

import "golang.org/x/sys/windows"

func osReplaceFile(oldpath, newpath string) error {
	src, err := windows.UTF16PtrFromString(oldpath)
	if err != nil {
		return err
	}
	dst, err := windows.UTF16PtrFromString(newpath)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(src, dst, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
