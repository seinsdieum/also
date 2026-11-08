package also

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config origins are the pre-install user values for managed config paths and
// the fact that also created a managed instruction file. They live outside
// state so a state loss does not make uninstall restore package values.
const SchemaConfigOrigins = "also.config-origins/v1"

type ConfigOrigin struct {
	File    string          `json:"file"`
	Path    []string        `json:"path,omitempty"`
	Value   json.RawMessage `json:"value,omitempty"`
	Absent  bool            `json:"absent,omitempty"`
	BaseSet bool            `json:"base_set,omitempty"`
	Managed json.RawMessage `json:"managed,omitempty"`
}

type configOriginFile struct {
	Schema  string          `json:"schema"`
	Project string          `json:"project"`
	Entries []ConfigOrigin  `json:"entries,omitempty"`
	Blocks  map[string]bool `json:"blocks,omitempty"`
	Files   map[string]bool `json:"files,omitempty"`
}

func configOriginPath(project string) string {
	return filepath.Join(HomeDir(), "config-origins", ShortHash([]byte(PathKey(project)), 16)+".json")
}

func loadConfigOrigins(project string) (*configOriginFile, error) {
	f := &configOriginFile{Schema: SchemaConfigOrigins, Project: PathKey(project), Blocks: map[string]bool{}, Files: map[string]bool{}}
	data, err := os.ReadFile(configOriginPath(project))
	if err != nil {
		if os.IsNotExist(err) {
			return f, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, f); err != nil {
		return nil, fmt.Errorf("parse config origins: %w", err)
	}
	if f.Blocks == nil {
		f.Blocks = map[string]bool{}
	}
	if f.Files == nil {
		f.Files = map[string]bool{}
	}
	return f, nil
}

func saveConfigOrigins(project string, f *configOriginFile) error {
	f.Schema = SchemaConfigOrigins
	f.Project = PathKey(project)
	path := configOriginPath(project)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWrite(path, data, 0o600)
}

func relProjectFile(project, file string) string {
	if abs, err := AbsPath(project); err == nil {
		project = abs
	}
	rel, err := filepath.Rel(project, file)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel)
}

func originKey(file string, path []string) string {
	return file + "\x00" + strings.Join(path, "\x00")
}

// PrepareKnownConfigOrigins lets BuildPlan treat a previously owned config
// path as safe even when state was lost or the harness changed.
func PrepareKnownConfigOrigins(st *State, project string) {
	if st == nil || project == "" {
		return
	}
	f, err := loadConfigOrigins(project)
	if err != nil {
		return
	}
	base := PathKey(project)
	st.knownOrigins = map[string]ConfigOrigin{}
	for _, e := range f.Entries {
		file := filepath.Join(base, filepath.FromSlash(e.File))
		st.knownOrigins[originKey(file, e.Path)] = e
	}
}

// ApplyConfigOrigins restores the original base and block provenance onto a
// plan before ApplyPlan, which is what makes frozen replay after state loss
// reversible.
func ApplyConfigOrigins(project string, edits []ConfigEdit, block *BlockEdit) error {
	if project == "" {
		return nil
	}
	f, err := loadConfigOrigins(project)
	if err != nil {
		return err
	}
	index := map[string]ConfigOrigin{}
	for _, e := range f.Entries {
		index[originKey(e.File, e.Path)] = e
	}
	for i := range edits {
		e := &edits[i]
		rel := relProjectFile(project, e.File)
		if rel == "" {
			continue
		}
		if f.Files[rel] {
			e.CreatedFile = true
		}
		if e.Kind != "json-set" {
			continue
		}
		origin, ok := index[originKey(rel, e.Path)]
		if !ok {
			continue
		}
		e.BaseSet = true
		if origin.Absent {
			e.Base = nil
		} else {
			e.Base = origin.Value
		}
	}
	if block != nil {
		rel := relProjectFile(project, block.File)
		if created, ok := f.Blocks[rel]; ok {
			block.CreatedFile = created
		}
	}
	return nil
}

