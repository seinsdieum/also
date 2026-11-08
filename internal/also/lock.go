package also

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// The project lockfile records what was resolved into a directory. It is meant
// to be committed: a machine without also state can install exactly the same
// versions with "also install --frozen".
const (
	LockFile   = "also.lock"
	SchemaLock = "also.lock/v1"
)

type LockEntry struct {
	Package       string   `json:"package"`
	Namespace     string   `json:"namespace"`
	Remote        string   `json:"remote,omitempty"`
	Version       string   `json:"version"`
	Digest        string   `json:"digest,omitempty"`
	Constraint    string   `json:"constraint,omitempty"`
	Harness       string   `json:"harness"`
	SourceHarness string   `json:"source_harness,omitempty"`
	Parts         []string `json:"parts,omitempty"`
	Exclude       []string `json:"exclude,omitempty"`
	WithConfig    bool     `json:"with_config,omitempty"`
}

type Lockfile struct {
	Schema   string      `json:"schema"`
	Packages []LockEntry `json:"packages"`
}

func LockfilePath(project string) string { return filepath.Join(project, LockFile) }

// LoadLock returns an empty lock when the file does not exist.
func LoadLock(project string) (*Lockfile, error) {
	return LoadLockFile(LockfilePath(project))
}

func LoadLockFile(path string) (*Lockfile, error) {
	lock := &Lockfile{Schema: SchemaLock}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return lock, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, lock); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if lock.Schema == "" {
		lock.Schema = SchemaLock
	}
	if lock.Schema != SchemaLock {
		return nil, fmt.Errorf("unsupported lock schema %q in %s", lock.Schema, path)
	}
	return lock, nil
}

func SaveLock(project string, lock *Lockfile) error {
	lock.Schema = SchemaLock
	lock.Sort()
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWrite(LockfilePath(project), data, 0o644)
}

func (l *Lockfile) Sort() {
	sort.Slice(l.Packages, func(i, j int) bool {
		a, b := l.Packages[i], l.Packages[j]
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		if a.Package != b.Package {
			return a.Package < b.Package
		}
		return a.Harness < b.Harness
	})
}

// Find returns the entry for one package installed into one harness.
// Find returns the first entry for a package and harness, ignoring the
// namespace. Use FindIn when the namespace is known.
func (l *Lockfile) Find(pkg, harness string) *LockEntry {
	if l == nil {
		return nil
	}
	for i := range l.Packages {
		if l.Packages[i].Package == pkg && l.Packages[i].Harness == harness {
			return &l.Packages[i]
		}
	}
	return nil
}

// FindIn includes the namespace in the identity, so the same package name from
// two namespaces can be installed side by side.
func (l *Lockfile) FindIn(namespace, pkg, harness string) *LockEntry {
	if l == nil {
		return nil
	}
	for i := range l.Packages {
		e := &l.Packages[i]
		if e.Namespace == namespace && e.Package == pkg && e.Harness == harness {
			return e
		}
	}
	return nil
}

func (l *Lockfile) Upsert(e LockEntry) {
	for i := range l.Packages {
		if l.Packages[i].Namespace == e.Namespace && l.Packages[i].Package == e.Package && l.Packages[i].Harness == e.Harness {
			l.Packages[i] = e
			return
		}
	}
	l.Packages = append(l.Packages, e)
}

func (l *Lockfile) Remove(pkg, harness string) {
	out := l.Packages[:0]
	for _, e := range l.Packages {
		if e.Package == pkg && (harness == "" || e.Harness == harness) {
			continue
		}
		out = append(out, e)
	}
	l.Packages = out
}

func (l *Lockfile) RemoveIn(namespace, pkg, harness string) {
	out := l.Packages[:0]
	for _, e := range l.Packages {
		if e.Namespace == namespace && e.Package == pkg && (harness == "" || e.Harness == harness) {
			continue
		}
		out = append(out, e)
	}
	l.Packages = out
}

func LockEntryFromInstall(in *Install, constraint string) LockEntry {
	return LockEntry{
		Package: in.Package, Namespace: in.Set, Remote: in.SetRemote,
		Version: in.Version, Digest: in.Digest, Constraint: constraint,
		Harness: in.Harness, SourceHarness: in.SourceHarness,
		Parts: in.Parts, Exclude: in.Exclude, WithConfig: in.WithConfig,
	}
}

// CheckEntry rejects a lockfile entry that cannot describe a real install.
func (e LockEntry) CheckEntry() error {
	if !ValidName(e.Package) {
		return fmt.Errorf("invalid package name %q in lockfile", e.Package)
	}
	if !ValidName(e.Namespace) {
		return fmt.Errorf("invalid namespace %q in lockfile", e.Namespace)
	}
	if !validVersion(e.Version) {
		return fmt.Errorf("invalid version %q in lockfile for %s", e.Version, e.Package)
	}
	if e.Harness == "" {
		return fmt.Errorf("missing harness for %s in lockfile", e.Package)
	}
	if e.Digest != "" && !validDigest(e.Digest) {
		return fmt.Errorf("invalid digest %q in lockfile for %s", e.Digest, e.Package)
	}
	return nil
}

// VerifyInstall compares a completed install with the locked entry.
func (e LockEntry) VerifyInstall(in *Install) error {
	if in.Version != e.Version {
		return fmt.Errorf("%s/%s resolved to %s but the lockfile pins %s", e.Namespace, e.Package, in.Version, e.Version)
	}
	if e.Digest != "" && in.Digest != "" && in.Digest != e.Digest {
		return fmt.Errorf("%s/%s digest %s does not match the lockfile digest %s", e.Namespace, e.Package, in.Digest, e.Digest)
	}
	return nil
}
