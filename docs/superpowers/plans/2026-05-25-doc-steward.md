# Doc Steward Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A user-level Claude Code skill that runs as a sibling session, watches a main session's transcript via a streaming preprocessor, and surfaces doc-staleness findings with proposed edits.

**Architecture:** Two files — `formatter.sh` (shell + jq stdin→stdout filter that extracts doc-relevant events from a `.jsonl` transcript) and `SKILL.md` (defines the watcher loop: discover transcript, start pipe, batch events, cross-reference against a per-project concerns file, produce findings). No external dependencies beyond jq.

**Tech Stack:** Shell (bash), jq, Claude Code skill format (markdown with frontmatter)

---

## File Structure

```
~/.claude/skills/doc-steward/
├── SKILL.md              # Skill definition: frontmatter + behavior loop
├── formatter.sh          # Transcript → compact delimited events
└── test/
    ├── sample.jsonl      # Synthetic transcript for testing
    └── expected.txt      # Expected formatter output for sample.jsonl
```

---

### Task 1: Create skill directory and formatter.sh

**Files:**
- Create: `~/.claude/skills/doc-steward/formatter.sh`
- Create: `~/.claude/skills/doc-steward/test/sample.jsonl`
- Create: `~/.claude/skills/doc-steward/test/expected.txt`

- [ ] **Step 1: Create directory structure**

```bash
mkdir -p ~/.claude/skills/doc-steward/test
```

- [ ] **Step 2: Write the test fixture — sample.jsonl**

A synthetic transcript containing one of each relevant event type plus noise to drop:

```bash
cat > ~/.claude/skills/doc-steward/test/sample.jsonl << 'FIXTURE'
{"type":"last-prompt","leafUuid":"abc","sessionId":"test-session-1"}
{"type":"permission-mode","permissionMode":"default","sessionId":"test-session-1"}
{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"internal reasoning"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","id":"r1","input":{"file_path":"pkg/runner/dispatch.go"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"r1","content":"file contents here"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","id":"e1","input":{"file_path":"pkg/runner/dispatch.go","old_string":"func (r *Runner) dispatch(wave []AP) {","new_string":"func (r *Runner) dispatchWave(ctx context.Context, wave []AP) {","replace_all":false}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"e1","content":"Edit applied successfully"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","id":"w1","input":{"file_path":"pkg/calibration/calibration.go","content":"package calibration\n\n// Calibrator provides confidence calibration.\ntype Calibrator struct {\n\twindow int\n\tsamples []float64\n}\n\nfunc New(window int) *Calibrator {\n\treturn &Calibrator{window: window}\n}\n"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"w1","content":"File written successfully"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","id":"b1","input":{"command":"git add pkg/calibration/ && git commit -m \"feat: add calibration package\"","description":"Commit new calibration package"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"b1","content":"[main abc1234] feat: add calibration package"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","id":"b2","input":{"command":"go test ./pkg/calibration/...","description":"Run calibration tests"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"b2","content":"ok pkg/calibration 0.003s"}]}}
{"type":"user","message":{"content":"let's drop the parse playbook entirely and update CLAUDE.md"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Removing the parse playbook from the v0 set — updating CLAUDE.md to reflect 4 playbooks instead of 5."}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Grep","id":"g1","input":{"pattern":"parse","path":"."}}]}}
{"type":"system","message":{"content":"system reminder about tasks"}}
{"type":"file-history-snapshot","messageId":"snap1","snapshot":{"timestamp":"2026-05-25T10:00:00Z"}}
FIXTURE
```

- [ ] **Step 3: Write expected output for the test fixture**

```bash
cat > ~/.claude/skills/doc-steward/test/expected.txt << 'EXPECTED'
--EDIT--
path: pkg/runner/dispatch.go
old: func (r *Runner) dispatch(wave []AP) {
new: func (r *Runner) dispatchWave(ctx context.Context, wave []AP) {
--END--
--WRITE--
path: pkg/calibration/calibration.go
lines: 11
preview: package calibration
--END--
--BASH--
git add pkg/calibration/ && git commit -m "feat: add calibration package"
--END--
--USER--
let's drop the parse playbook entirely and update CLAUDE.md
--END--
--ASSISTANT--
Removing the parse playbook from the v0 set — updating CLAUDE.md to reflect 4 playbooks instead of 5.
--END--
EXPECTED
```

