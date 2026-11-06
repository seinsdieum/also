package also

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// Version is the CLI and package-encoder version.
	Version = "1.0.2"

	SchemaConfig  = "also.config/v2"
	SchemaState   = "also.state/v1"
	SchemaSet     = "also.set/v1"
	SchemaPkg     = "also.pkg/v1"
	SchemaAdapter = "also.adapter/v1"

	EnvHome = "ALSO_HOME"
)

// Commit and BuildDate are set by the release build through -ldflags. Source
// builds keep the defaults.
var (
	Commit    = "unknown"
	BuildDate = "unknown"
)

// HomeDir returns the also state directory. ALSO_HOME overrides it (used by tests).
func HomeDir() string {
	if v := os.Getenv(EnvHome); v != "" {
		if abs, err := filepath.Abs(v); err == nil {
			return abs
		}
		return v
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ".also"
	}
	return filepath.Join(h, ".also")
}

func ConfigPath() string { return filepath.Join(HomeDir(), "config.json") }
func StatePath() string  { return filepath.Join(HomeDir(), "state.json") }
func CacheDir() string   { return filepath.Join(HomeDir(), "cache") }
func BackupsDir() string { return filepath.Join(HomeDir(), "backups") }
func LockPath() string   { return filepath.Join(HomeDir(), "lock") }

// SetCachePath is the clone used for --from <url> installs.
func SetCachePath(remote string) string {
	return filepath.Join(CacheDir(), ShortHash([]byte(remote), 16))
}

func EnsureHome() error {
	for _, dir := range []string{HomeDir(), CacheDir(), BackupsDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return nil
}

// Lock serializes mutating also processes across terminals.
type Lock struct {
	path string
	held bool
}

func AcquireLock() (*Lock, error) {
	if err := EnsureHome(); err != nil {
		return nil, err
	}
	path := LockPath()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create lock: %w", err)
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > 30*time.Minute {
			_ = os.Remove(path)
			return AcquireLock()
		}
		return nil, fmt.Errorf("another also process is running (lock: %s)", path)
	}
	_, _ = fmt.Fprintf(f, "%d\n%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
	_ = f.Close()
	return &Lock{path: path, held: true}, nil
}

func (l *Lock) Release() {
	if l != nil && l.held {
		_ = os.Remove(l.path)
		l.held = false
	}
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".also-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func ExpandHome(p string) string {
	if p == "~" {
		h, _ := os.UserHomeDir()
		return h
	}
	if strings.HasPrefix(p, "~/") {
		h, _ := os.UserHomeDir()
		return filepath.Join(h, p[2:])
	}
	return p
}

func AbsPath(p string) (string, error) {
	return filepath.Abs(ExpandHome(p))
}

func isWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// SafeRel validates a path that came from a package or a bundle.
func SafeRel(p string) error {
	if p == "" {
		return errors.New("empty path")
	}
	if filepath.IsAbs(p) {
		return fmt.Errorf("absolute path is not allowed: %s", p)
	}
	clean := filepath.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes its root: %s", p)
	}
	if strings.ContainsRune(p, 0) {
		return fmt.Errorf("NUL in path: %s", p)
	}
	return nil
}
