package also

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(Release{TagName: "v1.1.0"})
	}))
	defer srv.Close()
	old := releaseAPIURL
	releaseAPIURL = srv.URL
	defer func() { releaseAPIURL = old }()

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
	sum := sha256.Sum256(data)
	sums := hex.EncodeToString(sum[:]) + "  " + asset + "\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			_ = json.NewEncoder(w).Encode(Release{
				TagName: "v1.1.0",
				Assets: []ReleaseAsset{
					{Name: asset, BrowserDownloadURL: "http://" + r.Host + "/asset"},
					{Name: "SHA256SUMS", BrowserDownloadURL: "http://" + r.Host + "/sums"},
				},
			})
		case "/asset":
			_, _ = w.Write(data)
		case "/sums":
			_, _ = w.Write([]byte(sums))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := releaseAPIURL
	releaseAPIURL = srv.URL + "/latest"
	defer func() { releaseAPIURL = old }()

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
