package also

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func setReleaseServer(t *testing.T, tag string, asset string, data []byte) *httptest.Server {
	t.Helper()
	sum := sha256.Sum256(data)
	sums := hex.EncodeToString(sum[:]) + "  " + asset + "\n"
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			http.Redirect(w, r, "/tag/"+tag, http.StatusFound)
		case "/download/" + tag + "/" + asset:
			_, _ = w.Write(data)
		case "/download/" + tag + "/SHA256SUMS":
			_, _ = w.Write([]byte(sums))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ALSO_RELEASE_LATEST_URL", srv.URL+"/latest")
	t.Setenv("ALSO_RELEASE_DOWNLOAD_BASE", srv.URL+"/download")
	return srv
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "v1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.1.0", -1},
		{"1.0.0", "1.0.0-rc.1", 1},
		{"1.0.0-rc.1", "1.0.0-rc.2", -1},
		{"2.0", "1.9.9", 1},
	}
	for _, tc := range cases {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Fatalf("CompareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestCheckLatestVersionNotifiesOncePerInterval(t *testing.T) {
	t.Setenv("ALSO_HOME", filepath.Join(t.TempDir(), "also"))
	calls := 0
	old := latestReleaseURL()
	defer func() {
		t.Setenv("ALSO_RELEASE_LATEST_URL", old)
	}()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Redirect(w, r, "/tag/v1.1.0", http.StatusFound)
	}))
	defer srv.Close()
	t.Setenv("ALSO_RELEASE_LATEST_URL", srv.URL+"/latest")

	now := time.Now()
	latest, notify, err := CheckLatestVersion("1.0.0", now, false)
	if err != nil || latest != "v1.1.0" || !notify {
		t.Fatalf("first check: %q %v %v", latest, notify, err)
	}
	if calls != 1 {
		t.Fatalf("expected one request, got %d", calls)
	}
	if _, notify, _ := CheckLatestVersion("1.0.0", now, false); notify {
		t.Fatal("notice repeated inside the interval")
	}
	if calls != 1 {
		t.Fatalf("cache was not used, got %d requests", calls)
	}
	_, notify, err = CheckLatestVersion("1.0.0", now.Add(25*time.Hour), false)
	if err != nil || !notify || calls != 2 {
		t.Fatalf("next interval: notify=%v err=%v calls=%d", notify, err, calls)
	}
}

func TestApplyUpdateReplacesBinary(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "also")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	asset := releaseAssetName(runtime.GOOS, runtime.GOARCH)
	data := []byte("new binary")
	setReleaseServer(t, "v1.1.0", asset, data)

	res, err := applyUpdate("1.0.0", exe, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Updated || res.Latest != "v1.1.0" {
		t.Fatalf("unexpected result: %+v", res)
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new binary" {
		t.Fatalf("binary was not replaced: %q", got)
	}
}

func TestApplyUpdateRejectsBadChecksum(t *testing.T) {
	data := []byte("new binary")
	err := verifyAsset(data, "also-test", []byte("0000  also-test\n"))
	if err == nil {
		t.Fatal("expected a checksum mismatch")
	}
}

func TestLatestTagRejectsMissingRelease(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	t.Setenv("ALSO_RELEASE_LATEST_URL", srv.URL+"/latest")
	if _, err := LatestTag(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}
