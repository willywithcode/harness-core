// Package install copies the embedded Harness payload into a consumer
// repository. This is the stage-1 "copy-on-install" behavior: no provenance
// tracking and no merge logic yet. An existing file is left untouched unless
// override is set.
package install

import (
	"io/fs"
	"os"
	"path/filepath"
)

// Result reports what Init actually did, so the caller can print or verify it.
type Result struct {
	Written []string
	Skipped []string
}

// executable lists payload paths that must be written with the execute bit
// set. go:embed does not preserve source file permissions, so this cannot be
// read from the embedded FS itself and must be kept in sync with the actual
// git file mode (100755) of the payload manifest by hand.
var executable = map[string]bool{
	".agents/skills/onboard-repository/scripts/emit_evidence_bundle.py": true,
	".agents/skills/onboard-repository/scripts/render_patch.py":         true,
}

// Init walks payloadFS and writes every file under destDir, preserving the
// relative path. It never deletes or modifies a file outside destDir.
func Init(payloadFS fs.FS, destDir string, override bool) (Result, error) {
	var result Result

	err := fs.WalkDir(payloadFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		destPath := filepath.Join(destDir, filepath.FromSlash(path))

		if !override {
			if _, statErr := os.Stat(destPath); statErr == nil {
				result.Skipped = append(result.Skipped, path)
				return nil
			}
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return err
		}

		content, err := fs.ReadFile(payloadFS, path)
		if err != nil {
			return err
		}

		mode := os.FileMode(0o644)
		if executable[path] {
			mode = 0o755
		}

		if err := os.WriteFile(destPath, content, mode); err != nil {
			return err
		}

		result.Written = append(result.Written, path)
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	return result, nil
}