Events correctly dropped: `last-prompt`, `permission-mode`, thinking block, `Read` tool, `Grep` tool, non-mutating Bash (`go test`), `tool_result` messages, `system`, `file-history-snapshot`.

- [ ] **Step 4: Write formatter.sh**

```bash
cat > ~/.claude/skills/doc-steward/formatter.sh << 'FORMATTER'
#!/usr/bin/env bash
# Doc Steward transcript formatter
# Reads Claude Code .jsonl transcript from stdin, emits compact delimited events.
# Drops: read-only tools, thinking, system, tool_results, snapshots.
# Keeps: Edit, Write, mutating Bash, user directives, short assistant text.

set -euo pipefail

# Mutating command patterns (anchored to start of command)
MUTATING_RE='^(git (add|commit|push|merge|rebase|reset|mv|rm|tag|branch -[dDmM])|mv |rm |mkdir |cp |ln |chmod |chown )'

while IFS= read -r line; do
  type=$(echo "$line" | jq -r '.type // empty' 2>/dev/null) || continue

  case "$type" in
    assistant)
      # Process each content block in the message
      echo "$line" | jq -c '.message.content[]? // empty' 2>/dev/null | while IFS= read -r block; do
        block_type=$(echo "$block" | jq -r '.type // empty')

        case "$block_type" in
          tool_use)
            tool_name=$(echo "$block" | jq -r '.name // empty')

            case "$tool_name" in
              Edit)
                file_path=$(echo "$block" | jq -r '.input.file_path // empty')
                old_str=$(echo "$block" | jq -r '.input.old_string // empty' | head -3)
                new_str=$(echo "$block" | jq -r '.input.new_string // empty' | head -3)
                printf '%s\n' "--EDIT--"
                printf 'path: %s\n' "$file_path"
                printf 'old: %s\n' "$old_str"
                printf 'new: %s\n' "$new_str"
                printf '%s\n' "--END--"
                ;;
              Write)
                file_path=$(echo "$block" | jq -r '.input.file_path // empty')
                content=$(echo "$block" | jq -r '.input.content // empty')
                line_count=$(echo "$content" | wc -l | tr -d ' ')
                preview=$(echo "$content" | head -1)
                printf '%s\n' "--WRITE--"
                printf 'path: %s\n' "$file_path"
                printf 'lines: %s\n' "$line_count"
                printf 'preview: %s\n' "$preview"
                printf '%s\n' "--END--"
                ;;
              Bash)
                cmd=$(echo "$block" | jq -r '.input.command // empty')
                if echo "$cmd" | grep -qE "$MUTATING_RE"; then
                  printf '%s\n' "--BASH--"
                  printf '%s\n' "$cmd"
                  printf '%s\n' "--END--"
                fi
                ;;
              # Read, Grep, Glob, LS, Agent, TaskCreate, etc. → drop
            esac
            ;;
          text)
            txt=$(echo "$block" | jq -r '.text // empty')
            # Only emit short assistant text (announcements, not explanations)
            if [ ${#txt} -le 200 ] && [ ${#txt} -gt 0 ]; then
              printf '%s\n' "--ASSISTANT--"
              printf '%s\n' "$txt"
              printf '%s\n' "--END--"
            fi
            ;;
          # thinking → drop
        esac
      done
      ;;
    user)
      # Only emit user-typed text (strings), not tool_result arrays
      content_type=$(echo "$line" | jq -r '.message.content | type' 2>/dev/null)
      if [ "$content_type" = "string" ]; then
        txt=$(echo "$line" | jq -r '.message.content')
        printf '%s\n' "--USER--"
        printf '%s\n' "$txt"
        printf '%s\n' "--END--"
      fi
      ;;
    # system, attachment, permission-mode, last-prompt, file-history-snapshot, ai-title → drop
  esac
done
FORMATTER
chmod +x ~/.claude/skills/doc-steward/formatter.sh
```

- [ ] **Step 5: Run formatter against test fixture, compare to expected**

Run:
```bash
cat ~/.claude/skills/doc-steward/test/sample.jsonl | bash ~/.claude/skills/doc-steward/formatter.sh > /tmp/actual.txt
diff ~/.claude/skills/doc-steward/test/expected.txt /tmp/actual.txt
```

