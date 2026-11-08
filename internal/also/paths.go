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
	Version = "1.2.0"

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
//
// shortcut: a stale lock is reclaimed with rename, so a process that held the
// lock for over 30 minutes can race a fresh lock during Release; upgrade to
// flock/LockFileEx if that window ever matters.
type Lock struct {
	path  string
	file  *os.File
	token string
}

func lockToken() string {
	return ShortHash([]byte(fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())), 16)
}

func AcquireLock() (*Lock, error) {
	if err := EnsureHome(); err != nil {
		return nil, err
	}
	path := LockPath()
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err == nil {
			token := lockToken()
			if _, err := f.WriteString(token + "\n"); err != nil {
				_ = f.Close()
				_ = os.Remove(path)
				return nil, fmt.Errorf("write lock: %w", err)
			}
			return &Lock{path: path, file: f, token: token}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create lock: %w", err)
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				continue
			}
			return nil, fmt.Errorf("stat lock: %w", statErr)
		}
		if time.Since(info.ModTime()) <= 30*time.Minute {
			return nil, fmt.Errorf("another also process is running (lock: %s)", path)
		}
		// Only the process that renames the stale lock away may delete it, so
		// a second process can never delete a freshly created lock.
		stale := path + ".stale." + lockToken()
		if err := os.Rename(path, stale); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("reclaim stale lock: %w", err)
		}
		_ = os.Remove(stale)
	}
}

func (l *Lock) Release() {
	if l == nil || l.file == nil {
		return
	}
	fi, err := l.file.Stat()
	remove := false
	if err == nil {
		if pi, statErr := os.Stat(l.path); statErr == nil && os.SameFile(fi, pi) {
			if data, readErr := os.ReadFile(l.path); readErr == nil && strings.TrimSpace(string(data)) == l.token {
				remove = true
			}
		}
	}
	// Windows cannot delete an open file: close before removing.
	_ = l.file.Close()
	l.file = nil
	if remove {
		_ = os.Remove(l.path)
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
