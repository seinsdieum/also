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
	root := managedIgnoreRoot(project)
	st, err := also.LoadState()
	if err != nil {
		return nil
	}
	names := map[string]bool{}
	if h, ok := also.ResolveHarness(harness); ok && h.ID != "" {
		names[h.ID] = true
	}
	for _, in := range st.Installs {
		if in.Harness != "" && in.Project != "" && underRoot(root, in.Project) {
			names[in.Harness] = true
		}
	}
	var ids []string
	for id := range names {
		ids = append(ids, id)
	}
	computed := []string{}
	if len(ids) > 0 {
		computed = also.InfraIgnoreEntries(ids)
	}
	entries := also.UniqueStrings(append(readManagedIgnores(root), computed...))
	if err := writeManagedIgnores(root, also.IgnoreInfra, true, entries); err != nil {
		return err
	}
	return writeManagedIgnores(root, also.IgnoreAlso, true, also.AlsoIgnoreEntries())
}

// dropManagedHarness removes one harness's entries after its last install in
// this git root. Entries another installed harness still needs stay.
func dropManagedHarness(project, harness string) error {
	h, ok := also.ResolveHarness(harness)
	if !ok {
		return nil
	}
	root := managedIgnoreRoot(project)
	st, err := also.LoadState()
	if err != nil {
		return nil
	}
	remaining := map[string]bool{}
	for _, in := range st.Installs {
		if in.Harness != "" && in.Project != "" && underRoot(root, in.Project) {
			remaining[in.Harness] = true
		}
	}
	var ids []string
	for id := range remaining {
		ids = append(ids, id)
	}
	remove := map[string]bool{}
	for _, e := range also.InfraIgnoreEntries([]string{h.ID}) {
		remove[e] = true
	}
	keep := map[string]bool{}
	if len(ids) > 0 {
		// InfraIgnoreEntries(nil) means "every adapter"; an empty remainder
		// must mean no harness at all.
		for _, e := range also.InfraIgnoreEntries(ids) {
			keep[e] = true
		}
	}
	var entries []string
	for _, e := range readManagedIgnores(root) {
		if remove[e] && !keep[e] {
			continue
		}
		entries = append(entries, e)
	}
	return writeManagedIgnores(root, also.IgnoreInfra, len(entries) > 0, entries)
}

func managedIgnoreRoot(project string) string {
	root, err := also.GitProjectRoot(project)
	if err != nil {
		// Archive/tarball checkouts have no .git; still keep a managed block
		// next to the project.
		return project
	}
	return root
}

func underRoot(root, path string) bool {
	abs, err := also.AbsPath(path)
	if err != nil {
		abs = path
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func readManagedIgnores(root string) []string {
	entries, _ := also.IgnoreBlockEntriesAt(filepath.Join(root, ".gitignore"), also.IgnoreInfra)
	return entries
}

func writeManagedIgnores(root string, kind also.IgnoreKind, add bool, entries []string) error {
	if _, err := also.GitProjectRoot(root); err == nil {
		_, err := also.UpdateGitIgnore(root, kind, add, entries)
		return err
	}
	_, err := also.UpdateGitIgnoreAt(filepath.Join(root, ".gitignore"), kind, add, entries)
	return err
}