Expected: no diff output (files match).

- [ ] **Step 6: Run formatter against a real transcript to sanity-check**

Run:
```bash
cat ~/.claude/projects/-Users-james-Documents-Claude-Projects-Oscillitron/5acc8b9a-6b32-4d62-b59c-9a82ade54013.jsonl | bash ~/.claude/skills/doc-steward/formatter.sh | head -40
```

Expected: structured output with `--EDIT--`, `--WRITE--`, `--BASH--`, `--USER--`, `--ASSISTANT--` blocks. No raw JSON. No thinking blocks. No Read/Grep events.

- [ ] **Step 7: Commit**

```bash
cd ~/.claude/skills/doc-steward
git init
git add formatter.sh test/
git commit -m "feat: doc-steward formatter — transcript-to-structured-events filter"
```

---

### Task 2: Write SKILL.md

**Files:**
- Create: `~/.claude/skills/doc-steward/SKILL.md`

- [ ] **Step 1: Write SKILL.md**

```bash
cat > ~/.claude/skills/doc-steward/SKILL.md << 'SKILL'
---
name: doc-steward
description: Watches a sibling Claude Code session's transcript and surfaces documentation staleness findings with proposed edits. Run in a separate terminal alongside your working session.
---

# Doc Steward

You are a documentation steward. You watch a sibling Claude Code session's transcript for changes that may have made project documentation stale, and surface findings with proposed edits.

## Setup

On invocation, perform these steps in order:

### 1. Discover the sibling transcript

Find the most-recently-modified `.jsonl` file in the Claude Code project directory for this cwd:

```bash
PROJECT_DIR="$HOME/.claude/projects/$(echo "$PWD" | sed 's|/|-|g; s|^-||')"
# List .jsonl files by mtime, pick newest that isn't this session
ls -t "$PROJECT_DIR"/*.jsonl 2>/dev/null | head -5
```

Identify which file is the active sibling session (most recently modified, not your own session). If no sibling found, report: "No active sibling session found. Start a working session in another terminal, then retry." and wait for user input.

### 2. Read the concerns file

Read `.claude/doc-steward.md` from the project root. If it doesn't exist:
- Scan the project for `.md` files
- Draft a starter concerns file listing each `.md` with 1-2 relevant aspects
- Print the draft to terminal and say: "No `.claude/doc-steward.md` found. Here's a starter — create the file in your project with the content above (or ask me to adjust it first)."
- Wait for user confirmation before proceeding.

### 3. Start the formatter pipe

Launch the formatter as a background Monitor process:

```bash
tail -F <transcript_path> | bash ~/.claude/skills/doc-steward/formatter.sh
```

This streams structured events as they appear in the sibling session.

### 4. Enter the watch loop

Repeat indefinitely:
1. Wait for new output from the formatter pipe (~15 seconds of silence after last event = batch is ready).
2. Read the accumulated events since last batch.
3. If no events, continue waiting.
4. For each concern in `.claude/doc-steward.md`:
   - Do any events implicate this concern? (file path matches, keyword overlap, semantic relevance)
   - If yes: read the relevant section of the tracked doc file.
   - Compare the doc content against what the events reveal happened.
   - If drift detected: emit a finding.
5. If you notice a pattern not covered by existing concerns (3+ related events with no matching concern), emit a suggested concern.

## Finding Format

Print findings to terminal in this exact format:

```
━━━ FINDING #<n> ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Concern: <file> → <aspect>
Signal:  <what happened in the main session>
Risk:    <what's now stale and where>

Proposed edit:
  file: <path>
  line: <approximate line number>
  old: <current text>
  new: <proposed replacement>
```

## Suggested Concern Format

```
━━━ SUGGESTED CONCERN ━━━━━━━━━━━━━━━━━━━━━━━━
<observation about recurring signals without a matching concern>

Consider adding to .claude/doc-steward.md:

  ## <file path>
  - <aspect to track>
```

## Rules

- Never modify any project files. You only observe and propose.
- Keep findings concise. One concern per finding.
- When unsure whether drift is real, say so in the Risk line ("possible drift" vs "definite drift").
- Batch findings — don't interrupt with single-event noise. Wait for the silence window.
- If the formatter pipe stops producing (sibling session ended), report: "Sibling session appears to have ended. Watching for new activity..." and continue tailing.
SKILL'
```

- [ ] **Step 2: Verify skill is discoverable**

Run:
```bash
ls ~/.claude/skills/doc-steward/SKILL.md && echo "Skill file exists"
head -5 ~/.claude/skills/doc-steward/SKILL.md
```

Expected: file exists, frontmatter shows `name: doc-steward`.

- [ ] **Step 3: Commit**

```bash
cd ~/.claude/skills/doc-steward
git add SKILL.md
git commit -m "feat: doc-steward skill definition — watcher loop and finding format"
```

---

### Task 3: Create example concerns file for Oscillitron

**Files:**
- Create: `/Users/james/Documents/Claude/Projects/Oscillitron/.claude/doc-steward.md`

- [ ] **Step 1: Write the concerns file**

```bash
cat > /Users/james/Documents/Claude/Projects/Oscillitron/.claude/doc-steward.md << 'CONCERNS'
# Doc Steward Concerns

## CLAUDE.md
- Architecture decisions: locked items must match code reality in oscillitron/
- Status section: stage label, version number, what's complete vs in-progress
- Open questions list: resolved items should migrate to "Recently locked" with date
- Stack & tooling: Go version, module path, dependencies

## oscillitron/CLAUDE.md
- Package inventory: every pkg/* directory should be listed with a one-line description
- Test commands: `go test` invocations must work as documented
- Build commands: `go build` targets must be current

## INDEX.md
- Every file in references/ and skills/ has an entry
- Hook descriptions (when-to-load) match actual file contents

## scratch/design-notes.md
- Section headings match concepts still in use (renamed/removed concepts = stale section)
- Code examples reference real types and function signatures

## references/phase1-measurement-guide.md
- Measurement approach matches cmd/phase1 implementation
- Case categories match cases.json entries
CONCERNS
```

- [ ] **Step 2: Verify**

Run:
```bash
cat /Users/james/Documents/Claude/Projects/Oscillitron/.claude/doc-steward.md | head -5
```

Expected: `# Doc Steward Concerns` header visible.

- [ ] **Step 3: Commit in the Oscillitron repo**

```bash
cd /Users/james/Documents/Claude/Projects/Oscillitron
git add .claude/doc-steward.md
git commit -m "feat: add doc-steward concerns file for staleness tracking"
```

---

### Task 4: Integration test — end-to-end dry run

**Files:**
- No new files. Testing existing artifacts against a real transcript.

- [ ] **Step 1: Verify formatter handles a full real transcript without errors**

Run:
```bash
cat ~/.claude/projects/-Users-james-Documents-Claude-Projects-Oscillitron/5acc8b9a-6b32-4d62-b59c-9a82ade54013.jsonl | bash ~/.claude/skills/doc-steward/formatter.sh > /tmp/steward-integration.txt 2>&1
echo "Exit code: $?"
wc -l /tmp/steward-integration.txt
```

Expected: exit code 0, non-zero line count, no jq parse errors.

- [ ] **Step 2: Verify output contains only valid event blocks**

Run:
```bash
# Every non-empty line should be inside a block or a delimiter
grep -v '^--\(EDIT\|WRITE\|BASH\|USER\|ASSISTANT\|END\)--$' /tmp/steward-integration.txt | grep -v '^path: \|^old: \|^new: \|^lines: \|^preview: ' | grep -v '^\s*$' | head -20
```

Expected: only content lines (user text, bash commands, assistant text). No raw JSON, no jq errors, no stray transcript fields.

- [ ] **Step 3: Simulate the skill loop manually**

Open a second terminal, cd to the Oscillitron project, launch `claude`, invoke `/doc-steward`. Verify:
1. It discovers the other session's transcript
2. Reads `.claude/doc-steward.md`
3. Starts the tail pipe
4. If the main session makes an edit, the watcher produces a finding

This is a manual verification step — no automated assertion.

- [ ] **Step 4: Final commit (if any fixes were needed)**

```bash
cd ~/.claude/skills/doc-steward
git add -A
git status
# Only commit if there are changes
git diff --cached --quiet || git commit -m "fix: integration test fixes for formatter"
```
