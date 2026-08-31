// Package provenance records which core version installed which payload
// files, and their content hashes, so a later command can tell an unmodified
// managed file apart from one the consumer edited by hand.
package provenance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// RelPath is where provenance lives inside an installed repository.
const RelPath = ".harness-core/provenance.json"

// Provenance is the durable record written after a successful install.
type Provenance struct {
	CoreVersion string            `json:"core_version"`
	InstalledAt time.Time         `json:"installed_at"`
	Files       map[string]string `json:"files"` // relative path -> "sha256:<hex>"
}

// HashAll hashes every file in payloadFS, keyed by its slash-separated
// relative path. update.BuildPlan calls this to get the "UPSTREAM" side of
// its three-way comparison without writing anything.
func HashAll(payloadFS fs.FS) (map[string]string, error) {
	hashes := map[string]string{}

	err := fs.WalkDir(payloadFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		content, err := fs.ReadFile(payloadFS, path)
		if err != nil {
			return err
		}
		hashes[path] = hashHex(content)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return hashes, nil
}

// Compute hashes every file in payloadFS and returns a fresh Provenance for
// the given core version. Call this against the exact payload that was just
// written to disk, so the recorded hashes match what Init actually wrote.
func Compute(payloadFS fs.FS, coreVersion string) (Provenance, error) {
	files, err := HashAll(payloadFS)
	if err != nil {
		return Provenance{}, err
	}

	return Provenance{
		CoreVersion: coreVersion,
		InstalledAt: time.Now().UTC(),
		Files:       files,
	}, nil
}

// Save writes p to destDir/.harness-core/provenance.json.
func Save(destDir string, p Provenance) error {
	path := filepath.Join(destDir, filepath.FromSlash(RelPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

// Load reads provenance from destDir/.harness-core/provenance.json. It
// returns an *os.PathError satisfying os.IsNotExist when the repository has
// no recorded provenance yet.
func Load(destDir string) (Provenance, error) {
	path := filepath.Join(destDir, filepath.FromSlash(RelPath))
	data, err := os.ReadFile(path)
	if err != nil {
		return Provenance{}, err
	}
	var p Provenance
	if err := json.Unmarshal(data, &p); err != nil {
		return Provenance{}, err
	}
	return p, nil
}

// HashFile returns the same "sha256:<hex>" form as Compute, for a file that
// already exists on disk. Callers use it to compare a tracked path's current
// on-disk content against the hash recorded in Provenance.
func HashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return hashHex(data), nil
}

func hashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
