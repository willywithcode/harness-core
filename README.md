# mustang

Hosted at [github.com/willywithcode/harness-core](https://github.com/willywithcode/harness-core) — the repository kept its original name, but the CLI, binary, and everything it installs are `mustang`.

`mustang` installs and maintains a small set of agent-facing guidance
files (`AGENTS.md`, `docs/`, and a set of skills) in any repository, as a
single self-updating binary with no runtime dependencies.

It never merges. A file you never touched gets updated safely; a file you
edited is left alone — unless upstream changed it too, in which case it
stops and asks you to decide. Reconciling conflicting intent is a human
call, not something this tool will guess at.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/willywithcode/harness-core/main/scripts/install.sh | bash
```

```powershell
irm https://raw.githubusercontent.com/willywithcode/harness-core/main/scripts/install.ps1 | iex
```

Both scripts detect your platform, download the matching release binary and
its `.sha256` checksum, verify the bytes, and install to
`~/.local/bin/mustang` (or `%LOCALAPPDATA%\mustang\mustang.exe`
on Windows). Set `MUSTANG_INSTALL_DIR` to install elsewhere. Make sure
the install directory is on your `PATH`.

You can also download a binary directly from the
[latest release](https://github.com/willywithcode/harness-core/releases/latest)
if you'd rather skip the script.

## Usage

### `mustang init [dest] [--override] [--target=agent|claude|both]`

Copies the payload into `dest` (default: current directory) and writes
`dest/.mustang/provenance.json`, recording the installed core version,
target, and a SHA-256 hash per file. An existing file is left untouched
unless `--override` is passed.

`--target` picks which skill discovery format(s) to install (default `both`):

- `agent` — `.agents/skills/<name>/SKILL.md`, the vendor-neutral
  [Agent Skills](https://agentskills.io) format other agent runtimes can
  read directly.
- `claude` — a self-contained `.claude/skills/<name>/SKILL.md` tree, the
  exact path Claude Code's project skill discovery scans. Every file a
  skill needs (scripts, references) is copied alongside it, with internal
  references rewritten so the skill works with no `.agents/` directory
  present at all. Also installs a `CLAUDE.md` that imports `AGENTS.md` —
  Claude Code never reads `AGENTS.md` on its own, so without this it would
  sit in the repo unread. If you already have a `CLAUDE.md`, it's left
  untouched (same skip-if-exists rule as every other file); add `@AGENTS.md`
  to it yourself in that case.
- `both` — both skill trees, each self-contained, plus the same `CLAUDE.md`.

```bash
mustang init .
mustang init ../some-other-repo --override
mustang init ../claude-only-repo --target=claude
```

### `mustang status [dest]`

Compares the files on disk against the recorded provenance and reports which
tracked files are unchanged, locally modified, or missing.

```bash
$ mustang status
core version: 0.1.0
target: both
installed at: 2026-08-31T09:18:46Z

modified: docs/patterns/encoding-invariants.md

38 unchanged, 1 modified, 0 missing (of 39 tracked).
```

### `mustang update [dest] [--apply] [--target=agent|claude|both] [--accept-upstream=<path>]... [--keep-local=<path>]...`

Compares three versions of every payload file:

- **BASE** — what provenance recorded at install time;
- **LOCAL** — what's actually on disk now;
- **UPSTREAM** — the payload embedded in the binary you're currently running,
  built for the effective target: the target from provenance, or `--target`
  if you pass one to switch (e.g. `mustang update --target=both --apply`
  adds `.claude/skills/` to an existing `agent`-only install; switching away
  from a target reports its files as "upstream removed" and leaves them on
  disk, untracked, rather than deleting them).

A file you never touched is safely overwritten when upstream changed it. A
file upstream never changed is left alone even if you edited it. A file
**both** you and upstream changed is a **conflict** — `update` never guesses
how to merge it. Run without `--apply` first to preview:

```bash
$ mustang update
update: AGENTS.md
CONFLICT: docs/WORKFLOW.md (modified locally and changed upstream)

23 up to date, 1 kept as local edits, 0 already match upstream, 0 locally deleted (upstream unchanged).

1 conflict(s) found. Resolve them with --accept-upstream=<path> (take the new
version, backing up the old one first), --keep-local=<path> (keep your edit
and stop asking), or by hand (edit the local file to match either side), then
rerun `mustang update`.
```

If there is any unresolved conflict, `--apply` refuses to write **anything**
— not even the non-conflicting files. Resolve each one explicitly, per path:

- `--accept-upstream=<path>` — take the new upstream content. The
  overwritten local content is backed up to
  `dest/.mustang/backup/<timestamp>/` first, same as any other
  overwrite.
- `--keep-local=<path>` — keep your edit exactly as-is; nothing is written.
  This is permanent, not a one-time skip: `update` won't ask about this
  path again unless upstream changes it once more.

Both flags can be repeated for multiple paths. A path that doesn't match a
current conflict prints a warning and is otherwise ignored — it never
silently no-ops your typo.

```bash
mustang update --apply
mustang update --accept-upstream=docs/WORKFLOW.md --apply
mustang update --keep-local=CLAUDE.md --apply
```

### `mustang self-update [--check]`

Checks the latest GitHub release of this project, and — if newer — downloads
the matching platform binary, verifies its SHA-256 checksum, and replaces
the running executable in place. `--check` only reports whether an update is
available without downloading or writing anything.

```bash
mustang self-update --check
mustang self-update
```

### Help

`mustang --help` (or `-h`, or `help`) prints the full command list.
`mustang <command> --help` prints that one command's full details — e.g.
`mustang update --help`.

## How it works

- **Payload embedding**: `AGENTS.md`, `docs/`, and `.agents/skills/` — the
  single canonical payload — are embedded into the binary at compile time
  via Go's `//go:embed`. There is no network call, config file, or external
  dependency involved in `init` — the payload ships inside the executable
  itself.
- **Generated, not duplicated, Claude Code discovery**: only
  `.agents/skills/<name>/SKILL.md` (the vendor-neutral
  [Agent Skills](https://agentskills.io) format) is checked into this repo.
  Claude Code's project skill discovery, however, scans exactly
  `.claude/skills/<name>/SKILL.md` and has no knowledge of `.agents/` at
  all. Rather than hand-maintaining a second copy of every skill,
  `internal/target.Build` derives a full, self-contained `.claude/skills/`
  tree at install time — copying every file a skill needs (scripts,
  references) and rewriting the skill's own internal `.agents/skills/`
  path references to `.claude/skills/` — whenever `--target=claude` or
  `--target=both` is requested. `--target` decides which tree(s) actually
  land on disk; nothing about this lives as static files in git.
- **Making AGENTS.md actually take effect for Claude Code**: Claude Code
  only auto-loads `CLAUDE.md`/`CLAUDE.local.md` at session start — it never
  reads `AGENTS.md` on its own. The `claude`/`both` targets also generate a
  one-line `CLAUDE.md` (`@AGENTS.md`) so the payload isn't silently inert
  for Claude Code users; it's skipped, like any other file, if one already
  exists.
- **Provenance**: every `init` and `update --apply` writes
  `.mustang/provenance.json`: the installed core version, the target
  used, an install timestamp, and a SHA-256 hash per tracked file. This is
  the only state `update`'s three-way comparison needs.
- **Copy-on-conflict, never merge**: `update` will never attempt to combine
  two versions of a file. A real conflict always stops and asks a human to
  resolve it — reconciling conflicting intent is a decision, not something
  a tool should guess at.
- **Atomic writes**: every file write goes through a temp file in the same
  directory, then an atomic rename. A process killed mid-write can never
  leave a payload file holding truncated or corrupted content.
- **Self-update integrity**: `self-update` requires a `.sha256` sidecar
  published alongside every release binary and refuses to touch the running
  executable unless the downloaded bytes match it exactly.

## Development

Requires Go 1.22+.

```bash
go build ./...        # build
go vet ./...           # static checks
go test ./...           # unit tests (target and selfupdate packages)
go run . init /tmp/test # try it locally without installing
```

### Project layout

```
main.go                       CLI entry point and subcommand dispatch
internal/target/              derives the agent/claude/both file set from the raw payload
internal/install/             writes files (used by init and update)
internal/provenance/          hashing, provenance.json read/write
internal/update/              three-way BASE/LOCAL/UPSTREAM plan + apply
internal/selfupdate/          GitHub release resolution, checksum verify, binary replace
AGENTS.md, docs/, .agents/    the canonical payload (also what init installs)
scripts/install.sh, .ps1      bootstrap installers
.github/workflows/release.yml cross-platform release build
```

### Cutting a release

Push a tag matching `v*`; the release workflow builds `darwin`/`linux`
(`amd64`+`arm64`) and `windows/amd64`, stamps the exact tag into the binary's
reported version via `-ldflags -X main.coreVersion=<tag>`, computes each
binary's SHA-256, and publishes all of them as one GitHub release.

```bash
git tag v0.2.0
git push origin v0.2.0
```

## Updating the payload itself

`AGENTS.md`, `docs/`, and `.agents/skills/` at the repository root are both
this project's own working payload *and* the exact content `init` embeds
and ships. Edit them like any other tracked files, then cut a release —
every installed `mustang` binary out there will pick up the change via
`mustang update`. There is nothing to keep in sync by hand for Claude
Code: `.claude/skills/` is always derived from `.agents/skills/` at install
time (see `internal/target`), so adding, renaming, or editing a skill only
ever means touching its one file under `.agents/skills/`.
