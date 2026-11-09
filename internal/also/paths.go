package also

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	// Version is the CLI and package-encoder version.
	Version = "0.2.1"

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

// Lock serializes mutating also processes across terminals. A second process
// waits briefly for the lock instead of failing immediately, so parallel
// projects on one machine do not lose work. A crashed process is reclaimed
// after ten minutes.
type Lock struct {
	path  string
	file  *os.File
	token string
}

const (
	lockWait     = 30 * time.Second
	lockStaleAge = 10 * time.Minute
)

func lockToken() string {
	return ShortHash([]byte(fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())), 16)
}

func AcquireLock() (*Lock, error) {
	if err := EnsureHome(); err != nil {
		return nil, err
	}
	path := LockPath()
	deadline := time.Now().Add(lockWait)
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
		if time.Since(info.ModTime()) > lockStaleAge {
			// Only the process that renames the stale lock away may delete it,
			// so a second process can never delete a freshly created lock.
			stale := path + ".stale." + lockToken()
			if err := os.Rename(path, stale); err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("reclaim stale lock: %w", err)
			}
			_ = os.Remove(stale)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("another also process is running (lock: %s)", path)
		}
		time.Sleep(200 * time.Millisecond)
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
	abs, err := filepath.Abs(ExpandHome(p))
	if err != nil {
		return "", err
	}
	// Symlinked workspaces are the same project. Canonicalizing here keeps
	// install slots, ownership checks and the lock pointing at one path.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return canonicalCase(abs), nil
}

// PathKey is the identity form of a path. A case-insensitive filesystem
// (Windows, default APFS) must not turn two spellings of one directory into two
// install slots. Case folding is applied only where the filesystem itself
// ignores case, so a case-sensitive volume keeps distinct `Foo` and `foo`.
func PathKey(p string) string {
	clean := filepath.Clean(ExpandHome(p))
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		clean = resolved
	}
	// Only directories need the on-disk spelling; file paths are derived from
	// an already canonical project root. This keeps audit's per-file keys
	// cheap on large repositories.
	if fi, err := os.Stat(clean); err == nil && fi.IsDir() {
		clean = canonicalCase(clean)
	}
	if runtime.GOOS == "windows" {
		clean = strings.ToLower(clean)
	}
	return clean
}

// canonicalCase returns the on-disk spelling of an existing path. Without it,
// `/Users/me/temp` and `/Users/me/Temp` are different strings even though APFS
// resolves them to the same directory, which breaks install slots, audit and
// git-ignore filtering.
func canonicalCase(abs string) string {
	if runtime.GOOS == "windows" || !caseInsensitive(abs) {
		return abs
	}
	vol := filepath.VolumeName(abs)
	rest := strings.TrimPrefix(abs, vol)
	sep := string(filepath.Separator)
	parts := strings.Split(strings.Trim(rest, sep), sep)
	cur := vol + sep
	for _, part := range parts {
		if part == "" {
			continue
		}
		entries, err := os.ReadDir(cur)
		if err != nil {
			return abs
		}
		match := part
		for _, e := range entries {
			if e.Name() == part {
				match = part
				break
			}
			if strings.EqualFold(e.Name(), part) {
				match = e.Name()
			}
		}
		cur = filepath.Join(cur, match)
	}
	return cur
}

var caseProbes sync.Map // probed directory -> bool

// caseInsensitive reports whether the filesystem holding path ignores case. It
// probes an existing component by flipping its ASCII letters and comparing the
// inode; no files are created.
func caseInsensitive(path string) bool {
	if runtime.GOOS == "windows" {
		return true
	}
	dir := path
	for {
		fi, err := os.Lstat(dir)
		if err == nil {
			base := filepath.Base(dir)
			flipped := flipASCII(base)
			if flipped != base {
				if v, ok := caseProbes.Load(dir); ok {
					return v.(bool)
				}
				result := false
				if other, statErr := os.Lstat(filepath.Join(filepath.Dir(dir), flipped)); statErr == nil && os.SameFile(fi, other) {
					result = true
				}
				caseProbes.Store(dir, result)
				return result
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

func flipASCII(s string) string {
	out := []byte(s)
	for i, b := range out {
		switch {
		case b >= 'a' && b <= 'z':
			out[i] = b - 'a' + 'A'
		case b >= 'A' && b <= 'Z':
			out[i] = b - 'A' + 'a'
		}
	}
	return string(out)
}

// FindProjectRoot resolves the project a command was run in. The nearest
// also.lock or also.json wins; otherwise the nearest git root wins. Without
// this, running an install from internal/... created a second project inside
// the first one.
func FindProjectRoot(start string) (string, error) {
	abs, err := AbsPath(start)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return abs, nil
	}
	for cur := abs; ; cur = filepath.Dir(cur) {
		if fileExists(filepath.Join(cur, LockFile)) || fileExists(filepath.Join(cur, InfraFile)) {
			return cur, nil
		}
		if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			return cur, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs, nil
		}
	}
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
