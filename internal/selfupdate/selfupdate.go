// Package selfupdate lets the harness-core binary replace itself with a
// newer release published on GitHub. It never trusts GitHub's plain
// "latest" convenience alone: it resolves the release, requires a matching
// per-platform binary asset and a ".sha256" checksum sidecar published
// alongside it, and verifies the downloaded bytes against that checksum
// before ever touching the installed executable.
package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Client talks to one GitHub repository's releases. APIBaseURL is
// overridable so tests can point it at a local httptest.Server instead of
// the real github.com. ExecutablePath is overridable so tests can verify
// the full Apply path against a scratch file instead of the real running
// test binary.
type Client struct {
	HTTPClient     *http.Client
	APIBaseURL     string
	Repo           string // "owner/repo"
	ExecutablePath func() (string, error)
}

// NewClient returns a Client configured for the real GitHub API and the
// real running executable.
func NewClient(repo string) *Client {
	return &Client{
		HTTPClient:     http.DefaultClient,
		APIBaseURL:     "https://api.github.com",
		Repo:           repo,
		ExecutablePath: defaultExecutablePath,
	}
}

func defaultExecutablePath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locating running executable: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return "", fmt.Errorf("resolving executable path: %w", err)
	}
	return exePath, nil
}

type release struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Plan is the resolved outcome of checking for an update: what asset would
// be downloaded, from where, and whether it differs from the running
// version. Building a Plan never writes anything.
type Plan struct {
	CurrentVersion string
	LatestVersion  string
	AssetName      string
	BinaryURL      string
	ChecksumURL    string
	UpToDate       bool
}

// AssetName returns the release asset name expected for the given
// GOOS/GOARCH, matching exactly what the release workflow publishes.
func AssetName(goos, goarch string) string {
	name := fmt.Sprintf("harness-core-%s-%s", goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// Plan resolves the latest release and decides what would change relative
// to currentVersion, without downloading or writing anything.
func (c *Client) Plan(currentVersion string) (Plan, error) {
	rel, err := c.latestRelease()
	if err != nil {
		return Plan{}, err
	}

	assetName := AssetName(runtime.GOOS, runtime.GOARCH)
	binAsset, ok := findAsset(rel.Assets, assetName)
	if !ok {
		return Plan{}, fmt.Errorf("release %s has no asset named %q for %s/%s",
			rel.TagName, assetName, runtime.GOOS, runtime.GOARCH)
	}
	sumAsset, ok := findAsset(rel.Assets, assetName+".sha256")
	if !ok {
		return Plan{}, fmt.Errorf("release %s has no checksum asset %q", rel.TagName, assetName+".sha256")
	}

	latestVersion := strings.TrimPrefix(rel.TagName, "v")

	return Plan{
		CurrentVersion: currentVersion,
		LatestVersion:  latestVersion,
		AssetName:      assetName,
		BinaryURL:      binAsset.BrowserDownloadURL,
		ChecksumURL:    sumAsset.BrowserDownloadURL,
		UpToDate:       latestVersion == currentVersion,
	}, nil
}

// Apply downloads plan's binary and checksum, verifies the binary's SHA-256
// against the checksum, and atomically replaces the currently running
// executable. It refuses to touch anything if the checksum does not match,
// or if plan reports the binary is already up to date.
func (c *Client) Apply(plan Plan) error {
	if plan.UpToDate {
		return fmt.Errorf("already up to date at version %s", plan.CurrentVersion)
	}

	binBytes, err := c.download(plan.BinaryURL)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", plan.AssetName, err)
	}
	sumBytes, err := c.download(plan.ChecksumURL)
	if err != nil {
		return fmt.Errorf("downloading %s.sha256: %w", plan.AssetName, err)
	}

	wantFields := strings.Fields(string(sumBytes))
	if len(wantFields) == 0 {
		return fmt.Errorf("empty checksum file for %s", plan.AssetName)
	}
	want := strings.ToLower(wantFields[0])

	sum := sha256.Sum256(binBytes)
	got := hex.EncodeToString(sum[:])
	if got != want {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", plan.AssetName, got, want)
	}

	exePath, err := c.ExecutablePath()
	if err != nil {
		return err
	}

	return replaceExecutable(exePath, binBytes)
}

// replaceExecutable writes content to a temp file in exePath's directory,
// makes it executable, and renames it over exePath. Same-directory rename
// is atomic on POSIX filesystems and stays on the same filesystem/volume,
// which a cross-directory move is not guaranteed to do.
func replaceExecutable(exePath string, content []byte) (err error) {
	dir := filepath.Dir(exePath)

	tmp, err := os.CreateTemp(dir, ".harness-core-selfupdate-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			os.Remove(tmpPath)
		}
	}()

	if _, err = tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmpPath, exePath)
}

func (c *Client) latestRelease() (release, error) {
	url := c.APIBaseURL + "/repos/" + c.Repo + "/releases/latest"

	resp, err := c.HTTPClient.Get(url)
	if err != nil {
		return release{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return release{}, fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return release{}, fmt.Errorf("decoding release response: %w", err)
	}
	return rel, nil
}

func (c *Client) download(url string) ([]byte, error) {
	resp, err := c.HTTPClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}

	return io.ReadAll(resp.Body)
}

func findAsset(assets []asset, name string) (asset, bool) {
	for _, a := range assets {
		if a.Name == name {
			return a, true
		}
	}
	return asset{}, false
}
