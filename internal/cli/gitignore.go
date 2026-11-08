package cli

import (
	"path/filepath"
	"strings"

	"github.com/seinsdieum/also/internal/also"
)

// ensureManagedIgnores keeps the harness files of every project under one git
// root out of the product history. install, upgrade and reshape call it
// automatically; "also exclude" remains the manual override.
func ensureManagedIgnores(project, harness string) error {
	root, err := also.GitProjectRoot(project)
	git := err == nil
	if !git {
		// Archive/tarball checkouts have no .git; still keep a managed block
		// next to the project.
		root = project
	}
	st, err := also.LoadState()
	if err != nil {
		return nil
	}
	names := map[string]bool{}
	if h, ok := also.ResolveHarness(harness); ok && h.ID != "" {
		names[h.ID] = true
	}
	for _, in := range st.Installs {
		if in.Harness == "" || in.Project == "" {
			continue
		}
		rel, rerr := filepath.Rel(root, in.Project)
		if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		names[in.Harness] = true
	}
	var ids []string
	for id := range names {
		ids = append(ids, id)
	}
	var existing []string
	if git {
		existing, _ = also.IgnoreBlockEntries(root, also.IgnoreInfra)
	} else {
		existing, _ = also.IgnoreBlockEntriesAt(filepath.Join(root, ".gitignore"), also.IgnoreInfra)
	}
	entries := also.UniqueStrings(append(existing, also.InfraIgnoreEntries(ids)...))
	update := func(kind also.IgnoreKind, values []string) error {
		if git {
			_, err := also.UpdateGitIgnore(root, kind, true, values)
			return err
		}
		_, err := also.UpdateGitIgnoreAt(filepath.Join(root, ".gitignore"), kind, true, values)
		return err
	}
	if err := update(also.IgnoreInfra, entries); err != nil {
		return err
	}
	return update(also.IgnoreAlso, also.AlsoIgnoreEntries())
}
