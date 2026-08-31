// Package target materializes the exact file set to install for a chosen
// agent runtime, from the single canonical payload embedded in the binary.
// Only .agents/skills/ is checked into the repository: it's the
// vendor-neutral Agent Skills format other agent runtimes can read
// directly. Claude Code's project skill discovery, however, scans exactly
// .claude/skills/<name>/SKILL.md and has no knowledge of .agents/ at all —
// so a "claude" (or "both") install needs a real .claude/skills/ tree on
// disk, generated at install time rather than duplicated in git.
package target

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"testing/fstest"
)

// Default is used when the caller does not specify --target.
const Default = "both"

const (
	agentsSkillsPrefix = ".agents/skills/"
	claudeSkillsPrefix = ".claude/skills/"
	skillFileName      = "SKILL.md"
)

var valid = map[string]bool{"agent": true, "claude": true, "both": true}

// Valid reports whether name is a recognized target.
func Valid(name string) bool {
	return valid[name]
}

// executable lists the original .agents/skills/... paths that must be
// written with the execute bit set, whichever target(s) they end up in.
// go:embed does not preserve source file permissions, so this cannot be
// read from the embedded FS and must be kept in sync with the actual git
// file mode (100755) of the payload by hand.
var executable = map[string]bool{
	".agents/skills/onboard-repository/scripts/emit_evidence_bundle.py": true,
	".agents/skills/onboard-repository/scripts/render_patch.py":         true,
}

// Build returns an in-memory fs.FS holding exactly the files that should be
// installed for the given target, derived from rawPayload (the raw
// embedded payload: AGENTS.md, docs/**, and .agents/skills/**).
//
//   - "agent": AGENTS.md, docs/**, and .agents/skills/** verbatim.
//   - "claude": AGENTS.md, docs/**, and a self-contained .claude/skills/**
//     tree. Every file under each skill is copied; in each skill's
//     SKILL.md specifically, any text occurrence of ".agents/skills/" is
//     rewritten to ".claude/skills/" so the skill's own references to its
//     scripts and references still resolve even though .agents/ is never
//     installed in this mode.
//   - "both": AGENTS.md, docs/**, and both trees above, each
//     self-contained.
//
// Build only reads rawPayload; it never touches disk.
func Build(rawPayload fs.FS, name string) (fs.FS, error) {
	if !Valid(name) {
		return nil, fmt.Errorf("unknown target %q: must be agent, claude, or both", name)
	}

	out := fstest.MapFS{}

	err := fs.WalkDir(rawPayload, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		content, err := fs.ReadFile(rawPayload, p)
		if err != nil {
			return err
		}

		mode := fs.FileMode(0o644)
		if executable[p] {
			mode = 0o755
		}

		if !strings.HasPrefix(p, agentsSkillsPrefix) {
			// AGENTS.md, docs/** -- always included, unmodified,
			// regardless of target.
			out[p] = &fstest.MapFile{Data: content, Mode: mode}
			return nil
		}

		if name == "agent" || name == "both" {
			out[p] = &fstest.MapFile{Data: content, Mode: mode}
		}
		if name == "claude" || name == "both" {
			claudePath := claudeSkillsPrefix + strings.TrimPrefix(p, agentsSkillsPrefix)
			claudeContent := content
			if path.Base(p) == skillFileName {
				claudeContent = bytes.ReplaceAll(content, []byte(agentsSkillsPrefix), []byte(claudeSkillsPrefix))
			}
			out[claudePath] = &fstest.MapFile{Data: claudeContent, Mode: mode}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return out, nil
}
