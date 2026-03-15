//go:build !windows

package service

func Run(configPath string) (bool, error) {
	return false, nil
}
