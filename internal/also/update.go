package also

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// UpdateCheckInterval controls both the GitHub lookup and the one-time
	// "new version" notice.
	UpdateCheckInterval = 24 * time.Hour

	latestReleaseDefault = "https://github.com/seinsdieum/also/releases/latest"
	downloadBaseDefault  = "https://github.com/seinsdieum/also/releases/download"
)

func latestReleaseURL() string {
	if value := os.Getenv("ALSO_RELEASE_LATEST_URL"); value != "" {
		return value
	}
	return latestReleaseDefault
}

func releaseDownloadBase() string {
	if value := os.Getenv("ALSO_RELEASE_DOWNLOAD_BASE"); value != "" {
		return strings.TrimRight(value, "/")
	}
	return downloadBaseDefault
}

// LatestTag reads the newest release tag from the releases/latest redirect.
// Using the redirect avoids the unauthenticated GitHub API rate limit. GitHub
// occasionally closes a connection, so retry a few times.
func LatestTag(ctx context.Context) (string, error) {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		tag, err := latestTagOnce(ctx)
		if err == nil {
			return tag, nil
		}
		last = err
		if strings.Contains(err.Error(), "no published release") || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
			return "", last
		case <-time.After(time.Duration(attempt+1) * 300 * time.Millisecond):
		}
	}
	return "", last
}

func latestTagOnce(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "also/"+Version)
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("check latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("no published release yet")
	}
	location := resp.Header.Get("Location")
	if location == "" {
		if resp.StatusCode == http.StatusOK {
			location = resp.Request.URL.String()
		}
	}
	if location == "" {
		return "", fmt.Errorf("check latest release: %s", resp.Status)
	}
	base, err := url.Parse(latestReleaseURL())
	if err != nil {
		return "", err
	}
	next, err := base.Parse(location)
	if err != nil {
		return "", err
	}
	parts := strings.Split(strings.TrimRight(next.Path, "/"), "/")
	tag := parts[len(parts)-1]
	if tag == "" || tag == "latest" || tag == "releases" {
		return "", fmt.Errorf("latest release redirect has no tag")
	}
	return tag, nil
}

