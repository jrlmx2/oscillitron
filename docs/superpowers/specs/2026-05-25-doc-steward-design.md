# Doc Steward — Design Spec

**Date:** 2026-05-25
**Status:** Draft
**Owner:** Jim

## Problem

Long-lived markdown docs (CLAUDE.md, design notes, references, READMEs) drift from code reality during active development sessions. Nobody notices until someone reads a stale doc and makes a wrong assumption. The gap between "code changed" and "doc updated" is where staleness enters.

## Solution

A Claude Code skill that runs as a sibling session in a parallel terminal, watches the main session's transcript for doc-relevant events, and surfaces findings (flag + proposed edit) to the watcher's terminal in real time.

## Design Constraints

- **General-purpose, user-level.** Installs at `~/.claude/skills/doc-steward/` and works against any project.
- **Token-minimal.** A streaming preprocessor script handles tailing and filtering outside the LLM. The LLM only wakes when structured, compressed signals arrive — never sees raw transcript.
- **User-curated concerns.** A per-project `.claude/doc-steward.md` file defines what the watcher cares about. The watcher never writes to it; it proposes additions in its own terminal output.
- **No action, only proposals.** The watcher produces findings. The user decides what to apply.

## Components

### 1. Skill directory

```
~/.claude/skills/doc-steward/
├── SKILL.md         # behavior definition, loop logic, finding format
└── formatter.sh     # stdin→stdout transcript filter (shell + jq)
```

### 2. Concerns file (per-project)

Lives at `.claude/doc-steward.md` in the project root. Version-controlled with the repo.

Each concern names a doc target, the aspect it tracks, and what signals drift:

```markdown
# Doc Steward Concerns

## CLAUDE.md
- Architecture decisions: locked items must match code reality
- Status section: stage, version, what's complete vs in-progress
- Open questions list: resolved items should migrate to locked

## README.md
- Installation steps match actual build commands
- Feature list matches what's implemented

## oscillitron/CLAUDE.md
- Package inventory matches actual pkg/ contents
- Test commands are current

## INDEX.md
- Every reference/ and skill/ file is listed
- Hook descriptions are still accurate
```

**Rules:**
- User owns this file. Watcher never writes to it.
- Watcher proposes additions in its terminal when it notices recurring uncovered patterns.
- If the file doesn't exist on first invocation, the watcher says so and offers to draft a starter based on the repo's `.md` files. The user accepts/pastes in their main session.

### 3. Formatter script (`formatter.sh`)

A pure stdin→stdout filter. Reads `.jsonl` lines from the Claude Code session transcript, emits only doc-relevant events in a compact delimited format.

**Events kept:**
- `Edit` / `Write` tool calls → path + old/new strings
- `Bash` tool calls that mutate state (git commit, mv, rm, mkdir) → command text
- User messages containing directives or decisions → message text
- Assistant text announcing decisions ("I'll do X", "Locking Y") → text

**Events dropped:**
- Read-only tool calls (`Read`, `Grep`, `Glob`, `LS`)
- Thinking blocks
- Tool results that are pure output (test results, build output) unless they contain errors
- System reminders, task management chatter

**Output format:**

```
--EDIT--
path: pkg/runner/dispatch.go
old: func (r *Runner) dispatch(wave []AP) {
new: func (r *Runner) dispatchWave(ctx context.Context, wave []AP) {
--END--
--USER--
let's drop the parse playbook entirely
--END--
--BASH--
git commit -m "rename dispatch to dispatchWave"
--END--
--ASSISTANT--
Removing the parse playbook from the v0 set.
--END--
```

**Implementation:** shell `while read` loop + jq for JSON field extraction. No dependencies beyond jq (ships with macOS).

### 4. Skill loop (SKILL.md)

On invocation (`/doc-steward` in a fresh terminal):

1. **Discover sibling transcript.** Resolve `~/.claude/projects/<encoded-cwd>/`. List `.jsonl` files by mtime descending. Pick the most-recently-modified one that is not this session's transcript. If none found, report and re-check periodically.

2. **Read concerns.** Load `.claude/doc-steward.md`. If missing, offer to draft a starter and wait.

3. **Start the pipe.** `tail -F <transcript> | bash ~/.claude/skills/doc-steward/formatter.sh` via Monitor.

4. **Batch on silence.** When new formatted events arrive, wait ~15 seconds of pipe silence before processing (avoids mid-burst analysis).

5. **Process batch.** For each batch:
   - Read the accumulated deltas from the pipe.
   - Read the relevant doc files named in concerns.
   - Cross-reference: do any deltas implicate a tracked concern?
   - If yes: produce a finding (flag + proposed edit).
   - If no: stay silent.

6. **Repeat** from step 4 until session exit.

### 5. Finding format

Printed to the watcher's terminal:

```
━━━ FINDING #1 ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Concern: CLAUDE.md → Architecture decisions
Signal:  main session renamed dispatch → dispatchWave, added ctx param
Risk:    CLAUDE.md §Architecture still references "dispatch" by name (line 84)

Proposed edit:
  file: CLAUDE.md
  line: 84
  old: the runner invokes `Inhibitor.Check(Edge)` at each parent→child edge after dispatch
  new: the runner invokes `Inhibitor.Check(Edge)` at each parent→child edge after dispatchWave
```

### 6. Suggested concerns

When the watcher notices recurring signals not covered by an existing concern:

```
━━━ SUGGESTED CONCERN ━━━━━━━━━━━━━━━━━━━━━━━━
I've seen 3 edits to pkg/vram/ with no concern tracking
references/vram-platform-coverage.md. Consider adding:

  ## references/vram-platform-coverage.md
  - Platform probe coverage matches pkg/vram implementations
```

## Session lifecycle

- The watcher runs until the user exits (Ctrl+C / `/quit`).
- Working memory (what the watcher has seen, its evolving understanding of the session) lives only for the watcher session's duration.
- The concerns file is the only durable state.
- If the main session ends before the watcher, the pipe stops producing; the watcher goes idle. It can be pointed at a new session by restarting.

## Token economics

| Phase | Token cost |
|---|---|
| Pipe running, no doc-relevant events | Zero |
| Formatter filtering transcript lines | Zero (shell script, no LLM) |
| LLM processing a batch | Compressed deltas + concern definitions + relevant doc excerpts. Typically hundreds of tokens, not thousands. |
| Finding output | Terminal text, no LLM cost |

The expensive path (LLM reasoning) only fires when structured signals actually arrive AND implicate a tracked concern. Idle sessions cost nothing.

## Future work (explicitly deferred)

- **Launcher script (Approach B).** A `~/.claude/bin/doc-steward` wrapper that auto-opens a terminal window. Folds on top of this design with no changes.
- **Heuristic evolution persistence.** The watcher proposes concern additions in-terminal. A future version could persist "learned" heuristics to a separate file the user opts into.
- **Cross-session memory.** Findings from past watcher sessions surfaced on next invocation.
- **Automatic code drift detection.** The concerns file is file-agnostic — users can track `.go` files, code comments, or anything else today by adding a concern entry. What's deferred is *automatic* detection of code-level drift without explicit concerns (e.g., inferring that a renamed function broke callers in other files).
