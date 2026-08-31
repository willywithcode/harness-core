package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeReleaseFixture is a running fake-GitHub server plus an "old" mustang
// binary (version 0.1.0-test) already `init`-ed against destDir, pointed at
// that server via env. The server's "latest release" is version
// 0.2.0-test, whose asset content is newBinBytes -- always a real,
// currently-built mustang binary, since selfupdate applies real bytes.
type fakeReleaseFixture struct {
	oldBinPath string
	newBinPath string
	newBinData []byte
	destDir    string
	env        []string
}

func setupFakeRelease(t *testing.T) fakeReleaseFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("builds two real binaries and runs them as subprocesses; skipped with -short")
	}

	moduleDir, err := os.Getwd() // this test file lives at the module root
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	work := t.TempDir()

	newBinPath := filepath.Join(work, binName("new"))
	buildMustang(t, moduleDir, newBinPath, "0.2.0-test")
	newBinBytes, err := os.ReadFile(newBinPath)
	if err != nil {
		t.Fatalf("reading built new binary: %v", err)
	}
	sum := sha256.Sum256(newBinBytes)
	newBinChecksum := hex.EncodeToString(sum[:])

	assetName := fmt.Sprintf("mustang-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		assetName += ".exe"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/assets/bin", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, newBinPath)
	})
	mux.HandleFunc("/assets/sum", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, newBinChecksum)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("/repos/"+selfUpdateRepo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v0.2.0-test",
			"assets": []map[string]string{
				{"name": assetName, "browser_download_url": server.URL + "/assets/bin"},
				{"name": assetName + ".sha256", "browser_download_url": server.URL + "/assets/sum"},
			},
		})
	})

	oldBinPath := filepath.Join(work, binName("old"))
	buildMustang(t, moduleDir, oldBinPath, "0.1.0-test")

	destDir := filepath.Join(work, "consumer-repo")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("mkdir destDir: %v", err)
	}
	runOK(t, oldBinPath, nil, "init", destDir, "--target=agent")

	return fakeReleaseFixture{
		oldBinPath: oldBinPath,
		newBinPath: newBinPath,
		newBinData: newBinBytes,
		destDir:    destDir,
		env:        append(os.Environ(), "MUSTANG_SELFUPDATE_API_BASE_URL="+server.URL),
	}
}

// TestUpdate_SelfUpdatesAndReExecs is an end-to-end integration test for the
// highest-blast-radius mechanism in this project: `update --apply`
// replacing its own running executable on disk and re-executing itself to
// pick up the new payload. It builds two real mustang binaries, serves the
// "new" one from a fake GitHub-shaped HTTP server, and runs the "old" one
// as a real subprocess -- the only way to actually prove self-replace +
// re-exec works, short of a real GitHub release.
func TestUpdate_SelfUpdatesAndReExecs(t *testing.T) {
	f := setupFakeRelease(t)

	updateOut := runOK(t, f.oldBinPath, f.env, "update", f.destDir, "--apply")

	if !strings.Contains(updateOut, "self-updating") {
		t.Fatalf("expected update --apply output to mention self-updating, got:\n%s", updateOut)
	}
	if !strings.Contains(updateOut, "re-running") {
		t.Fatalf("expected update --apply output to mention re-running, got:\n%s", updateOut)
	}

	// The defining assertion: the file at oldBinPath must now literally BE
	// the new binary's bytes. self-update replaces the executable in
	// place; the path does not change, only its content.
	replaced, err := os.ReadFile(f.oldBinPath)
	if err != nil {
		t.Fatalf("reading %s after update --apply: %v", f.oldBinPath, err)
	}
	if string(replaced) != string(f.newBinData) {
		t.Fatal("the binary on disk was not replaced with the fake release's content")
	}

	// Confirm the re-exec actually ran the payload update to completion
	// under the new binary, not just that self-update happened: status
	// (via the now-replaced binary) must report the new core version.
	statusOut := runOK(t, f.oldBinPath, nil, "status", f.destDir)
	if !strings.Contains(statusOut, "0.2.0-test") {
		t.Fatalf("expected status to report core version 0.2.0-test after the re-exec'd update, got:\n%s", statusOut)
	}

	// Re-running update --apply now must NOT loop or attempt another
	// self-update: the binary already reports 0.2.0-test, matching the
	// fake release, so selfUpdateBinary's Plan().UpToDate should be true.
	secondOut := runOK(t, f.oldBinPath, f.env, "update", f.destDir, "--apply")
	if strings.Contains(secondOut, "self-updating") {
		t.Fatalf("expected no further self-update once already at the latest version, got:\n%s", secondOut)
	}
}

// TestUpdate_NoSelfUpdateFlag proves --no-self-update actually suppresses
// the automatic self-update: the on-disk binary must stay byte-for-byte the
// original, even though a newer release is available and --apply is set.
func TestUpdate_NoSelfUpdateFlag(t *testing.T) {
	f := setupFakeRelease(t)

	before, err := os.ReadFile(f.oldBinPath)
	if err != nil {
		t.Fatalf("reading original binary: %v", err)
	}

	out := runOK(t, f.oldBinPath, f.env, "update", f.destDir, "--apply", "--no-self-update")
	if strings.Contains(out, "self-updating") {
		t.Fatalf("--no-self-update should suppress the self-update, got:\n%s", out)
	}

	after, err := os.ReadFile(f.oldBinPath)
	if err != nil {
		t.Fatalf("reading binary after --no-self-update run: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("--no-self-update was set, but the binary on disk changed anyway")
	}
}

// TestUpdate_DryRunDoesNotSelfUpdate proves that without --apply, `update`
// never mutates the running binary -- a dry-run preview must stay a dry
// run even when a newer release exists.
func TestUpdate_DryRunDoesNotSelfUpdate(t *testing.T) {
	f := setupFakeRelease(t)

	before, err := os.ReadFile(f.oldBinPath)
	if err != nil {
		t.Fatalf("reading original binary: %v", err)
	}

	// No --apply.
	runOK(t, f.oldBinPath, f.env, "update", f.destDir)

	after, err := os.ReadFile(f.oldBinPath)
	if err != nil {
		t.Fatalf("reading binary after dry-run update: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("a dry-run `update` (no --apply) mutated the binary on disk")
	}
}

func binName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

func buildMustang(t *testing.T, moduleDir, outPath, version string) {
	t.Helper()
	cmd := exec.Command("go", "build",
		"-ldflags", "-X main.coreVersion="+version,
		"-o", outPath, ".")
	cmd.Dir = moduleDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("building %s (version %s): %v\n%s", outPath, version, err, out)
	}
}

func runOK(t *testing.T, binPath string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running %s %v: %v\n%s", binPath, args, err, out)
	}
	return string(out)
}
