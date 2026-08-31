package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"harness-core/internal/install"
	"harness-core/internal/provenance"
	"harness-core/internal/update"
)

// coreVersion identifies the payload this binary ships. Bump it by hand
// whenever AGENTS.md, docs/, or .agents/ change in a way consumers should be
// able to detect via `harness-core status` (and, later, `update`).
const coreVersion = "0.1.0"

// Payload embeds the exact files this CLI ships into a consumer repository.
// docs/ and .agents/ already contain only the curated payload subset; the
// "all:" prefix is required so embed does not silently skip .agents (it
// begins with a dot, which embed excludes by default).
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
	default:
		usage()
		os.Exit(1)
	}
}

func runInit(args []string) {
	dest := "."
	override := false
	for _, a := range args {
		if a == "--override" {
			override = true
			continue
		}
		dest = a
	}

	payloadFS, err := fs.Sub(payload, ".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "harness-core: internal error reading embedded payload:", err)
		os.Exit(1)
	}

	result, err := install.Init(payloadFS, dest, override)
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
	fmt.Printf("\n%d file(s) installed, %d skipped.\n", len(result.Written), len(result.Skipped))
	if len(result.Skipped) > 0 && !override {
		fmt.Println("Re-run with --override to replace existing files.")
	}

	prov, err := provenance.Compute(payloadFS, coreVersion)
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
	for _, a := range args {
		if a == "--apply" {
			apply = true
			continue
		}
		dest = a
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

	payloadFS, err := fs.Sub(payload, ".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "harness-core: internal error reading embedded payload:", err)
		os.Exit(1)
	}

	plan, err := update.BuildPlan(payloadFS, dest, prov)
	if err != nil {
		fmt.Fprintln(os.Stderr, "harness-core update: building plan:", err)
		os.Exit(1)
	}

	printPlan(plan)

	if conflicts := plan.Conflicts(); len(conflicts) > 0 {
		fmt.Printf("\n%d conflict(s) found. Resolve them by hand (edit the local file to keep, "+
			"accept, or merge the upstream change), then rerun `harness-core update`.\n", len(conflicts))
		os.Exit(1)
	}

	actionable := plan.Actionable()
	upToDate := len(actionable) == 0 && prov.CoreVersion == coreVersion
	if upToDate {
		fmt.Println("\nAlready up to date.")
		return
	}

	if !apply {
		fmt.Printf("\n%d file(s) would change (core %s -> %s). Rerun with --apply to write them.\n",
			len(actionable), prov.CoreVersion, coreVersion)
		return
	}

	if err := update.Apply(payloadFS, dest, plan); err != nil {
		fmt.Fprintln(os.Stderr, "harness-core update:", err)
		os.Exit(1)
	}

	newProv, err := provenance.Compute(payloadFS, coreVersion)
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

func usage() {
	fmt.Fprintln(os.Stderr, "usage: harness-core init [dest] [--override]")
	fmt.Fprintln(os.Stderr, "       harness-core status [dest]")
	fmt.Fprintln(os.Stderr, "       harness-core update [dest] [--apply]")
}
