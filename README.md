# harness-core

`harness-core` installs and maintains a small set of agent-facing guidance
files (`AGENTS.md`, `docs/`, `.agents/skills/`, plus Claude Code discovery
pointers under `.claude/skills/`) in any repository, as a single
self-updating binary with no runtime dependencies.

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
`~/.local/bin/harness-core` (or `%LOCALAPPDATA%\harness-core\harness-core.exe`
on Windows). Set `HARNESS_CORE_INSTALL_DIR` to install elsewhere. Make sure
the install directory is on your `PATH`.

You can also download a binary directly from the
[latest release](https://github.com/willywithcode/harness-core/releases/latest)
if you'd rather skip the script.

## Usage

### `harness-core init [dest] [--override]`

Copies the embedded payload into `dest` (default: current directory) and
writes `dest/.harness-core/provenance.json`, recording the installed core
version and a SHA-256 hash per file. An existing file is left untouched
unless `--override` is passed.

```bash
harness-core init .
harness-core init ../some-other-repo --override
```

### `harness-core status [dest]`

Compares the files on disk against the recorded provenance and reports which
tracked files are unchanged, locally modified, or missing.

```bash
$ harness-core status
core version: 0.1.0
installed at: 2026-08-31T09:18:46Z

modified: docs/patterns/encoding-invariants.md

25 unchanged, 1 modified, 0 missing (of 26 tracked).
```

### `harness-core update [dest] [--apply]`

Compares three versions of every payload file:

- **BASE** — what provenance recorded at install time;
- **LOCAL** — what's actually on disk now;
- **UPSTREAM** — the payload embedded in the binary you're currently running.

A file you never touched is safely overwritten when upstream changed it. A
file upstream never changed is left alone even if you edited it. A file
**both** you and upstream changed is a **conflict** — `update` never guesses
how to merge it. Run without `--apply` first to preview:

```bash
$ harness-core update
update: AGENTS.md
CONFLICT: docs/WORKFLOW.md (modified locally and changed upstream)

23 up to date, 1 kept as local edits, 0 already match upstream, 0 locally deleted (upstream unchanged).

1 conflict(s) found. Resolve them by hand (edit the local file to keep,
accept, or merge the upstream change), then rerun `harness-core update`.
```

If there is any unresolved conflict, `--apply` refuses to write **anything**
— not even the non-conflicting files — until you resolve it by hand and
rerun. Once clean, `--apply` writes the changes and backs up every
overwritten file's previous content to
`dest/.harness-core/backup/<timestamp>/` first, so an unwanted upstream
change can be recovered by hand.

```bash
harness-core update --apply
```

### `harness-core self-update [--check]`

Checks the latest GitHub release of this project, and — if newer — downloads
the matching platform binary, verifies its SHA-256 checksum, and replaces
the running executable in place. `--check` only reports whether an update is
available without downloading or writing anything.

```bash
harness-core self-update --check
harness-core self-update
```

## How it works

- **Payload embedding**: `AGENTS.md`, `docs/`, `.agents/`, and `.claude/` are
  embedded into the binary at compile time via Go's `//go:embed`. There is
  no network call, config file, or external dependency involved in `init` —
  the payload ships inside the executable itself.
- **Claude Code skill discovery**: the canonical skill definitions live
  under `.agents/skills/<name>/SKILL.md` (a vendor-neutral format other
  agent runtimes can read too), but Claude Code's project skill discovery
  scans exactly `.claude/skills/<name>/SKILL.md` and has no knowledge of
  `.agents/`. `init` also installs a thin pointer file at
  `.claude/skills/<name>/SKILL.md` for each skill — same frontmatter, so
  Claude Code's auto-invocation matches on the same description, with a
  one-line body pointing at the real file under `.agents/skills/`.
- **Provenance**: every `init` and `update --apply` writes
  `.harness-core/provenance.json`: the installed core version, an install
  timestamp, and a SHA-256 hash per tracked file. This is the only state
  `update`'s three-way comparison needs.
- **Copy-on-conflict, never merge**: `update` will never attempt to combine
  two versions of a file. A real conflict always stops and asks a human to
  resolve it — matching the upstream Harness project's own position that
  reconciling conflicting intent is a decision, not something a tool should
  guess at.
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
go test ./...           # unit tests (selfupdate package)
go run . init /tmp/test # try it locally without installing
```

### Project layout

```
main.go                       CLI entry point and subcommand dispatch
internal/install/             writes payload files (used by init and update)
internal/provenance/          hashing, provenance.json read/write
internal/update/              three-way BASE/LOCAL/UPSTREAM plan + apply
internal/selfupdate/          GitHub release resolution, checksum verify, binary replace
AGENTS.md, docs/, .agents/,
.claude/                      the payload itself (also what init installs)
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

`AGENTS.md`, `docs/`, `.agents/skills/`, and `.claude/skills/` at the
repository root are both this project's own working payload *and* the exact
content `init` embeds and ships. Edit them like any other tracked files —
if you add or rename a skill under `.agents/skills/`, add its matching
pointer under `.claude/skills/` too — then cut a release — every installed
`harness-core` binary out there will pick up the change via
`harness-core update`.
