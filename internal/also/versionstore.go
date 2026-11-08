package also

// Version storage: one live tree per package plus immutable git tags.
//
//   packages/<name>/also-pkg.json
//   packages/<name>/origin/...
//   refs/tags/also/<name>/<version> -> commit holding that snapshot
//
// Old versions are git history, not duplicated directories. A legacy set that
// stored every version under packages/<name>/<version>/ is still readable and
// can be converted with `also connect <name> <git-url> --migrate`.

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var versionRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func validVersion(v string) bool {
	return versionRe.MatchString(v) && !strings.Contains(v, "..")
}

func versionTagName(pkg, version string) string {
	return "also/" + pkg + "/" + version
}

func versionTagRef(pkg, version string) string {
	return "refs/tags/" + versionTagName(pkg, version)
}

// Semantic-version aliases are mutable tags that point at one immutable
// content-addressed version. The content version stays canonical; the alias
// only answers "give me the latest 2.x".
func semverTagName(pkg, semver string) string {
	return "also/" + pkg + "/semver/" + semver
}

func semverTagRef(pkg, semver string) string {
	return "refs/tags/" + semverTagName(pkg, semver)
}

// listSemverTags maps an alias to the commit it points at.
func listSemverTags(dir, pkg string) (map[string]string, error) {
	prefix := "refs/tags/" + semverTagName(pkg, "")
	out, err := runGit(dir, "for-each-ref", "--format=%(refname) %(objectname) %(*objectname)", prefix)
	if err != nil {
		return nil, err
	}
	tags := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		semver := strings.TrimPrefix(fields[0], prefix)
		if _, ok := ParseSemver(semver); !ok {
			continue
		}
		commit := fields[1]
		if len(fields) > 2 && fields[2] != "" {
			commit = fields[2]
		}
		tags[semver] = commit
	}
	return tags, nil
}

// SemverAliases maps alias -> content version by matching commits.
func (s *SetRepo) SemverAliases(pkg string) (map[string]string, error) {
	versions, err := listVersionTags(s.Dir, pkg)
	if err != nil {
		return nil, err
	}
	aliases, err := listSemverTags(s.Dir, pkg)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for semver, commit := range aliases {
		for version, vc := range versions {
			if vc == commit {
				out[semver] = version
				break
			}
		}
	}
	return out, nil
}

// SetSemver points an alias at an existing content version and pushes the tag.
// Aliases are immutable by default; allowMove is only for the explicit
// "also tag --move" path and still refuses to overwrite a remote change that
// this clone has not seen.
func (s *SetRepo) SetSemver(pkg, semver, version string, allowMove bool) error {
	if _, ok := ParseSemver(semver); !ok {
		return fmt.Errorf("invalid semantic version %q; use MAJOR.MINOR.PATCH", semver)
	}
	commit, ok := s.tagCommit(pkg, version)
	if !ok {
		return fmt.Errorf("version %s of %q has no content tag", version, pkg)
	}
	current, exists := s.refCommit(semverTagRef(pkg, semver))
	moving := exists && current != commit
	remote := ""
	if moving {
		if !allowMove {
			return fmt.Errorf("semantic alias %s of %q already points at another version; use also tag --move", semver, pkg)
		}
		remote = remoteTagCommit(s.Dir, semverTagName(pkg, semver))
		if remote != "" && remote != current {
			return fmt.Errorf("semantic alias %s of %q moved on the remote; fetch it before moving", semver, pkg)
		}
	}
	if _, err := runGit(s.Dir, "update-ref", semverTagRef(pkg, semver), commit); err != nil {
		return err
	}
	if moving {
		if err := pushTagWithLease(s.Dir, semverTagName(pkg, semver), remote); err != nil {
			_, _ = runGit(s.Dir, "update-ref", semverTagRef(pkg, semver), current)
			s.invalidateVersionData()
			return err
		}
	} else if err := pushTag(s.Dir, semverTagName(pkg, semver)); err != nil {
		return err
	}
	s.invalidateVersionData()
	return nil
}

