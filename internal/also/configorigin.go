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
	File   string          `json:"file"`
	Path   []string        `json:"path,omitempty"`
	Value  json.RawMessage `json:"value,omitempty"`
	Absent bool            `json:"absent,omitempty"`
}

type configOriginFile struct {
	Schema  string          `json:"schema"`
	Project string          `json:"project"`
	Entries []ConfigOrigin  `json:"entries,omitempty"`
	Blocks  map[string]bool `json:"blocks,omitempty"`
}

func configOriginPath(project string) string {
	return filepath.Join(HomeDir(), "config-origins", ShortHash([]byte(PathKey(project)), 16)+".json")
}

func loadConfigOrigins(project string) (*configOriginFile, error) {
	f := &configOriginFile{Schema: SchemaConfigOrigins, Project: PathKey(project), Blocks: map[string]bool{}}
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
		if e.Kind != "json-set" {
			continue
		}
		rel := relProjectFile(project, e.File)
		if rel == "" {
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
	seen := map[string]bool{}
	for _, e := range f.Entries {
		seen[originKey(e.File, e.Path)] = true
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
		if seen[key] {
			continue
		}
		origin := ConfigOrigin{File: rel, Path: append([]string{}, e.Path...)}
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
		seen[key] = true
		changed = true
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