// CompareVersions compares dotted versions. It returns -1, 0 or 1.
func CompareVersions(a, b string) int {
	an, ap := splitVersion(a)
	bn, bp := splitVersion(b)
	for i := 0; i < len(an) || i < len(bn); i++ {
		var av, bv int
		if i < len(an) {
			av = an[i]
		}
		if i < len(bn) {
			bv = bn[i]
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	switch {
	case ap == "" && bp != "":
		return 1
	case ap != "" && bp == "":
		return -1
	case ap < bp:
		return -1
	case ap > bp:
		return 1
	}
	return 0
}

func splitVersion(v string) ([]int, string) {
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
	v = strings.SplitN(v, "+", 2)[0]
	pre := ""
	if i := strings.IndexByte(v, '-'); i >= 0 {
		pre = v[i+1:]
		v = v[:i]
	}
	var nums []int
	for _, part := range strings.Split(v, ".") {
		n := 0
		for _, r := range part {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		nums = append(nums, n)
	}
	return nums, pre
}

type updateCache struct {
	CheckedAt  time.Time `json:"checked_at"`
	Latest     string    `json:"latest"`
	NotifiedAt time.Time `json:"notified_at"`
}

func updateCachePath() string { return filepath.Join(HomeDir(), "update-check.json") }

func readUpdateCache() updateCache {
	var c updateCache
	data, err := os.ReadFile(updateCachePath())
	if err == nil {
		_ = json.Unmarshal(data, &c)
	}
	return c
}

func writeUpdateCache(c updateCache) {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	_ = atomicWrite(updateCachePath(), append(data, '\n'), 0o600)
}

// CheckLatestVersion returns the newest release and whether the user should be
// told about it. The lookup and the notice each happen at most once per
// UpdateCheckInterval.
func CheckLatestVersion(current string, now time.Time, force bool) (string, bool, error) {
	c := readUpdateCache()
	fresh := !c.CheckedAt.IsZero() && now.Sub(c.CheckedAt) < UpdateCheckInterval
	latest := c.Latest
	if force || !fresh {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		tag, err := LatestTag(ctx)
		if err != nil {
			// Cache the miss too, so an offline CLI does not retry on every
			// command and a deleted release cannot linger in the cache.
			c.CheckedAt = now
			c.Latest = ""
			writeUpdateCache(c)
			return "", false, nil
		}
		latest = tag
		c.CheckedAt = now
		c.Latest = latest
		writeUpdateCache(c)
	}
	if latest == "" || CompareVersions(latest, current) <= 0 {
		return latest, false, nil
	}
	notify := force || c.NotifiedAt.Before(c.CheckedAt)
	if notify {
		c.NotifiedAt = now
		writeUpdateCache(c)
	}
	return latest, notify, nil
}

func releaseAssetName(goos, goarch string) string {
	name := "also-" + goos + "-" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

type UpdateResult struct {
	Current    string `json:"current"`
	Latest     string `json:"latest"`
	Updated    bool   `json:"updated"`
	Reinstall  bool   `json:"reinstall,omitempty"`
	ManualPath string `json:"manual_path,omitempty"`
	Asset      string `json:"asset,omitempty"`
}

// reinstallHint tells users on a build newer than the published release how
// to get back onto the release line without an in-place update.
const ReinstallHint = "curl -fsSL https://raw.githubusercontent.com/seinsdieum/also/main/install.sh | sh"

// shortcut: SHA256SUMS is fetched from the same release as the binary, so a
// compromised release can serve both; upgrade to attestation verification or
// an out-of-band signed checksum when a signing key exists.
//
// ApplyUpdate downloads the latest release and replaces exe. On Windows the
// running executable cannot be replaced, so the new binary is written next to
// it and returned as ManualPath.
func ApplyUpdate(current, exe string) (*UpdateResult, error) {
	return applyUpdate(current, exe, runtime.GOOS == "windows")
}

func applyUpdate(current, exe string, windows bool) (*UpdateResult, error) {
	res := &UpdateResult{Current: current}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	tag, err := LatestTag(ctx)
	if err != nil {
		return res, err
	}
	res.Latest = tag
	if cmp := CompareVersions(tag, current); cmp < 0 {
		res.Reinstall = true
		return res, nil
	} else if cmp == 0 {
		return res, nil
	}
	assetName := releaseAssetName(runtime.GOOS, runtime.GOARCH)
	base := releaseDownloadBase() + "/" + tag
	res.Asset = assetName
	data, err := downloadAsset(ctx, base+"/"+assetName)
	if err != nil {
		return res, err
	}
	sumData, err := downloadAsset(ctx, base+"/SHA256SUMS")
	if err != nil {
		return res, err
	}
	if err := verifyAsset(data, assetName, sumData); err != nil {
		return res, err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if windows {
		path := exe + ".new"
		if err := os.WriteFile(path, data, 0o755); err != nil {
			return res, err
		}
		res.ManualPath = path
		return res, nil
	}
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".also-update-*")
	if err != nil {
		return res, fmt.Errorf("write next to %s: %w (try reinstalling with install.sh)", exe, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return res, err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return res, err
	}
	if err := tmp.Close(); err != nil {
		return res, err
	}
	if err := os.Rename(tmpName, exe); err != nil {
		return res, fmt.Errorf("replace %s: %w", exe, err)
	}
	res.Updated = true
	return res, nil
}

func downloadAsset(ctx context.Context, url string) ([]byte, error) {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		data, err := downloadAssetOnce(ctx, url)
		if err == nil {
			return data, nil
		}
		last = err
		if ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
			return nil, last
		case <-time.After(time.Duration(attempt+1) * 300 * time.Millisecond):
		}
	}
	return nil, last
}

func downloadAssetOnce(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "also/"+Version)
	resp, err := (&http.Client{Timeout: 90 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 128<<20))
}

func verifyAsset(data []byte, name string, sums []byte) error {
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.TrimPrefix(fields[1], "*") == name {
			want = strings.ToLower(fields[0])
			break
		}
	}
	if want == "" {
		return fmt.Errorf("SHA256SUMS has no entry for %s", name)
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if got != want {
		return fmt.Errorf("checksum mismatch for %s", name)
	}
	return nil
}

// MarkUpdateSeen records a successful manual update so the periodic notice
// does not immediately repeat.
func MarkUpdateSeen(latest string) {
	now := time.Now()
	writeUpdateCache(updateCache{CheckedAt: now, Latest: latest, NotifiedAt: now})
}
