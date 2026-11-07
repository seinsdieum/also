package also

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
)

// HealthCheck is one doctor/verify result.
type HealthCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok, warn, fail
	Detail string `json:"detail,omitempty"`
}

func (c HealthCheck) String() string {
	if c.Detail == "" {
		return fmt.Sprintf("%-5s %s", c.Status, c.Name)
	}
	return fmt.Sprintf("%-5s %s: %s", c.Status, c.Name, c.Detail)
}

// Doctor checks the local install: git, config, adapters, caches and state.
func Doctor(cfg *Config, st *State, refresh bool) []HealthCheck {
	var out []HealthCheck
	add := func(name, status, detail string) {
		out = append(out, HealthCheck{Name: name, Status: status, Detail: detail})
	}
	if _, err := exec.LookPath("git"); err != nil {
		add("git", "fail", "git is not on PATH")
	} else {
		add("git", "ok", "found")
	}
	if len(cfg.Namespaces) == 0 {
		add("namespaces", "warn", "none connected; run: also connect <name> <git-url>")
	}
	for _, name := range cfg.NamespaceNames() {
		ns := cfg.Namespaces[name]
		if refresh {
			if _, err := OpenSet(name, ns, true); err != nil {
				add("namespace "+name, "fail", err.Error())
			} else {
				add("namespace "+name, "ok", ns.Remote)
			}
			continue
		}
		dir := SetCachePath(ns.Remote)
		if !isGitRepo(dir) {
			add("namespace "+name, "warn", "not cached yet; it will be cloned on first use")
			continue
		}
		if dirty, status, err := gitDirty(dir); err != nil {
			add("namespace "+name, "fail", err.Error())
		} else if dirty {
			add("namespace "+name, "warn", "cached clone is dirty (run: also repair "+name+"):\n"+status)
		} else {
			add("namespace "+name, "ok", ns.Remote)
		}
	}
	if cfg.DefaultNamespace != "" {
		if _, ok := cfg.Namespaces[cfg.DefaultNamespace]; !ok {
			add("default namespace", "fail", cfg.DefaultNamespace+" is not connected")
		}
	}
	if cfg.DefaultHarness != "" {
		if _, ok := ResolveHarness(cfg.DefaultHarness); !ok {
			add("default harness", "fail", "unknown harness "+cfg.DefaultHarness)
		}
	}
	if err := ValidateAdapters(Harnesses()); err != nil {
		add("adapters", "fail", err.Error())
	} else {
		add("adapters", "ok", fmt.Sprintf("%d loaded", len(Harnesses())))
	}
	if st == nil {
		add("state", "warn", "no state loaded")
		return out
	}
	add("state", "ok", fmt.Sprintf("%d install(s)", len(st.Installs)))
	for _, in := range st.Installs {
		label := fmt.Sprintf("%s/%s (%s)", in.Set, in.Package, in.Harness)
		if in.Scope == "global" {
			add("install "+label, "warn", "legacy global install; remove it with: also uninstall "+in.Package+" --legacy-global")
			continue
		}
		if in.Scope == "project" && in.Project == "" {
			add("install "+label, "fail", "project scope without a project path")
			continue
		}
		missing, modified := 0, 0
		for _, f := range in.Files {
			data, err := os.ReadFile(f.Path)
			if err != nil {
				if os.IsNotExist(err) {
					missing++
				}
				continue
			}
			if ShortHash(data, 64) != f.SHA {
				modified++
			}
		}
		if missing > 0 {
			add("install "+label, "warn", fmt.Sprintf("%d installed file(s) missing", missing))
		}
		if modified > 0 {
			add("install "+label, "warn", fmt.Sprintf("%d installed file(s) modified", modified))
		}
	}
	return out
}

// Verify checks namespace contents, version tags and state references.
func Verify(cfg *Config, st *State, setName string, refresh bool) []HealthCheck {
	var out []HealthCheck
	add := func(name, status, detail string) {
		out = append(out, HealthCheck{Name: name, Status: status, Detail: detail})
	}
	names := cfg.NamespaceNames()
	if setName != "" {
		name, ns, err := cfg.ResolveSet(setName)
		if err != nil {
			return []HealthCheck{{Name: "namespace", Status: "fail", Detail: err.Error()}}
		}
		if _, ok := cfg.Namespaces[name]; !ok {
			cfg.Namespaces[name] = ns
		}
		names = []string{name}
	}
	if len(names) == 0 {
		return []HealthCheck{{Name: "namespaces", Status: "fail", Detail: "none connected"}}
	}
	for _, name := range names {
		ns := cfg.Namespaces[name]
		repo, err := OpenSet(name, ns, refresh)
		if err != nil {
			add("namespace "+name, "fail", err.Error())
			continue
		}
		if dirty, status, err := gitDirty(repo.Dir); err != nil {
			add("namespace "+name, "fail", err.Error())
		} else if dirty {
			add("namespace "+name, "warn", "cached clone is dirty (run: also repair "+name+"):\n"+status)
		}
		pkgs, err := repo.Packages()
		if err != nil {
			add("namespace "+name, "fail", err.Error())
			continue
		}
		versions := map[string][]VersionInfo{}
		broken := 0
		type loadJob struct{ pkg, version string }
		var jobs []loadJob
		for _, pkg := range pkgs {
			vs, err := repo.Versions(pkg)
			if err != nil {
				add(name+"/"+pkg, "fail", err.Error())
				broken++
				continue
			}
			versions[pkg] = vs
			for _, v := range vs {
				jobs = append(jobs, loadJob{pkg, v.Version})
			}
		}
		// Each version needs its own git archive; bound the parallelism so a
		// namespace with many versions does not spawn hundreds of processes.
		errs := make([]error, len(jobs))
		var wg sync.WaitGroup
		sem := make(chan struct{}, 4)
		for i, job := range jobs {
			wg.Add(1)
			sem <- struct{}{}
			go func(i int, j loadJob) {
				defer wg.Done()
				defer func() { <-sem }()
				_, err := repo.LoadPackage(j.pkg, j.version)
				errs[i] = err
			}(i, job)
		}
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				add(name+"/"+jobs[i].pkg+"@"+jobs[i].version, "fail", err.Error())
				broken++
			}
		}
		if broken == 0 {
			add("namespace "+name, "ok", fmt.Sprintf("%d package(s) verified", len(pkgs)))
		}
		if st == nil {
			continue
		}
		for _, in := range st.Installs {
			if in.Set != name {
				continue
			}
			label := fmt.Sprintf("state %s/%s@%s", name, in.Package, in.Version)
			vs, ok := versions[in.Package]
			if !ok {
				add(label, "fail", "package is not in the namespace")
				continue
			}
			found := false
			for _, v := range vs {
				if v.Version == in.Version {
					found = true
					break
				}
			}
			if !found {
				add(label, "fail", "installed version is missing from the namespace")
				continue
			}
			missing := 0
			for _, f := range in.Files {
				if _, err := os.Stat(f.Path); os.IsNotExist(err) {
					missing++
				}
			}
			if missing > 0 {
				add(label, "warn", fmt.Sprintf("%d installed file(s) missing", missing))
			} else {
				add(label, "ok", in.Harness)
			}
		}
	}
	return out
}

// HasFailures reports whether any check failed.
func HasFailures(checks []HealthCheck) bool {
	for _, c := range checks {
		if c.Status == "fail" {
			return true
		}
	}
	return false
}