// CleanupConfigOrigins drops origin entries for paths and files that no
// remaining install owns. Without this a later manual edit would be mistaken
// for the base of a previous ownership epoch and removed on uninstall.
func CleanupConfigOrigins(project string, st *State, edits []ConfigEdit, block *BlockEdit) error {
	if project == "" || st == nil {
		return nil
	}
	f, err := loadConfigOrigins(project)
	if err != nil {
		return err
	}
	owned := func(file string, path []string) bool {
		rel := relProjectFile(project, file)
		if rel == "" {
			return false
		}
		for idx := range st.Installs {
			for _, e := range st.Installs[idx].Edits {
				if e.Kind == "json-set" && relProjectFile(project, e.File) == rel && stringSliceEqual(e.Path, path) {
					return true
				}
			}
		}
		return false
	}
	changed := false
	kept := f.Entries[:0]
	for _, origin := range f.Entries {
		if owned(filepath.Join(project, filepath.FromSlash(origin.File)), origin.Path) {
			kept = append(kept, origin)
		} else {
			changed = true
		}
	}
	f.Entries = kept
	for rel := range f.Files {
		ownedFile := false
		for idx := range st.Installs {
			for _, e := range st.Installs[idx].Edits {
				if relProjectFile(project, e.File) == rel {
					ownedFile = true
					break
				}
			}
			if ownedFile {
				break
			}
		}
		if !ownedFile {
			delete(f.Files, rel)
			changed = true
		}
	}
	if block != nil {
		rel := relProjectFile(project, block.File)
		ownedBlock := false
		for idx := range st.Installs {
			if b := st.Installs[idx].Block; b != nil && relProjectFile(project, b.File) == rel {
				ownedBlock = true
				break
			}
		}
		if !ownedBlock {
			if _, ok := f.Blocks[rel]; ok {
				delete(f.Blocks, rel)
				changed = true
			}
		}
	}
	if !changed {
		return nil
	}
	return saveConfigOrigins(project, f)
}

// MergeConfigOrigins records the first observed base for each managed path and
// whether also created the instruction file.
func MergeConfigOrigins(project string, edits []ConfigEdit, block *BlockEdit) error {
	if project == "" {
		return nil
	}
	f, err := loadConfigOrigins(project)
	if err != nil {
		return err
	}
	index := map[string]int{}
	for i := range f.Entries {
		index[originKey(f.Entries[i].File, f.Entries[i].Path)] = i
	}
	changed := false
	for _, e := range edits {
		if e.Kind != "json-set" {
			continue
		}
		rel := relProjectFile(project, e.File)
		if rel == "" {
			continue
		}
		key := originKey(rel, e.Path)
		if i, ok := index[key]; ok {
			// Track what also last wrote. A user edit no longer matches it,
			// so the next plan asks for --force instead of silently losing it.
			if !jsonEqual(f.Entries[i].Managed, e.Value) {
				f.Entries[i].Managed = append(json.RawMessage{}, e.Value...)
				changed = true
			}
			continue
		}
		origin := ConfigOrigin{File: rel, Path: append([]string{}, e.Path...), BaseSet: true,
			Managed: append(json.RawMessage{}, e.Value...)}
		switch {
		case e.BaseSet:
			origin.Value = e.Base
			origin.Absent = len(e.Base) == 0
		case e.HadPrev:
			origin.Value = e.Previous
			origin.Absent = len(e.Previous) == 0
		default:
			origin.Absent = true
		}
		f.Entries = append(f.Entries, origin)
		index[key] = len(f.Entries) - 1
		changed = true
	}
	for _, e := range edits {
		if !e.CreatedFile {
			continue
		}
		rel := relProjectFile(project, e.File)
		if rel != "" && !f.Files[rel] {
			f.Files[rel] = true
			changed = true
		}
	}
	if block != nil {
		rel := relProjectFile(project, block.File)
		if _, ok := f.Blocks[rel]; !ok && rel != "" {
			f.Blocks[rel] = block.CreatedFile
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return saveConfigOrigins(project, f)
}
