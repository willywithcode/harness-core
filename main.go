package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"

	"harness-core/internal/install"
)

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
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: harness-core init [dest] [--override]")
}