func (s *SetRepo) refCommit(ref string) (string, bool) {
	out, err := runGit(s.Dir, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(out), true
}

func remoteTagCommit(dir, tag string) string {
	out, err := runGit(dir, "ls-remote", "--tags", "origin", "refs/tags/"+tag)
	if err != nil {
		return ""
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func (s *SetRepo) tagCommit(pkg, version string) (string, bool) {
	out, err := runGit(s.Dir, "rev-parse", "--verify", versionTagRef(pkg, version)+"^{commit}")
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(out), true
}

func listVersionTags(dir, pkg string) (map[string]string, error) {
	prefix := "refs/tags/" + versionTagName(pkg, "")
	out, err := runGit(dir, "for-each-ref", "--format=%(refname) %(objectname) %(*objectname)", prefix)
	if err != nil {
		return nil, err
	}
	tags := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		ref := fields[0]
		version := strings.TrimPrefix(ref, prefix)
		if !validVersion(version) {
			continue
		}
		commit := fields[1]
		if len(fields) > 2 && fields[2] != "" {
			commit = fields[2]
		}
		tags[version] = commit
	}
	return tags, nil
}

type commitInfo struct {
	Time  time.Time
	Order int
}

func commitTimes(dir string) (map[string]commitInfo, error) {
	out, err := runGit(dir, "log", "--all", "--format=%H %ct")
	if err != nil {
		if !hasCommits(dir) {
			return map[string]commitInfo{}, nil
		}
		return nil, err
	}
	info := map[string]commitInfo{}
	seq := 0
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		var ts int64
		if _, err := fmt.Sscanf(fields[1], "%d", &ts); err != nil {
			continue
		}
		info[fields[0]] = commitInfo{Time: time.Unix(ts, 0), Order: seq}
		seq++
	}
	return info, nil
}

func readManifestAtCommit(dir, pkg, version, commit string) (PackageMeta, bool) {
	var meta PackageMeta
	paths := []string{"packages/" + pkg + "/" + PackageManifestFile}
	if version != "" {
		paths = append(paths, "packages/"+pkg+"/"+version+"/"+PackageManifestFile)
	}
	for _, p := range paths {
		data, err := runGitBytes(dir, "show", commit+":"+p)
		if err != nil {
			continue
		}
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		return meta, true
	}
	return meta, false
}

func legacyVersionDirs(dir, pkg string) (map[string]bool, error) {
	if !hasCommits(dir) {
		return map[string]bool{}, nil
	}
	out, err := runGit(dir, "ls-tree", "-r", "--name-only", "HEAD", "--", "packages/"+pkg)
	if err != nil {
		return nil, err
	}
	prefix := "packages/" + pkg + "/"
	versions := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		rest := strings.TrimPrefix(strings.TrimSpace(line), prefix)
		parts := strings.Split(rest, "/")
		if len(parts) == 2 && parts[1] == PackageManifestFile && validVersion(parts[0]) {
			versions[parts[0]] = true
		}
	}
	return versions, nil
}

// ensureTagsForCurrentTree tags the live tree and every legacy version dir at
// HEAD before a push replaces them.
func (s *SetRepo) ensureTagsForCurrentTree(pkg string) error {
	legacy, err := legacyVersionDirs(s.Dir, pkg)
	if err != nil {
		return err
	}
	var times map[string]time.Time
	if len(legacy) > 0 {
		if versions, err := s.Versions(pkg); err == nil {
			times = map[string]time.Time{}
			for _, v := range versions {
				times[v.Version] = v.Time
			}
		}
	}
	if meta, ok := readManifestAtCommit(s.Dir, pkg, "", "HEAD"); ok && meta.Digest != "" {
		version := VersionFromDigest(meta.Digest)
		if _, exists := s.tagCommit(pkg, version); !exists {
			if err := s.createTag(pkg, version, "HEAD"); err != nil {
				return err
			}
		}
	}
	for version := range legacy {
		if _, exists := s.tagCommit(pkg, version); exists {
			continue
		}
		if err := s.createTagAt(pkg, version, "HEAD", times[version]); err != nil {
			return err
		}
	}
	return nil
}

func (s *SetRepo) createTag(pkg, version, rev string) error {
	_, err := runGit(s.Dir, "update-ref", versionTagRef(pkg, version), rev)
	if err == nil {
		s.invalidateVersionData()
	}
	return err
}

// createTagAt creates an annotated tag carrying the original version time.
// Lightweight tags rely on the package manifest instead.
func (s *SetRepo) createTagAt(pkg, version, rev string, created time.Time) error {
	if created.IsZero() {
		return s.createTag(pkg, version, rev)
	}
	msg, err := json.Marshal(struct {
		CreatedAt time.Time `json:"created_at"`
	}{created})
	if err != nil {
		return err
	}
	args := gitCommitArgs(s.Dir, "tag", "-f", "-a", "-m", string(msg), versionTagName(pkg, version), rev)
	_, err = runGit(s.Dir, args...)
	if err == nil {
		s.invalidateVersionData()
	}
	return err
}

