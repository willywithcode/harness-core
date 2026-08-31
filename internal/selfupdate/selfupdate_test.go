package selfupdate

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
)

// newFakeGitHub starts an httptest server that serves one release with one
// binary asset (binContent) for the current GOOS/GOARCH, plus its correct
// or deliberately wrong checksum sidecar, and returns the server and the
// asset name it published.
func newFakeGitHub(t *testing.T, tagName string, binContent []byte, badChecksum bool) (*httptest.Server, string) {
	t.Helper()

	assetName := AssetName(runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(binContent)
	checksum := hex.EncodeToString(sum[:])
	if badChecksum {
		checksum = "0000000000000000000000000000000000000000000000000000000000000000"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/assets/bin", func(w http.ResponseWriter, r *http.Request) {
		w.Write(binContent)
	})
	mux.HandleFunc("/assets/sum", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(checksum + "\n"))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("/repos/owner/repo/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		rel := release{
			TagName: tagName,
			Assets: []asset{
				{Name: assetName, BrowserDownloadURL: server.URL + "/assets/bin"},
				{Name: assetName + ".sha256", BrowserDownloadURL: server.URL + "/assets/sum"},
			},
		}
		json.NewEncoder(w).Encode(rel)
	})

	return server, assetName
}

func testClient(t *testing.T, server *httptest.Server, exePath string) *Client {
	t.Helper()
	c := NewClient("owner/repo")
	c.APIBaseURL = server.URL
	c.ExecutablePath = func() (string, error) { return exePath, nil }
	return c
}

func TestPlan_UpdateAvailable(t *testing.T) {
	server, assetName := newFakeGitHub(t, "v0.2.0", []byte("new binary content"), false)
	c := testClient(t, server, "")

	plan, err := c.Plan("0.1.0")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.UpToDate {
		t.Fatal("expected UpToDate=false for 0.1.0 -> 0.2.0")
	}
	if plan.LatestVersion != "0.2.0" {
		t.Fatalf("LatestVersion = %q, want 0.2.0", plan.LatestVersion)
	}
	if plan.AssetName != assetName {
		t.Fatalf("AssetName = %q, want %q", plan.AssetName, assetName)
	}
}

func TestPlan_AlreadyUpToDate(t *testing.T) {
	server, _ := newFakeGitHub(t, "v0.1.0", []byte("same content"), false)
	c := testClient(t, server, "")

	plan, err := c.Plan("0.1.0")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !plan.UpToDate {
		t.Fatal("expected UpToDate=true when tag matches current version")
	}
}

func TestPlan_MissingAsset(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(release{TagName: "v0.2.0", Assets: nil})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	c := testClient(t, server, "")
	_, err := c.Plan("0.1.0")
	if err == nil {
		t.Fatal("expected an error when the release has no matching asset")
	}
}

func TestApply_HappyPath(t *testing.T) {
	newContent := []byte("this is the new harness-core binary")
	server, _ := newFakeGitHub(t, "v0.2.0", newContent, false)

	dir := t.TempDir()
	exePath := filepath.Join(dir, "harness-core")
	if err := os.WriteFile(exePath, []byte("old binary content"), 0o755); err != nil {
		t.Fatalf("seeding old executable: %v", err)
	}

	c := testClient(t, server, exePath)
	plan, err := c.Plan("0.1.0")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if err := c.Apply(plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatalf("reading replaced executable: %v", err)
	}
	if string(got) != string(newContent) {
		t.Fatalf("executable content = %q, want %q", got, newContent)
	}

	info, err := os.Stat(exePath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("replaced executable is not executable: mode %v", info.Mode())
	}
}

func TestApply_ChecksumMismatchRejected(t *testing.T) {
	newContent := []byte("this is the new harness-core binary")
	server, _ := newFakeGitHub(t, "v0.2.0", newContent, true) // bad checksum

	dir := t.TempDir()
	exePath := filepath.Join(dir, "harness-core")
	original := []byte("old binary content")
	if err := os.WriteFile(exePath, original, 0o755); err != nil {
		t.Fatalf("seeding old executable: %v", err)
	}

	c := testClient(t, server, exePath)
	plan, err := c.Plan("0.1.0")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	err = c.Apply(plan)
	if err == nil {
		t.Fatal("expected checksum mismatch to be rejected")
	}

	got, readErr := os.ReadFile(exePath)
	if readErr != nil {
		t.Fatalf("reading executable after rejected apply: %v", readErr)
	}
	if string(got) != string(original) {
		t.Fatal("executable was modified despite a checksum mismatch")
	}
}

func TestApply_RefusesWhenUpToDate(t *testing.T) {
	server, _ := newFakeGitHub(t, "v0.1.0", []byte("same content"), false)
	c := testClient(t, server, "")

	plan, err := c.Plan("0.1.0")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if err := c.Apply(plan); err == nil {
		t.Fatal("expected Apply to refuse an already-up-to-date plan")
	}
}
