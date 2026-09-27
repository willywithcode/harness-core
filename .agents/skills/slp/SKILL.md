---
name: slp
description: "Work as a seat of an slp team (Supervisor, Lead, Peer, Reviewer or Critic). Use when SLP_PROJECT is set in the environment, or when a message starts with [SLP (e.g. [SLP INTRO, [SLP DIRECTIVE, [SLP TASK, [SLP REVIEW)."
---

# slp team

An slp team is a Supervisor, Leads, Peers, Reviewers and a Critic working on
one repository as agents in Herdr panes. The `slp` CLI
(github.com/willywithcode/slp) records every letter in a ledger outside the
repository and delivers it to the target seat's pane.

## Join

1. Run `slp whoami`. If it fails, you are not a seat: stop using this skill.
2. Run `slp guide`. It is the authority for your role: what you decide, what
   you never do, and the exact verbs. Follow it for everything in the team.
3. Your first letter carries your brief. Nothing else needs fetching.

## Rules With This Harness

- Reach other seats only through `slp` verbs. Do not use the `herdr` skill,
  `herdr agent prompt` or pane input to talk to them: those messages bypass
  the ledger, the watch and the team's authority.
- Run each `slp` command on its own (no `&&`, `;` or pipes): slp commands run
  without asking, anything else waits for the Human's approval.
- The team's state stays in `~/.slp`. Never copy letters, the ledger or the
  concept into the repository or create parallel task records for them.
- The Human's concept is read with `slp context`; only the Supervisor
  writes it.
- Git: commit only where your brief says; never push, switch branches, merge
  or rewrite history. slp lands lanes.
- A hand-back is a completion claim under the Completion Standard in
  `docs/WORKFLOW.md`: outcome, changes, behavior-appropriate evidence and
  unresolved risks. Never report unverified work as complete.
- Letters from `slp` itself (NOTICE, INCIDENT, NUDGE) are readings of the
  record, not orders or verdicts: check the record, then act.
