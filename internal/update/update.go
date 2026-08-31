// Package update plans and applies a three-way comparison between:
//
//   - BASE:     the hashes recorded in provenance.json at install time;
//   - LOCAL:    what is actually on disk right now;
//   - UPSTREAM: the payload embedded in the currently running binary.
//
// The rule is copy-on-conflict, not merge: a file the consumer never
// touched can be safely overwritten with a newer upstream version. A file
// the consumer edited and upstream also changed is a Conflict and is never
// written automatically — Harness's own workflow doctrine is explicit that
// resolving conflicting intent is a human decision, not something a tool
// should guess at.
package update

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"harness-core/internal/install"
	"harness-core/internal/provenance"
)

// Category classifies one tracked or newly-discovered path.
type Category int

const (
	// UpToDate: local matches base, base matches upstream. Nothing to do.
	UpToDate Category = iota
	// Add: upstream ships a new file that does not exist locally yet.
	Add
	// Update: local is untouched since install and upstream changed; safe
	// to overwrite.
	Update
	// KeptLocal: the consumer edited this file and upstream did not
	// change it. Their edit stands; nothing is written.
	KeptLocal
	// Adopted: the consumer's local content already equals the new
	// upstream content (independently converged). No write needed, but
	// it is now tracked as matching upstream.
	Adopted
	// LocallyDeletedUnchanged: the consumer deleted this file and
	// upstream did not change it since. The deletion is respected.
	LocallyDeletedUnchanged
	// Removed: upstream no longer ships this file. It is reported only;
	// the local copy, if any, is never deleted automatically.
	Removed
	// Conflict: both the consumer and upstream changed this file, or
	// upstream changed a file the consumer deleted, or a brand-new
	// upstream file already exists locally with different content. Must
	// be resolved by hand before applying.
	Conflict
)

// Item is one path's classification, with a human-readable reason for
// anything other than the quiet UpToDate/KeptLocal/Adopted/
// LocallyDeletedUnchanged outcomes.
type Item struct {
	Path     string
	Category Category
	Detail   string
}

// Plan is the complete three-way comparison result, one Item per path that
// appears in BASE and/or UPSTREAM.
type Plan struct {
	Items []Item
}

// Conflicts returns every item that needs a human decision before Apply can
// run.
func (p Plan) Conflicts() []Item {
	return filter(p.Items, Conflict)
}

// Actionable returns every item Apply will actually write (Add and Update).
func (p Plan) Actionable() []Item {
	var out []Item
	for _, it := range p.Items {
		if it.Category == Add || it.Category == Update {
			out = append(out, it)
		}
	}
	return out
}

func filter(items []Item, cat Category) []Item {
	var out []Item
	for _, it := range items {
		if it.Category == cat {
			out = append(out, it)
		}
	}
	return out
}

// BuildPlan computes the three-way comparison. It only reads: the embedded
// payload, provenance's recorded BASE hashes, and the consumer's on-disk
// files under destDir. It never writes anything.
func BuildPlan(payloadFS fs.FS, destDir string, prov provenance.Provenance) (Plan, error) {
	upstream, err := provenance.HashAll(payloadFS)
	if err != nil {
		return Plan{}, err
	}
	base := prov.Files

	paths := map[string]bool{}
	for p := range base {
		paths[p] = true
	}
	for p := range upstream {
		paths[p] = true
	}
	sorted := make([]string, 0, len(paths))
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)

	var plan Plan
	for _, path := range sorted {
		baseHash, inBase := base[path]
		upstreamHash, inUpstream := upstream[path]

		localHash, localErr := provenance.HashFile(filepath.Join(destDir, filepath.FromSlash(path)))
		localExists := localErr == nil
		if localErr != nil && !os.IsNotExist(localErr) {
			return Plan{}, fmt.Errorf("reading %s: %w", path, localErr)
		}

		item := Item{Path: path}

		switch {
		case !inBase && inUpstream:
			switch {
			case !localExists:
				item.Category = Add
			case localHash == upstreamHash:
				item.Category = Adopted
			default:
				item.Category = Conflict
				item.Detail = "new upstream file already exists locally with different content"
			}

		case inBase && !inUpstream:
			item.Category = Removed
			item.Detail = "upstream no longer ships this file; local copy left as-is"

		default: // inBase && inUpstream
			switch {
			case !localExists:
				if baseHash == upstreamHash {
					item.Category = LocallyDeletedUnchanged
				} else {
					item.Category = Conflict
					item.Detail = "deleted locally but upstream changed this file"
				}
			case localHash == baseHash:
				if upstreamHash == baseHash {
					item.Category = UpToDate
				} else {
					item.Category = Update
				}
			default: // localHash != baseHash: consumer edited it
				switch {
				case upstreamHash == baseHash:
					item.Category = KeptLocal
				case upstreamHash == localHash:
					item.Category = Adopted
				default:
					item.Category = Conflict
					item.Detail = "modified locally and changed upstream"
				}
			}
		}

		plan.Items = append(plan.Items, item)
	}

	return plan, nil
}

// Apply writes every Add and Update item to destDir. It refuses to write
// anything if the plan still contains unresolved conflicts. Before
// overwriting any Update-category file, it snapshots that file's current
// content under destDir/.harness-core/backup/<timestamp>/ so the consumer
// can recover it by hand if the new upstream content turns out to be
// unwanted. It returns that backup directory, or "" if nothing needed
// backing up (an Add-only or no-op apply).
func Apply(payloadFS fs.FS, destDir string, plan Plan) (backupDir string, err error) {
	if conflicts := plan.Conflicts(); len(conflicts) > 0 {
		return "", fmt.Errorf("%d conflict(s) require manual resolution before applying", len(conflicts))
	}

	actionable := plan.Actionable()

	backupDir, err = backup(destDir, actionable)
	if err != nil {
		return "", fmt.Errorf("backing up before update: %w", err)
	}

	for _, item := range actionable {
		if err := install.WriteFile(payloadFS, destDir, item.Path); err != nil {
			return backupDir, fmt.Errorf("writing %s: %w", item.Path, err)
		}
	}

	return backupDir, nil
}

// backup copies the current on-disk content of every Update-category item
// (the ones about to be overwritten) into a fresh timestamped directory. Add
// items have no prior content and are skipped. Returns "" without creating
// anything if there is nothing to back up.
func backup(destDir string, items []Item) (string, error) {
	var toBackup []Item
	for _, it := range items {
		if it.Category == Update {
			toBackup = append(toBackup, it)
		}
	}
	if len(toBackup) == 0 {
		return "", nil
	}

	stamp := time.Now().UTC().Format("20060102T150405Z")
	backupDir := filepath.Join(destDir, ".harness-core", "backup", stamp)

	for _, item := range toBackup {
		src := filepath.Join(destDir, filepath.FromSlash(item.Path))
		content, err := os.ReadFile(src)
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", item.Path, err)
		}

		dst := filepath.Join(backupDir, filepath.FromSlash(item.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(dst, content, 0o644); err != nil {
			return "", fmt.Errorf("writing backup of %s: %w", item.Path, err)
		}
	}

	return backupDir, nil
}
