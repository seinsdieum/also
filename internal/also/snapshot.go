package also

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	PackageManifestFile = "also-pkg.json"
	SetManifestFile     = "also-set.json"
	OriginDir           = "origin"
	maxFileSize         = 8 << 20 // 8 MiB
)

type SnapshotFile struct {
	Data []byte
	Mode fs.FileMode
}

type Snapshot struct {
	Files map[string]*SnapshotFile // slash-separated rel paths
}

type SnapshotOptions struct {
	AllowSecrets bool
	AllowLarge   bool
}

type PackageMeta struct {
	Schema        string `json:"schema"`
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	SourceHarness string `json:"source_harness"`
	Label         string `json:"label,omitempty"`
	Digest        string `json:"digest"`
	EncodedWith   string `json:"encoded_with,omitempty"`
}

type Package struct {
	Meta  PackageMeta
	Files map[string]*SnapshotFile // origin files
	Dir   string
}

var secretNames = []string{
	".env", ".netrc", ".npmrc", "auth.json", "credentials", "credentials.json",
}

func isSecretName(base string) bool {
	l := strings.ToLower(base)
	for _, s := range secretNames {
		if l == s {
			return true
		}
	}
	if strings.HasPrefix(l, ".env.") &&
		!strings.HasSuffix(l, ".example") && !strings.HasSuffix(l, ".sample") && !strings.HasSuffix(l, ".template") {
		return true
	}
	for _, suffix := range []string{".pem", ".key", ".p12", ".pfx", ".sqlite", ".db", ".sqlite3"} {
		if strings.HasSuffix(l, suffix) {
			return true
		}
	}
	if strings.HasPrefix(l, "id_rsa") || strings.HasPrefix(l, "id_ed25519") || strings.HasPrefix(l, "id_ecdsa") {
		return true
	}
	return false
}

var excludedDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, ".also": true,
	"node_modules": true, "__pycache__": true, ".venv": true, "venv": true,
	".idea": true, ".cache": true, ".tmp": true,
}

func excludedFile(base string) bool {
	switch base {
	case ".DS_Store", "Thumbs.db":
		return true
	}
	return strings.HasSuffix(strings.ToLower(base), ".pyc")
}

