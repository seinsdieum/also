package also

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

type InstalledFile struct {
	Path string `json:"path"`
	SHA  string `json:"sha"`
	Mode string `json:"mode"`
}

// BlockEdit describes the managed instructions block owned by one install.
type BlockEdit struct {
	File        string `json:"file"`
	ID          string `json:"id"`
	SHA         string `json:"sha"`
	CreatedFile bool   `json:"created_file"`
}

// ConfigEdit is one reversible change in a harness config file.
type ConfigEdit struct {
	File     string          `json:"file"`
	Kind     string          `json:"kind"` // json-set | json-append | toml-block
	Path     []string        `json:"path,omitempty"`
	Value    json.RawMessage `json:"value,omitempty"`
	Previous json.RawMessage `json:"previous,omitempty"`
	HadPrev  bool            `json:"had_previous,omitempty"`
	// Base is the value before any package managed this path. It is shared by
	// every owner, so uninstalling the last owner restores the user's value
	// regardless of uninstall order.
	Base    json.RawMessage `json:"base,omitempty"`
	BaseSet bool            `json:"base_set,omitempty"`
	Marker  string          `json:"marker,omitempty"`
	Block   string          `json:"block,omitempty"`
	// CreatedFile means also created the file (it did not exist before).
	CreatedFile bool `json:"created_file,omitempty"`
}

// Executable is one installed component that can run code or start a process:
// an MCP server, a hook, a plugin or a slash command.
type Executable struct {
	Kind   string `json:"kind"` // mcp | hook | plugin | command
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
}

type Install struct {
	ID            string          `json:"id"`
	Set           string          `json:"set"`
	SetRemote     string          `json:"set_remote"`
	Package       string          `json:"package"`
	Version       string          `json:"version"`
	Digest        string          `json:"digest,omitempty"`
	Semver        string          `json:"semver,omitempty"`
	Label         string          `json:"label,omitempty"`
	SourceHarness string          `json:"source_harness"`
	Harness       string          `json:"harness"`
	Scope         string          `json:"scope"`
	Project       string          `json:"project,omitempty"`
	Root          string          `json:"root"`
	WithConfig    bool            `json:"with_config"`
	Parts         []string        `json:"parts,omitempty"`
	Exclude       []string        `json:"exclude,omitempty"`
	InstalledAt   time.Time       `json:"installed_at"`
	Files         []InstalledFile `json:"files"`
	Block         *BlockEdit      `json:"block,omitempty"`
	Edits         []ConfigEdit    `json:"edits,omitempty"`
	Executables   []Executable    `json:"executables,omitempty"`
	Warnings      []string        `json:"warnings,omitempty"`
	Skipped       []string        `json:"skipped,omitempty"`
}

type State struct {
	Schema   string    `json:"schema"`
	Installs []Install `json:"installs"`
	// knownOrigins is transient context for BuildPlan. It is not persisted.
	knownOrigins map[string]bool
}

func LoadState() (*State, error) {
	st := &State{Schema: SchemaState}
	data, err := os.ReadFile(StatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return nil, fmt.Errorf("read state: %w", err)
	}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, fmt.Errorf("parse %s: %w", StatePath(), err)
	}
	if st.Schema != SchemaState {
		return nil, fmt.Errorf("unsupported state schema %q in %s", st.Schema, StatePath())
	}
	return st, nil
}

func SaveState(st *State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWrite(StatePath(), data, 0o600)
}

// FindByID returns a copy. Callers may hold it across a state mutation (for
// example Uninstall, which removes the slice element) without reading a
// different install after the slice shifts.
func (s *State) FindByID(id string) *Install {
	for idx := range s.Installs {
		if s.Installs[idx].ID == id {
			in := s.Installs[idx]
			return &in
		}
	}
	return nil
}

func (s *State) InstallsOf(pkg string) []*Install {
	var out []*Install
	for idx := range s.Installs {
		if pkg == "" || s.Installs[idx].Package == pkg {
			out = append(out, &s.Installs[idx])
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Set != out[b].Set {
			return out[a].Set < out[b].Set
		}
		return out[a].Package < out[b].Package
	})
	return out
}

// ownerOf returns the install that owns a file, if any.
func (s *State) ownerOf(path string) *Install {
	owners := s.ownersOf(path)
	if len(owners) == 0 {
		return nil
	}
	return owners[0]
}

// ownersOf returns every install that lists path as one of its files.
func (s *State) ownersOf(path string) []*Install {
	var out []*Install
	key := PathKey(path)
	for idx := range s.Installs {
		for _, f := range s.Installs[idx].Files {
			if PathKey(f.Path) == key {
				out = append(out, &s.Installs[idx])
				break
			}
		}
	}
	return out
}

// sharedConfigEdit reports whether another install owns the same config value.
func (s *State) sharedConfigEdit(e ConfigEdit, selfID string) bool {
	for idx := range s.Installs {
		if s.Installs[idx].ID == selfID {
			continue
		}
		for _, oe := range s.Installs[idx].Edits {
			if oe.File == e.File && oe.Kind == e.Kind && oe.Marker == e.Marker &&
				stringSliceEqual(oe.Path, e.Path) && jsonEqual(oe.Value, e.Value) {
				return true
			}
		}
	}
	return false
}

