// Package install copies the embedded Harness payload into a consumer
// repository. This is the stage-1 "copy-on-install" behavior: no merge
// logic here. An existing file is left untouched unless override is set.
// WriteFile is also reused by the update package to apply individual
// files once a three-way plan says it is safe to do so.
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

// Executable lists payload paths that must be written with the execute bit
// set. go:embed does not preserve source file permissions, so this cannot be
// read from the embedded FS itself and must be kept in sync with the actual
// git file mode (100755) of the payload manifest by hand.
var Executable = map[string]bool{
	".agents/skills/onboard-repository/scripts/emit_evidence_bundle.py": true,
	".agents/skills/onboard-repository/scripts/render_patch.py":         true,
}

// WriteFile copies one payload file to destDir/path, creating parent
// directories as needed and preserving the execute bit for paths listed in
// Executable.
func WriteFile(payloadFS fs.FS, destDir, path string) error {
	destPath := filepath.Join(destDir, filepath.FromSlash(path))

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}

	content, err := fs.ReadFile(payloadFS, path)
	if err != nil {
		return err
	}

	mode := os.FileMode(0o644)
	if Executable[path] {
		mode = 0o755
	}

	return writeAtomic(destPath, content, mode)
}

// writeAtomic writes content to a temporary file in the same directory as
// path, then renames it into place. Rename is atomic on POSIX filesystems,
// so a process killed mid-write can never leave path holding truncated or
// corrupted content: a reader always sees either the complete old content or
// the complete new content, never something in between. Plain os.WriteFile
// does not have this property.
func writeAtomic(path string, content []byte, mode os.FileMode) (err error) {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".harness-core-write-*")
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
	if err = tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmpPath, path)
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

		if err := WriteFile(payloadFS, destDir, path); err != nil {
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
