package clientconfig

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// BackupCopy copies src to src + ".bak." + timestamp if src exists.
func BackupCopy(src string) (backupPath string, err error) {
	st, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if st.IsDir() {
		return "", fmt.Errorf("refuse to backup directory: %s", src)
	}
	backupPath = src + ".bak." + time.Now().UTC().Format("20060102-150405")
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.OpenFile(backupPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, st.Mode()&0o666)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		_ = os.Remove(backupPath)
		return "", err
	}
	return backupPath, nil
}

// CodexConfigPath returns ~/.codex/config.toml
func CodexConfigPath(home string) string {
	return filepath.Join(home, ".codex", "config.toml")
}

// ClaudeSettingsPath returns ~/.claude/settings.json
func ClaudeSettingsPath(home string) string {
	return filepath.Join(home, ".claude", "settings.json")
}

// OpenClawConfigPath returns ~/.openclaw/openclaw.json
func OpenClawConfigPath(home string) string {
	return filepath.Join(home, ".openclaw", "openclaw.json")
}
