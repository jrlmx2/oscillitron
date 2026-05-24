// Prefix-cache behavior probe — operator tool, NOT a unit test.
//
// Asks the question: "Does Hermes' KV cache hit across session_id
// boundaries when two calls share a byte-stable prompt prefix?"
//
// The answer decides whether PR #19's tree-scoped session_id rework
// is the right path. PR #19 traded invocation-level isolation (the
// "Specialist vs. invocation" lock, parent CLAUDE.md 2026-05-18) for
// what we thought were KV-cache hits. If the underlying inference
// engine does prefix caching by token-prefix bytes (vLLM-style), then
// the cache hits don't need session-sharing — and we should revert
// PR #19 to restore per-AP isolation per the lock.
//
// What the probe does:
//
//	trial 1 (cold):       session-A, input I1 — primes the prefix in cache.
//	trial 2 (same-session): session-A, input I2 — confirms the cache
//	                       works AT ALL within a session.
//	trial 3 (cross-session): session-B (different session_id, identical
//	                       prompt prefix), input I3 — the actual test.
//
// Decision rule:
//   - trial-3 ≈ trial-2  ⇒ prefix-cache is global → safe to revert #19
//   - trial-3 ≈ trial-1  ⇒ prefix-cache is session-scoped → keep #19,
//     or fall back to persona-guided isolation
//
// Three trials per condition by default (configurable) to smooth out
// jitter; report min, median, and max wall-clock for each.
//
// Usage:
//
//	go run ./cmd/probe-prefix-cache --hermes http://127.0.0.1:8642
//	go run ./cmd/probe-prefix-cache --hermes http://127.0.0.1:8642 --hermes-model openrouter:openai/gpt-4o-mini
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter/hermes"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// stableInstructions is the fixed Evaluate preamble both probes share.
// Locking this to a byte-identical string is the whole point — it's
// what creates the cache-friendly prefix the test is measuring.
const stableInstructions = `You are a routing classifier for a multi-agent system. ` +
	`Read the user's input and reply with a single JSON object: ` +
	`{"playbook":"process","rationale":"<short reason>","confidence":<0.0..1.0>}. ` +
	`No prose, no markdown, no commentary. JSON only.`

// probeInputs are short, distinct user inputs. They're intentionally
// trivial — we want the prompt-processing cost to be dominated by the
// shared prefix, not by the input itself.
var probeInputs = []string{
	"name a color",
	"name a fruit",
	"name a country",
	"name a planet",
	"name a number",
	"name an animal",
}

