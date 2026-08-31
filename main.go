package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"harness-core/internal/install"
	"harness-core/internal/provenance"
	"harness-core/internal/selfupdate"
	"harness-core/internal/target"
	"harness-core/internal/update"
)

// coreVersion identifies the payload (and binary) this build ships. The
// release workflow overrides it via `-ldflags "-X main.coreVersion=<tag>"`
// so a released binary's reported version always matches its git tag
// exactly. A local `go build`/`go run` without that flag falls back to this
// placeholder, which intentionally never matches a real release tag.
var coreVersion = "0.0.0-dev"

// selfUpdateRepo is the GitHub "owner/repo" that hosts harness-core
// releases.
const selfUpdateRepo = "willywithcode/harness-core"

// Payload embeds the single canonical payload this CLI ships: AGENTS.md,
// docs/, and .agents/skills/ (the vendor-neutral Agent Skills format). The
// "all:" prefix is required so embed does not silently skip .agents (it
// begins with a dot, which embed excludes by default). There is no
// .claude/ here — internal/target.Build derives a Claude-Code-native
// .claude/skills/ tree from this same payload at install time; see that
// package for why it can't just be a static copy checked into git.
//
//go:embed AGENTS.md
//go:embed docs
//go:embed all:.agents
var payload embed.FS

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "init":
		runInit(os.Args[2:])
	case "status":
		runStatus(os.Args[2:])
	case "update":
		runUpdate(os.Args[2:])
	case "self-update":
		runSelfUpdate(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func runInit(args []string) {
	dest := "."
	override := false
	targetName := target.Default
	for _, a := range args {
		switch {
		case a == "--override":
			override = true
		case strings.HasPrefix(a, "--target="):
			targetName = strings.TrimPrefix(a, "--target=")
		default:
			dest = a
		}
	}

	rawPayload, err := fs.Sub(payload, ".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "harness-core: internal error reading embedded payload:", err)
		os.Exit(1)
	}

	targetFS, err := target.Build(rawPayload, targetName)
	if err != nil {
		fmt.Fprintln(os.Stderr, "harness-core init:", err)
		os.Exit(1)
	}

	result, err := install.Init(targetFS, dest, override)
	if err != nil {
		fmt.Fprintln(os.Stderr, "harness-core init:", err)
		os.Exit(1)
	}

	for _, f := range result.Written {
		fmt.Println("installed:", f)
	}
	for _, f := range result.Skipped {
		fmt.Println("skipped (already exists):", f)
	}
	fmt.Printf("\n%d file(s) installed, %d skipped (target: %s).\n", len(result.Written), len(result.Skipped), targetName)
	if len(result.Skipped) > 0 && !override {
		fmt.Println("Re-run with --override to replace existing files.")
	}

	prov, err := provenance.Compute(targetFS, coreVersion, targetName)
	if err != nil {
		fmt.Fprintln(os.Stderr, "harness-core init: computing provenance:", err)
		os.Exit(1)
	}
	if err := provenance.Save(dest, prov); err != nil {
		fmt.Fprintln(os.Stderr, "harness-core init: writing provenance:", err)
		os.Exit(1)
	}
	fmt.Println("provenance written:", filepath.Join(dest, provenance.RelPath))
}

func runStatus(args []string) {
	dest := "."
	if len(args) > 0 {
		dest = args[0]
	}

	prov, err := provenance.Load(dest)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("not initialized: no %s found under %s\n", provenance.RelPath, dest)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "harness-core status:", err)
		os.Exit(1)
	}

	fmt.Println("core version:", prov.CoreVersion)
	fmt.Println("target:", prov.Target)
	fmt.Println("installed at:", prov.InstalledAt.Format(time.RFC3339))
	fmt.Println()

	paths := make([]string, 0, len(prov.Files))
	for p := range prov.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var unchangedCount, modifiedCount, missingCount int
	for _, p := range paths {
		want := prov.Files[p]
		got, hashErr := provenance.HashFile(filepath.Join(dest, filepath.FromSlash(p)))
		switch {
		case hashErr != nil && os.IsNotExist(hashErr):
			missingCount++
			fmt.Println("missing:", p)
		case hashErr != nil:
			fmt.Fprintln(os.Stderr, "warning: could not read", p, hashErr)
		case got == want:
			unchangedCount++
		default:
			modifiedCount++
			fmt.Println("modified:", p)
		}
	}

	fmt.Printf("\n%d unchanged, %d modified, %d missing (of %d tracked).\n",
		unchangedCount, modifiedCount, missingCount, len(prov.Files))
}

