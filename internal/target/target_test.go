package target

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

// fakeRawPayload builds a synthetic raw payload with the same shape as the
// real embedded one (AGENTS.md, docs/, .agents/skills/<name>/...), so these
// tests exercise the Build mechanism itself without depending on the real
// skill content, which can change independently.
func fakeRawPayload() fstest.MapFS {
	return fstest.MapFS{
		"AGENTS.md":        &fstest.MapFile{Data: []byte("agents content")},
		"docs/WORKFLOW.md": &fstest.MapFile{Data: []byte("workflow content")},
		".agents/skills/demo-skill/SKILL.md": &fstest.MapFile{Data: []byte(
			"---\nname: demo-skill\n---\n\nRun `python3 .agents/skills/demo-skill/scripts/run.py` for details.\n",
		)},
		".agents/skills/demo-skill/scripts/run.py": &fstest.MapFile{
			Data: []byte("print('hi')"),
			Mode: 0o755,
		},
	}
}

func init() {
	// This test's synthetic script path must match a real executable
	// listed in the package's static table for the mode-propagation
	// assertions below to be meaningful; since the real table only
	// knows real payload paths, register this test's own path here.
	executable[".agents/skills/demo-skill/scripts/run.py"] = true
}

func TestBuild_InvalidTarget(t *testing.T) {
	if _, err := Build(fakeRawPayload(), "bogus"); err == nil {
		t.Fatal("expected an error for an unrecognized target")
	}
}

func TestBuild_Agent(t *testing.T) {
	out, err := Build(fakeRawPayload(), "agent")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	mustExist(t, out, "AGENTS.md")
	mustExist(t, out, "docs/WORKFLOW.md")
	mustExist(t, out, ".agents/skills/demo-skill/SKILL.md")
	mustExist(t, out, ".agents/skills/demo-skill/scripts/run.py")
	mustNotExist(t, out, ".claude/skills/demo-skill/SKILL.md")
	mustNotExist(t, out, "CLAUDE.md")

	content := mustReadFile(t, out, ".agents/skills/demo-skill/SKILL.md")
	if !strings.Contains(content, ".agents/skills/demo-skill/scripts/run.py") {
		t.Fatal("agent target must not rewrite .agents/skills/ references")
	}

	assertExecutable(t, out, ".agents/skills/demo-skill/scripts/run.py")
}

func TestBuild_Claude(t *testing.T) {
	out, err := Build(fakeRawPayload(), "claude")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	mustExist(t, out, "AGENTS.md")
	mustExist(t, out, "docs/WORKFLOW.md")
	mustExist(t, out, ".claude/skills/demo-skill/SKILL.md")
	mustExist(t, out, ".claude/skills/demo-skill/scripts/run.py")
	mustNotExist(t, out, ".agents/skills/demo-skill/SKILL.md")
	mustNotExist(t, out, ".agents/skills/demo-skill/scripts/run.py")

	content := mustReadFile(t, out, ".claude/skills/demo-skill/SKILL.md")
	if strings.Contains(content, ".agents/skills/") {
		t.Fatalf("claude target must rewrite every .agents/skills/ reference, got: %s", content)
	}
	if !strings.Contains(content, ".claude/skills/demo-skill/scripts/run.py") {
		t.Fatalf("claude target must rewrite the script reference to .claude/skills/, got: %s", content)
	}

	// The non-SKILL.md file must be copied byte-for-byte, unmodified.
	script := mustReadFile(t, out, ".claude/skills/demo-skill/scripts/run.py")
	if script != "print('hi')" {
		t.Fatalf("script content = %q, want unmodified", script)
	}

	assertExecutable(t, out, ".claude/skills/demo-skill/scripts/run.py")

	claudeMDContent := mustReadFile(t, out, "CLAUDE.md")
	if !strings.Contains(claudeMDContent, "@AGENTS.md") {
		t.Fatalf("CLAUDE.md must import AGENTS.md, got: %s", claudeMDContent)
	}
}

func TestBuild_Both(t *testing.T) {
	out, err := Build(fakeRawPayload(), "both")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	mustExist(t, out, ".agents/skills/demo-skill/SKILL.md")
	mustExist(t, out, ".claude/skills/demo-skill/SKILL.md")
	mustExist(t, out, "CLAUDE.md")

	agentContent := mustReadFile(t, out, ".agents/skills/demo-skill/SKILL.md")
	if !strings.Contains(agentContent, ".agents/skills/demo-skill/scripts/run.py") {
		t.Fatal("both target's .agents copy must keep the original .agents/skills/ reference")
	}

	claudeContent := mustReadFile(t, out, ".claude/skills/demo-skill/SKILL.md")
	if strings.Contains(claudeContent, ".agents/skills/") {
		t.Fatal("both target's .claude copy must still rewrite .agents/skills/ references")
	}
}

func mustExist(t *testing.T, fsys fs.FS, path string) {
	t.Helper()
	if _, err := fs.Stat(fsys, path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func mustNotExist(t *testing.T, fsys fs.FS, path string) {
	t.Helper()
	if _, err := fs.Stat(fsys, path); err == nil {
		t.Fatalf("expected %s to NOT exist", path)
	}
}

func mustReadFile(t *testing.T, fsys fs.FS, path string) string {
	t.Helper()
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func assertExecutable(t *testing.T, fsys fs.FS, path string) {
	t.Helper()
	info, err := fs.Stat(fsys, path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("%s should be executable, mode = %v", path, info.Mode())
	}
}
