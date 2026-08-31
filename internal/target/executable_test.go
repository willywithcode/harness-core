package target

import (
	"os/exec"
	"strings"
	"testing"
)

// TestExecutableTableMatchesGit guards against exactly the class of bug this
// project has already shipped twice: go:embed silently drops the real
// executable bit, so `executable` is a hand-maintained table that must be
// kept in sync by a human every time a skill adds or removes a script. This
// test makes that drift a test failure instead of a silent runtime bug, by
// comparing the table against git's actual recorded file mode for every
// path under .agents/skills/.
func TestExecutableTableMatchesGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	rootOut, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skip("not inside a git checkout; skipping (this test only makes sense against the source repo)")
	}
	repoRoot := strings.TrimSpace(string(rootOut))

	out, err := exec.Command("git", "-C", repoRoot, "ls-files", "-s", ".agents/skills").Output()
	if err != nil {
		t.Fatalf("git ls-files -s .agents/skills: %v", err)
	}

	gitExecutable := map[string]bool{}
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		// Format: "<mode> <blob-sha> <stage>\t<path>"
		fields := strings.Fields(line)
		if len(fields) < 4 {
			t.Fatalf("unexpected `git ls-files -s` line: %q", line)
		}
		mode := fields[0]
		path := fields[len(fields)-1]
		gitExecutable[path] = mode == "100755"
	}

	for path := range executable {
		if !gitExecutable[path] {
			t.Errorf("target.executable[%q] = true, but git does not record it as mode 100755 "+
				"(stale entry — remove it, or the file's git mode changed)", path)
		}
	}
	for path, isExec := range gitExecutable {
		if isExec && !executable[path] {
			t.Errorf("git records %q as mode 100755, but it is missing from target.executable "+
				"(a new executable script was added without updating the table)", path)
		}
	}
}