func main() {
	hermesFlag := flag.String("hermes", "", "Hermes BaseURL, e.g. http://127.0.0.1:8642")
	modelFlag := flag.String("hermes-model", "", "model identifier (optional)")
	trials := flag.Int("trials", 3, "trials per condition (>=1)")
	flag.Parse()

	if *hermesFlag == "" {
		fmt.Fprintln(os.Stderr, "probe: --hermes URL required")
		os.Exit(2)
	}
	if *trials < 1 {
		*trials = 1
	}

	cfg := hermes.SingleEndpoint(*hermesFlag, *modelFlag)
	cfg.RawEvaluateInstructions = stableInstructions
	a, err := hermes.New(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "probe: hermes adapter:", err)
		os.Exit(1)
	}

	fmt.Println("Prefix-cache probe against", *hermesFlag)
	if *modelFlag != "" {
		fmt.Println("Model:", *modelFlag)
	}
	fmt.Println("Trials per condition:", *trials)
	fmt.Println()

	// --- trial 1: cold session-A — primes the cache. ---
	fmt.Println("=== trial 1: cold (session-A) ===")
	coldDurations := runTrials(a, "probe-A-cold", *trials, 0)

	// --- trial 2: warm session-A — same session, new inputs. ---
	fmt.Println("\n=== trial 2: warm (session-A, same session as trial 1) ===")
	sameSessionDurations := runTrials(a, "probe-A-cold", *trials, *trials)

	// --- trial 3: cross-session — distinct session_ids, identical prefix. ---
	fmt.Println("\n=== trial 3: cross-session (session-B, new session each call) ===")
	crossSessionDurations := runTrialsRotatingSessions(a, "probe-B-cross-", *trials, 2**trials)

	fmt.Println("\n=== summary ===")
	report("trial 1 (cold, session-A)", coldDurations)
	report("trial 2 (warm, session-A)", sameSessionDurations)
	report("trial 3 (cross-session, B,C,D,...)", crossSessionDurations)

	cold := median(coldDurations)
	warm := median(sameSessionDurations)
	cross := median(crossSessionDurations)

	fmt.Println("\n=== interpretation ===")
	fmt.Printf("cold / warm / cross median (s): %.2f / %.2f / %.2f\n",
		cold.Seconds(), warm.Seconds(), cross.Seconds())
	fmt.Printf("warm / cold:  %.2fx — does the cache work AT ALL?\n", warm.Seconds()/cold.Seconds())
	fmt.Printf("cross / cold: %.2fx — does the cache work ACROSS sessions?\n", cross.Seconds()/cold.Seconds())
	fmt.Println()

	switch {
	case warm.Seconds()/cold.Seconds() > 0.7:
		fmt.Println("VERDICT: cache doesn't seem to be helping within a session either.")
		fmt.Println("→ Investigate Hermes/runtime config before drawing conclusions about PR #19.")
	case cross.Seconds()/cold.Seconds() < 0.5:
		fmt.Println("VERDICT: prefix cache is GLOBAL — hits across session boundaries.")
		fmt.Println("→ Safe to revert PR #19. KV-cache wins survive because the prefix is byte-stable;")
		fmt.Println("  session-sharing was unnecessary and violated the invocation-isolation lock.")
	case cross.Seconds()/cold.Seconds() > 0.85:
		fmt.Println("VERDICT: prefix cache is SESSION-SCOPED — cross-session calls re-process the prefix.")
		fmt.Println("→ Reverting PR #19 would lose the cache win. Fall back to persona-guided isolation")
		fmt.Println("  (instruct the model to treat each turn as independent) and keep tree-scoping.")
	default:
		fmt.Println("VERDICT: inconclusive — cross-session ratio is ambiguous (0.5..0.85).")
		fmt.Println("→ Re-run with --trials larger; check hermes logs for input_tokens to confirm")
		fmt.Println("  whether prompt processing was actually skipped.")
	}
}

func runTrials(a *hermes.Adapter, idPrefix string, count, inputStart int) []time.Duration {
	out := make([]time.Duration, 0, count)
	for i := 0; i < count; i++ {
		input := probeInputs[(inputStart+i)%len(probeInputs)]
		d := timeOne(a, session.ID(idPrefix), input)
		out = append(out, d)
	}
	return out
}

func runTrialsRotatingSessions(a *hermes.Adapter, idPrefix string, count, inputStart int) []time.Duration {
	out := make([]time.Duration, 0, count)
	for i := 0; i < count; i++ {
		// Distinct session per trial — that's the cross-session
		// condition the cache must clear to claim global prefix caching.
		id := session.ID(fmt.Sprintf("%s%c", idPrefix, 'A'+i))
		input := probeInputs[(inputStart+i)%len(probeInputs)]
		d := timeOne(a, id, input)
		out = append(out, d)
	}
	return out
}

func timeOne(a *hermes.Adapter, id session.ID, input string) time.Duration {
	env := session.NewRoot(id, input, "{playbook}", "", session.Budget{DepthRemaining: 1})
	start := time.Now()
	_, err := a.Evaluate(context.Background(), env)
	d := time.Since(start)
	status := "ok"
	if err != nil {
		status = "err: " + err.Error()
	}
	fmt.Printf("  %-32s input=%-20q wall=%.2fs  %s\n", id, input, d.Seconds(), status)
	return d
}

func report(label string, ds []time.Duration) {
	if len(ds) == 0 {
		fmt.Printf("%s: <no data>\n", label)
		return
	}
	sorted := append([]time.Duration(nil), ds...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	fmt.Printf("%s: min=%.2fs  med=%.2fs  max=%.2fs  (n=%d)\n",
		label,
		sorted[0].Seconds(),
		sorted[len(sorted)/2].Seconds(),
		sorted[len(sorted)-1].Seconds(),
		len(sorted))
}

func median(ds []time.Duration) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), ds...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[len(sorted)/2]
}
