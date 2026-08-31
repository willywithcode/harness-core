---
name: onboard-repository
description: Inspect an unfamiliar or brownfield repository, trace one real operational path, and propose evidence-backed improvements that help future agents work independently. Use when explicitly asked to onboard, map, assess, or backfill agent-facing repository guidance; use again after the user approves exact proposal items. The first pass is read-only and must not edit files, install tools, start services, create state, or infer missing product policy.
---

Read `.agents/skills/onboard-repository/SKILL.md` completely and follow it
exactly, including its safety contract and its `scripts/render_patch.py` /
`scripts/emit_evidence_bundle.py` tooling. This file exists only so Claude
Code's project skill discovery (`.claude/skills/`) can find and auto-invoke
it; the maintained skill definition, in the vendor-neutral Agent Skills
format shared with other agent runtimes, lives under `.agents/skills/`
alongside its scripts and references.