func sameConfigScope(a, b *Install) bool {
	if a == nil || b == nil {
		return false
	}
	if a.Scope != b.Scope {
		return false
	}
	if a.Scope == "global" {
		return true
	}
	return PathKey(a.Project) == PathKey(b.Project)
}

// otherConfigEdit returns another install that owns the same config path in the
// same project. Used by uninstall and upgrade to keep a surviving package's
// value instead of restoring a value that belonged to a removed package.
func (s *State) otherConfigEdit(in *Install, file string, path []string) *ConfigEdit {
	for idx := range s.Installs {
		other := &s.Installs[idx]
		if other.ID == in.ID || !sameConfigScope(in, other) {
			continue
		}
		for i := range other.Edits {
			e := &other.Edits[i]
			if e.File == file && e.Kind == "json-set" && stringSliceEqual(e.Path, path) {
				return e
			}
		}
	}
	return nil
}

// configBase finds the original pre-also value shared by any owner of a path.
// set=true with a nil base means the key did not exist before also. owner
// distinguishes "another package owns this path, base absent" from "no owner
// at all".
func (s *State) configBase(in *Install, file string, path []string) (json.RawMessage, bool, bool) {
	for idx := range s.Installs {
		other := &s.Installs[idx]
		if in != nil && other.ID == in.ID {
			continue
		}
		if in != nil && !sameConfigScope(in, other) {
			continue
		}
		for i := range other.Edits {
			e := &other.Edits[i]
			if e.File != file || e.Kind != "json-set" || !stringSliceEqual(e.Path, path) {
				continue
			}
			if e.BaseSet {
				return e.Base, true, true
			}
			if e.HadPrev {
				return e.Previous, true, true
			}
			return nil, true, true
		}
	}
	return nil, false, false
}

// fileHasOtherBlock reports whether another install keeps a block in this file.
func (s *State) fileHasOtherBlock(in *Install, file string) bool {
	for idx := range s.Installs {
		other := &s.Installs[idx]
		if other.ID == in.ID || !sameConfigScope(in, other) {
			continue
		}
		if other.Block != nil && other.Block.File == file {
			return true
		}
	}
	return false
}

// fileHasOtherConfigEdits reports whether any other install manages this file.
func (s *State) fileHasOtherConfigEdits(in *Install, file string) bool {
	for idx := range s.Installs {
		other := &s.Installs[idx]
		if other.ID == in.ID || !sameConfigScope(in, other) {
			continue
		}
		for _, e := range other.Edits {
			if e.File == file {
				return true
			}
		}
	}
	return false
}

// fileCreatedByAlso reports whether also created this config/instruction file.
func (s *State) fileCreatedByAlso(file string) bool {
	for idx := range s.Installs {
		if in := &s.Installs[idx]; in.Block != nil && in.Block.File == file && in.Block.CreatedFile {
			return true
		}
		for _, e := range s.Installs[idx].Edits {
			if e.File == file && e.CreatedFile {
				return true
			}
		}
	}
	return false
}

// sharedBlock reports whether another install owns the same managed block.
func (s *State) sharedBlock(b *BlockEdit, selfID string) bool {
	if b == nil {
		return false
	}
	for idx := range s.Installs {
		if s.Installs[idx].ID == selfID {
			continue
		}
		other := s.Installs[idx].Block
		if other != nil && other.File == b.File && other.ID == b.ID {
			return true
		}
	}
	return false
}

// PruneMissingProjects drops install records whose project directory no longer
// exists. It never removes files; the directory is already gone.
func (s *State) PruneMissingProjects() []Install {
	var removed []Install
	out := s.Installs[:0]
	for _, in := range s.Installs {
		if in.Project != "" {
			if _, err := os.Stat(in.Project); os.IsNotExist(err) {
				removed = append(removed, in)
				continue
			}
		}
		out = append(out, in)
	}
	s.Installs = out
	return removed
}

func (s *State) Remove(id string) {
	out := s.Installs[:0]
	for _, in := range s.Installs {
		if in.ID != id {
			out = append(out, in)
		}
	}
	s.Installs = out
}

// InstallID is stable across upgrades of one package into one harness. The
// harness is part of the slot so the same package can live in two harnesses.
func InstallID(set, pkg, harness, scope, project string) string {
	sum := sha256.Sum256([]byte(set + "\x00" + pkg + "\x00" + harness + "\x00" + scope + "\x00" + PathKey(project)))
	return hex.EncodeToString(sum[:8])
}

// legacyInstallID is the pre-harness slot id, kept so upgrades can adopt state
// written by older also versions.
func legacyInstallID(set, pkg, scope, project string) string {
	sum := sha256.Sum256([]byte(set + "\x00" + pkg + "\x00" + scope + "\x00" + project))
	return hex.EncodeToString(sum[:8])
}

func ShortHash(data []byte, n int) string {
	sum := sha256.Sum256(data)
	h := hex.EncodeToString(sum[:])
	if n <= 0 || n > len(h) {
		return h
	}
	return h[:n]
}