func runUpdate(args []string) {
	dest := "."
	apply := false
	targetFlag := ""
	var acceptUpstream, keepLocal []string
	for _, a := range args {
		switch {
		case a == "--apply":
			apply = true
		case strings.HasPrefix(a, "--target="):
			targetFlag = strings.TrimPrefix(a, "--target=")
		case strings.HasPrefix(a, "--accept-upstream="):
			acceptUpstream = append(acceptUpstream, strings.TrimPrefix(a, "--accept-upstream="))
		case strings.HasPrefix(a, "--keep-local="):
			keepLocal = append(keepLocal, strings.TrimPrefix(a, "--keep-local="))
		default:
			dest = a
		}
	}

	prov, err := provenance.Load(dest)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("not initialized: no %s found under %s. Run `harness-core init` first.\n", provenance.RelPath, dest)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "harness-core update:", err)
		os.Exit(1)
	}

	targetName := prov.Target
	if targetName == "" {
		targetName = "agent" // provenance predating the Target field
	}
	if targetFlag != "" {
		targetName = targetFlag
	}

	rawPayload, err := fs.Sub(payload, ".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "harness-core: internal error reading embedded payload:", err)
		os.Exit(1)
	}

	targetFS, err := target.Build(rawPayload, targetName)
	if err != nil {
		fmt.Fprintln(os.Stderr, "harness-core update:", err)
		os.Exit(1)
	}

	if targetName != prov.Target {
		fmt.Printf("switching target: %s -> %s\n\n", prov.Target, targetName)
	}

	plan, err := update.BuildPlan(targetFS, dest, prov)
	if err != nil {
		fmt.Fprintln(os.Stderr, "harness-core update: building plan:", err)
		os.Exit(1)
	}

	plan = resolveConflicts(plan, acceptUpstream, keepLocal)

	printPlan(plan)

	if conflicts := plan.Conflicts(); len(conflicts) > 0 {
		fmt.Printf("\n%d conflict(s) found. Resolve them with --accept-upstream=<path> (take the new "+
			"version, backing up the old one first), --keep-local=<path> (keep your edit and stop asking), "+
			"or by hand (edit the local file to match either side), then rerun `harness-core update`.\n",
			len(conflicts))
		os.Exit(1)
	}

	actionable := plan.Actionable()
	upToDate := len(actionable) == 0 && prov.CoreVersion == coreVersion && targetName == prov.Target
	if upToDate {
		fmt.Println("\nAlready up to date.")
		return
	}

	if !apply {
		fmt.Printf("\n%d file(s) would change (core %s -> %s). Rerun with --apply to write them.\n",
			len(actionable), prov.CoreVersion, coreVersion)
		return
	}

	backupDir, err := update.Apply(targetFS, dest, plan)
	if err != nil {
		fmt.Fprintln(os.Stderr, "harness-core update:", err)
		os.Exit(1)
	}
	if backupDir != "" {
		fmt.Println("backed up previous content to:", backupDir)
	}

	newProv, err := provenance.Compute(targetFS, coreVersion, targetName)
	if err != nil {
		fmt.Fprintln(os.Stderr, "harness-core update: computing provenance:", err)
		os.Exit(1)
	}
	if err := provenance.Save(dest, newProv); err != nil {
		fmt.Fprintln(os.Stderr, "harness-core update: writing provenance:", err)
		os.Exit(1)
	}

	fmt.Printf("\nApplied %d change(s). Provenance updated to core version %s.\n", len(actionable), coreVersion)
}

