package main

import (
	"io/fs"
	"os"
	"path/filepath"

	configfs "github.com/Tencent/WeKnora/config"
	sqlitemigrations "github.com/Tencent/WeKnora/migrations/sqlite"
)

// ensureDesktopUserConfig materializes the embedded default config tree into
// a per-user directory and chdirs there when ./config/config.yaml cannot be
// resolved from the current directory. Installed Windows/Linux builds run
// outside the repo checkout (and macOS bundles only ship config under
// Resources), so internal/config's viper search would otherwise fail and
// panic at startup. Existing files are never overwritten, so user edits in
// the per-user config tree survive upgrades.
func ensureDesktopUserConfig() (envPath string) {
	// Materialize only what is missing. A repo checkout (or CI smoke test) run
	// from the project root already has ./config/config.yaml, but not the .env
	// that supplies DB_DRIVER and friends; skipping unconditionally left those
	// builds without a .env and the container panicked on an empty driver.
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

	// Database migrations are read from disk at <cwd>/migrations/sqlite
	// (internal/database/migration.go); materialize them next to the config.
	_ = fs.WalkDir(sqlitemigrations.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		dst := filepath.Join(root, "migrations", "sqlite", filepath.FromSlash(path))
		if _, statErr := os.Stat(dst); statErr == nil {
			return nil
		}
		if mkErr := os.MkdirAll(filepath.Dir(dst), 0o755); mkErr != nil {
			return mkErr
		}
		data, readErr := sqlitemigrations.FS.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(dst, data, 0o644)
	})

	// Repo checkout / CI smoke run from the project root: ./config resolves,
	// so keep the working directory and reuse a .env there if present.
	if _, statErr := os.Stat(filepath.Join("config", "config.yaml")); statErr == nil {
		if _, envErr := os.Stat(".env"); envErr == nil {
			if abs, absErr := filepath.Abs(".env"); absErr == nil {
				return abs
			}
			return ".env"
		}
		return ""
	}

	// godotenv.Load in main reads an explicit path (see ensureDesktopEnv);
	// without the DB_DRIVER=sqlite and friends it supplies, the desktop
	// container cannot initialize. Never overwrite user edits.
	if envPath == "" {
		envPath = filepath.Join(root, ".env")
		if _, statErr := os.Stat(envPath); statErr != nil {
			_ = os.WriteFile(envPath, []byte(desktopEnvTemplate), 0o644)
		}
	}
	// Relative data paths in the template (./data/weknora.db) resolve here.
	_ = os.Chdir(root)
	return envPath
}

// desktopEnvTemplate mirrors .env.lite.example (the template the macOS
// desktop bundle ships as Resources/.env). Relative data paths resolve under
// the per-user root because ensureDesktopUserConfig chdirs there.
const desktopEnvTemplate = `# Generated on first run (mirrors .env.lite.example).
GIN_MODE=debug
LOG_LEVEL=debug
DB_DRIVER=sqlite
DB_PATH=./data/weknora.db
RETRIEVE_DRIVER=sqlite
STORAGE_TYPE=local
LOCAL_STORAGE_BASE_DIR=./data/files
STREAM_MANAGER_TYPE=memory
OLLAMA_BASE_URL=http://127.0.0.1:11434
NEO4J_ENABLE=false
WEKNORA_SANDBOX_MODE=disabled
ENABLE_GRAPH_RAG=false
CONCURRENCY_POOL_SIZE=3
DOCREADER_ADDR=127.0.0.1:50051
DOCREADER_TRANSPORT=grpc
`
