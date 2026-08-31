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

func usage() {
	fmt.Fprintln(os.Stderr, "usage: harness-core init [dest] [--override]")
	fmt.Fprintln(os.Stderr, "       harness-core status [dest]")
}
