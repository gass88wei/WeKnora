package main

import (
	"io/fs"
	"os"
	"path/filepath"

	configfs "github.com/Tencent/WeKnora/config"
)

// ensureDesktopUserConfig materializes the embedded default config tree into
// a per-user directory and chdirs there when ./config/config.yaml cannot be
// resolved from the current directory. Installed Windows/Linux builds run
// outside the repo checkout (and macOS bundles only ship config under
// Resources), so internal/config's viper search would otherwise fail and
// panic at startup. Existing files are never overwritten, so user edits in
// the per-user config tree survive upgrades.
func ensureDesktopUserConfig() {
	if _, err := os.Stat(filepath.Join("config", "config.yaml")); err == nil {
		return
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	root := filepath.Join(base, "WeKnora Lite")
	_ = fs.WalkDir(configfs.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		dst := filepath.Join(root, filepath.FromSlash(path))
		if _, statErr := os.Stat(dst); statErr == nil {
			return nil
		}
		if mkErr := os.MkdirAll(filepath.Dir(dst), 0o755); mkErr != nil {
			return mkErr
		}
		data, readErr := configfs.FS.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(dst, data, 0o644)
	})
	_ = os.Chdir(root)
}