// ReadSnapshot walks a bundle and returns all files, with safety checks.
func ReadSnapshot(root string, opts SnapshotOptions) (*Snapshot, []string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	st, err := os.Stat(root)
	if err != nil {
		return nil, nil, err
	}
	if !st.IsDir() {
		return nil, nil, fmt.Errorf("%s is not a directory", root)
	}
	snap := &Snapshot{Files: map[string]*SnapshotFile{}}
	var warnings []string
	var secrets, large []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if excludedDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			rel, _ := filepath.Rel(root, path)
			return fmt.Errorf("symlink is not supported: %s (replace it with a real file or directory)", filepath.ToSlash(rel))
		}
		if !d.Type().IsRegular() {
			rel, _ := filepath.Rel(root, path)
			warnings = append(warnings, "skipped special file: "+filepath.ToSlash(rel))
			return nil
		}
		if excludedFile(name) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if isSecretName(name) && !opts.AllowSecrets {
			secrets = append(secrets, rel)
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxFileSize && !opts.AllowLarge {
			large = append(large, fmt.Sprintf("%s (%s)", rel, humanBytes(info.Size())))
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		mode := fs.FileMode(0o644)
		if info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		snap.Files[rel] = &SnapshotFile{Data: data, Mode: mode}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if len(secrets) > 0 {
		return nil, warnings, fmt.Errorf("refusing to encode likely secrets (use --allow-secrets if this is intentional):\n  %s",
			strings.Join(secrets, "\n  "))
	}
	if len(large) > 0 {
		return nil, warnings, fmt.Errorf("refusing to encode files larger than 8 MiB (use --allow-large if this is intentional):\n  %s",
			strings.Join(large, "\n  "))
	}
	if len(snap.Files) == 0 {
		return nil, warnings, fmt.Errorf("nothing to encode in %s", root)
	}
	return snap, warnings, nil
}

func (p *Package) Paths() []string {
	out := make([]string, 0, len(p.Files))
	for rel := range p.Files {
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

func (s *Snapshot) Paths() []string {
	out := make([]string, 0, len(s.Files))
	for p := range s.Files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func (s *Snapshot) TotalBytes() int64 {
	var n int64
	for _, f := range s.Files {
		n += int64(len(f.Data))
	}
	return n
}

// Digest computes the immutable package version: metadata plus origin bytes.
func (s *Snapshot) Digest(meta PackageMeta) string {
	h := sha256.New()
	write := func(parts ...string) {
		for _, p := range parts {
			h.Write([]byte(p))
			h.Write([]byte{0})
		}
	}
	write("also.pkg/v1", meta.Name, meta.Description, meta.SourceHarness, meta.Label)
	for _, p := range s.Paths() {
		f := s.Files[p]
		sum := sha256.Sum256(f.Data)
		write(p, fmt.Sprintf("%04o", f.Mode.Perm()), hex.EncodeToString(sum[:]))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func VersionFromDigest(d string) string { return d[:16] }

func WritePackage(dest string, meta PackageMeta, snap *Snapshot) error {
	tmp, err := os.MkdirTemp(filepath.Dir(dest), ".also-pkg-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	manifest, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	manifest = append(manifest, '\n')
	if err := os.WriteFile(filepath.Join(tmp, PackageManifestFile), manifest, 0o644); err != nil {
		return err
	}
	origin := filepath.Join(tmp, OriginDir)
	for _, rel := range snap.Paths() {
		if err := SafeRel(rel); err != nil {
			return err
		}
		f := snap.Files[rel]
		path := filepath.Join(origin, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, f.Data, f.Mode); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(origin, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func LoadPackage(dir string) (*Package, error) {
	manifestPath := filepath.Join(dir, PackageManifestFile)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	var meta PackageMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse %s: %w", manifestPath, err)
	}
	if meta.Schema != SchemaPkg {
		return nil, fmt.Errorf("unsupported package schema %q in %s", meta.Schema, manifestPath)
	}
	if !ValidName(meta.Name) {
		return nil, fmt.Errorf("invalid package name %q in %s", meta.Name, manifestPath)
	}
	origin := filepath.Join(dir, OriginDir)
	files := map[string]*SnapshotFile{}
	err = filepath.WalkDir(origin, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == origin {
			return nil
		}
		rel, err := filepath.Rel(origin, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if err := SafeRel(rel); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("package contains a symlink, refusing to install: %s", rel)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("package contains a special file: %s", rel)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		mode := fs.FileMode(0o644)
		if info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		files[rel] = &SnapshotFile{Data: data, Mode: mode}
		return nil
	})
	if err != nil {
		return nil, err
	}
	p := &Package{Meta: meta, Files: files, Dir: dir}
	if modes := gitFileModes(dir); modes != nil {
		// The worktree mode is meaningless on Windows (no exec bit), but the
		// git index preserves it, so keep digests identical across platforms.
		for rel, f := range files {
			if m, ok := modes["origin/"+rel]; ok {
				f.Mode = m
			}
		}
	}
	if meta.Digest != "" {
		snap := &Snapshot{Files: files}
		if got := snap.Digest(meta); got != meta.Digest {
			return nil, fmt.Errorf("package integrity check failed for %s: manifest digest %s, content %s", dir, ShortHash([]byte(meta.Digest), 12), ShortHash([]byte(got), 12))
		}
	}
	return p, nil
}

// gitFileModes returns tracked modes relative to dir, or nil outside git.
func gitFileModes(dir string) map[string]fs.FileMode {
	out, err := runGit(dir, "ls-files", "-s", "--", ".")
	if err != nil {
		return nil
	}
	modes := map[string]fs.FileMode{}
	for _, line := range strings.Split(out, "\n") {
		tab := strings.IndexByte(line, '\t')
		if tab < 0 {
			continue
		}
		fields := strings.Fields(line[:tab])
		if len(fields) == 0 {
			continue
		}
		mode := fs.FileMode(0o644)
		if fields[0] == "100755" {
			mode = 0o755
		}
		modes[line[tab+1:]] = mode
	}
	return modes
}

func humanBytes(n int64) string {	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MiB", float64(n)/(1024*1024))
}

// FormatBytes renders a byte count for CLI output.
func FormatBytes(n int64) string { return humanBytes(n) }
