---
name: audit-onboarding-proposal
description: Independently audit a brownfield onboarding transcript, operational map, or exact proposed documentation patch before application. Use when a fresh reviewer must verify an $onboard-repository first pass, distinguish environment-caused Unknowns from reasoning defects, score its safety and evidence gates, or run a narrow patch-admissibility decision for specific capsule-backed hunks. This audit is read-only and must not edit files, install tools, start services, create state, or trust the producer's self-score.
---

Read `.agents/skills/audit-onboarding-proposal/SKILL.md` completely and
follow it exactly, including its `scripts/validate_evidence_capsule.py`
tooling. This file exists only so Claude Code's project skill discovery
(`.claude/skills/`) can find and auto-invoke it; the maintained skill
definition, in the vendor-neutral Agent Skills format shared with other
agent runtimes, lives under `.agents/skills/` alongside its scripts and
references.