// readManifestBlobs reads the live (or legacy) manifest of every version tag
// in one git process instead of one git show per tag.
func readManifestBlobs(dir, pkg string, tags map[string]string) (map[string]PackageMeta, error) {
	specs := make([]string, 0, len(tags)*2)
	spec := func(commit, version string) []string {
		return []string{
			commit + ":packages/" + pkg + "/" + PackageManifestFile,
			commit + ":packages/" + pkg + "/" + version + "/" + PackageManifestFile,
		}
	}
	for version, commit := range tags {
		specs = append(specs, spec(commit, version)...)
	}
	blobs, err := gitBatchBlobs(dir, specs)
	if err != nil {
		return nil, err
	}
	out := map[string]PackageMeta{}
	for version, commit := range tags {
		for _, s := range spec(commit, version) {
			data, ok := blobs[s]
			if !ok {
				continue
			}
			var meta PackageMeta
			if json.Unmarshal(data, &meta) == nil {
				out[version] = meta
				break
			}
		}
	}
	return out, nil
}

// tagCreatedTimes reads every annotated tag's recorded creation time at once.
func tagCreatedTimes(dir, pkg string) map[string]time.Time {
	out := map[string]time.Time{}
	prefix := "refs/tags/" + versionTagName(pkg, "")
	lines, err := runGit(dir, "for-each-ref", "--format=%(refname) %(objecttype) %(contents)", prefix)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(lines, "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), " ", 3)
		if len(fields) < 3 || fields[1] != "tag" {
			continue
		}
		var meta struct {
			CreatedAt time.Time `json:"created_at"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(fields[2])), &meta) != nil {
			continue
		}
		version := strings.TrimPrefix(fields[0], prefix)
		if validVersion(version) && !meta.CreatedAt.IsZero() {
			out[version] = meta.CreatedAt
		}
	}
	return out
}

func (s *SetRepo) loadPackageFromCommit(pkg, version, commit string) (*Package, error) {
	// Tar headers add overhead; the exact limits are enforced while unpacking.
	data, err := runGitBytesLimit(s.Dir, maxPackageBytes*2, "archive", "--format=tar", commit, "--", "packages/"+pkg)
	if err != nil {
		return nil, err
	}
	return packageFromTar(data, pkg, version, fmt.Sprintf("%s@%s", pkg, version))
}

type tarFile struct {
	name string
	data []byte
	mode fs.FileMode
}

func packageFromTar(data []byte, pkg, version, source string) (*Package, error) {
	tr := tar.NewReader(bytes.NewReader(data))
	var entries []tarFile
	var total int64
	prefix := "packages/" + pkg + "/"
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read archive for %s: %w", source, err)
		}
		name := path.Clean(strings.TrimPrefix(hdr.Name, "./"))
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg:
		case tar.TypeSymlink, tar.TypeLink:
			return nil, fmt.Errorf("package %s contains a link, refusing to install: %s", source, name)
		default:
			return nil, fmt.Errorf("package %s contains a special file: %s", source, name)
		}
		total += hdr.Size
		if hdr.Size > maxFileSize || total > maxPackageBytes {
			return nil, fmt.Errorf("package %s exceeds the load limit (%s per file, %s total)", source,
				humanBytes(maxFileSize), humanBytes(maxPackageBytes))
		}
		body, err := io.ReadAll(io.LimitReader(tr, maxFileSize+1))
		if err != nil {
			return nil, err
		}
		if int64(len(body)) > maxFileSize {
			return nil, fmt.Errorf("package %s exceeds the load limit (%s per file)", source, humanBytes(maxFileSize))
		}
		mode := fs.FileMode(0o644)
		if hdr.Mode&0o111 != 0 {
			mode = 0o755
		}
		entries = append(entries, tarFile{name: name, data: body, mode: mode})
	}

	byName := map[string]tarFile{}
	for _, e := range entries {
		byName[e.name] = e
	}
	newRoot := prefix
	oldRoot := prefix + version + "/"
	root := newRoot
	if _, ok := byName[newRoot+PackageManifestFile]; !ok {
		root = oldRoot
	}
	manifest, ok := byName[root+PackageManifestFile]
	if !ok {
		return nil, fmt.Errorf("package %s has no %s", source, PackageManifestFile)
	}
	var meta PackageMeta
	if err := json.Unmarshal(manifest.data, &meta); err != nil {
		return nil, fmt.Errorf("parse %s: %w", source, err)
	}
	if meta.Name != pkg {
		return nil, fmt.Errorf("package manifest name %q does not match %q", meta.Name, pkg)
	}
	files := map[string]*SnapshotFile{}
	origin := root + OriginDir + "/"
	for name, e := range byName {
		if !strings.HasPrefix(name, origin) {
			continue
		}
		rel := strings.TrimPrefix(name, origin)
		if err := SafeRel(rel); err != nil {
			return nil, err
		}
		files[rel] = &SnapshotFile{Data: e.data, Mode: e.mode}
	}
	return NewPackage(meta, files, source)
}

// removePackageTree deletes the live package tree, keeping git history and tags.
func (s *SetRepo) removePackageTree(pkg string) error {
	return os.RemoveAll(filepath.Join(s.Dir, "packages", pkg))
}