// resolveConflicts applies --accept-upstream and --keep-local resolutions
// to plan's Conflict items before Apply ever sees them, so a specific,
// deliberately chosen file never blocks every other pending change forever.
//
//   - accept-upstream reclassifies the item as Update: Apply will write the
//     new upstream content, and its pre-existing local content gets backed
//     up first, same as any other overwritten file.
//   - keep-local reclassifies the item as KeptLocal: Apply writes nothing.
//     The saved provenance for this path still ends up recording the
//     freshly built upstream hash as BASE (Compute always hashes from
//     targetFS), which is exactly what makes this permanent rather than a
//     one-time skip: next run, local content still differs from that BASE,
//     upstream still equals that BASE, so BuildPlan classifies it as
//     KeptLocal again on its own -- it never re-surfaces as a conflict
//     unless upstream changes the file again.
//
// A path that does not match any current conflict is a no-op with a
// warning (a stale or misspelled flag should not silently succeed).
func resolveConflicts(plan update.Plan, acceptUpstream, keepLocal []string) update.Plan {
	accept := toSet(acceptUpstream)
	keep := toSet(keepLocal)

	for i, item := range plan.Items {
		if item.Category != update.Conflict {
			continue
		}
		switch {
		case accept[item.Path]:
			fmt.Println("resolved via --accept-upstream:", item.Path)
			plan.Items[i].Category = update.Update
			plan.Items[i].Detail = ""
			delete(accept, item.Path)
		case keep[item.Path]:
			fmt.Println("resolved via --keep-local:", item.Path)
			plan.Items[i].Category = update.KeptLocal
			plan.Items[i].Detail = ""
			delete(keep, item.Path)
		}
	}

	for p := range accept {
		fmt.Fprintf(os.Stderr, "warning: --accept-upstream=%s does not match any current conflict\n", p)
	}
	for p := range keep {
		fmt.Fprintf(os.Stderr, "warning: --keep-local=%s does not match any current conflict\n", p)
	}

	return plan
}

func toSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, it := range items {
		set[it] = true
	}
	return set
}

func printPlan(plan update.Plan) {
	var upToDate, keptLocal, adopted, locallyDeleted int
	for _, item := range plan.Items {
		switch item.Category {
		case update.Add:
			fmt.Println("add:", item.Path)
		case update.Update:
			fmt.Println("update:", item.Path)
		case update.Removed:
			fmt.Println("upstream removed (kept locally):", item.Path)
		case update.Conflict:
			fmt.Printf("CONFLICT: %s (%s)\n", item.Path, item.Detail)
		case update.UpToDate:
			upToDate++
		case update.KeptLocal:
			keptLocal++
		case update.Adopted:
			adopted++
		case update.LocallyDeletedUnchanged:
			locallyDeleted++
		}
	}
	fmt.Printf("\n%d up to date, %d kept as local edits, %d already match upstream, %d locally deleted (upstream unchanged).\n",
		upToDate, keptLocal, adopted, locallyDeleted)
}

func runSelfUpdate(args []string) {
	checkOnly := false
	for _, a := range args {
		if a == "--check" {
			checkOnly = true
		}
	}

	client := selfupdate.NewClient(selfUpdateRepo)

	plan, err := client.Plan(coreVersion)
	if err != nil {
		fmt.Fprintln(os.Stderr, "harness-core self-update: checking latest release:", err)
		os.Exit(1)
	}

	if plan.UpToDate {
		fmt.Println("already up to date: core version", coreVersion)
		return
	}

	fmt.Printf("update available: %s -> %s (%s)\n", plan.CurrentVersion, plan.LatestVersion, plan.AssetName)
	if checkOnly {
		return
	}

	if err := client.Apply(plan); err != nil {
		fmt.Fprintln(os.Stderr, "harness-core self-update:", err)
		os.Exit(1)
	}

	fmt.Printf("updated to core version %s. Rerun the command to use the new binary.\n", plan.LatestVersion)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: harness-core init [dest] [--override] [--target=agent|claude|both]")
	fmt.Fprintln(os.Stderr, "       harness-core status [dest]")
	fmt.Fprintln(os.Stderr, "       harness-core update [dest] [--apply] [--target=agent|claude|both]")
	fmt.Fprintln(os.Stderr, "                           [--accept-upstream=<path>]... [--keep-local=<path>]...")
	fmt.Fprintln(os.Stderr, "       harness-core self-update [--check]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "--target selects which skill discovery format(s) to install:")
	fmt.Fprintln(os.Stderr, "  agent  - .agents/skills/ only (vendor-neutral, other agent runtimes)")
	fmt.Fprintln(os.Stderr, "  claude - .claude/skills/ only (Claude Code's native discovery path)")
	fmt.Fprintln(os.Stderr, "  both   - both, self-contained (default)")
}
