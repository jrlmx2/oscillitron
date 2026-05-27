# Per-Playbook ResponseFormat Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the Tree orchestrator's 100% error rate by giving each playbook its own `response_format` JSON schema so the engine constrains the model to the right output shape per playbook, not just the process shape.

**Architecture:** Add schema factories for all 5 playbooks in `pkg/adapter/minimal`. Add a `responseFormat` parameter to `oneCall` in the 3 OpenAI-compat adapters (ollama, vllm, lmstudio) so `Execute` can pass the right schema per playbook while `Evaluate` passes `nil`. Wire the per-playbook map in `cmd/bench`.

**Tech Stack:** Go, JSON Schema, Ollama/vLLM/LM Studio OpenAI-compat surface

**Spec:** `scratch/2026-05-25-per-playbook-response-format-design.md`

---

### Task 1: Add per-playbook schema factories to `pkg/adapter/minimal`

**Files:**
- Modify: `oscillitron/pkg/adapter/minimal/minimal.go`
- Modify: `oscillitron/pkg/adapter/minimal/minimal_test.go`

- [ ] **Step 1: Write failing tests for PlanSchema**

Add to `minimal_test.go`:

```go
func TestPlanSchema_Shape(t *testing.T) {
	s := PlanSchema()
	if got, _ := s["type"].(string); got != "object" {
		t.Errorf("schema type = %q, want object", got)
	}
	props, _ := s["properties"].(map[string]any)
	if _, ok := props["sub_aps"]; !ok {
		t.Error("schema missing properties.sub_aps")
	}
	if _, ok := props["recompose"]; !ok {
		t.Error("schema missing properties.recompose")
	}
	required, _ := s["required"].([]string)
	if len(required) != 2 {
		t.Errorf("schema required has %d fields, want 2 (sub_aps, recompose)", len(required))
	}
	if addl, _ := s["additionalProperties"].(bool); addl {
		t.Error("additionalProperties should be false")
	}
}

func TestPlanSchema_RecomposeEnum(t *testing.T) {
	s := PlanSchema()
	props, _ := s["properties"].(map[string]any)
	recompose, _ := props["recompose"].(map[string]any)
	enum, ok := recompose["enum"].([]string)
	if !ok {
		t.Fatal("recompose.enum is not []string")
	}
	want := map[string]bool{"pairwise": true, "sequential": true, "none": true}
	if len(enum) != len(want) {
		t.Errorf("recompose.enum has %d values, want %d", len(enum), len(want))
	}
	for _, v := range enum {
		if !want[v] {
			t.Errorf("recompose.enum contains unexpected value %q", v)
		}
	}
}

func TestPlanSchema_MarshalsCleanJSON(t *testing.T) {
	rf := AsResponseFormat("plan_response", PlanSchema())
	body, err := json.Marshal(rf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var roundtrip map[string]any
	if err := json.Unmarshal(body, &roundtrip); err != nil {
		t.Fatalf("unmarshal roundtrip: %v", err)
	}
	if _, ok := roundtrip["json_schema"]; !ok {
		t.Error("roundtrip lost json_schema field")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd oscillitron && go test ./pkg/adapter/minimal/ -run 'TestPlan' -v`
Expected: FAIL — `PlanSchema` undefined.

- [ ] **Step 3: Implement PlanSchema**

Add to `minimal.go`:

```go
// PlanSchema returns the JSON Schema for the plan playbook's
// emit_subtree output: {sub_aps: [...], recompose: "pairwise"|"sequential"|"none"}.
// The recompose field is enum-constrained so the engine forces a valid
// value — eliminates the empty-string bug that caused 198/198 Tree
// errors on the 2026-05-24 bench.
func PlanSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sub_aps": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"input_kind":         map[string]any{"type": "string"},
						"input":              map[string]any{"type": "string"},
						"output_schema":      map[string]any{"type": "string"},
						"classification":     map[string]any{"type": "string"},
						"needs_verification": map[string]any{"type": "boolean"},
					},
					"required": []string{"input"},
				},
			},
			"recompose": map[string]any{
				"type": "string",
				"enum": []string{"pairwise", "sequential", "none"},
			},
		},
		"required":             []string{"sub_aps", "recompose"},
		"additionalProperties": false,
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd oscillitron && go test ./pkg/adapter/minimal/ -run 'TestPlan' -v`
Expected: PASS

- [ ] **Step 5: Write failing tests for CritiqueSchema**

Add to `minimal_test.go`:

```go
func TestCritiqueSchema_Shape(t *testing.T) {
	s := CritiqueSchema()
	if got, _ := s["type"].(string); got != "object" {
		t.Errorf("schema type = %q, want object", got)
	}
	props, _ := s["properties"].(map[string]any)
	if _, ok := props["verdict"]; !ok {
		t.Error("schema missing properties.verdict")
	}
	if _, ok := props["issues"]; !ok {
		t.Error("schema missing properties.issues")
	}
	required, _ := s["required"].([]string)
	if len(required) != 2 {
		t.Errorf("schema required has %d fields, want 2 (verdict, issues)", len(required))
	}
}

func TestCritiqueSchema_VerdictEnum(t *testing.T) {
	s := CritiqueSchema()
	props, _ := s["properties"].(map[string]any)
	verdict, _ := props["verdict"].(map[string]any)
	enum, ok := verdict["enum"].([]string)
	if !ok {
		t.Fatal("verdict.enum is not []string")
	}
	want := map[string]bool{"pass": true, "fail": true, "issues": true}
	if len(enum) != len(want) {
		t.Errorf("verdict.enum has %d values, want %d", len(enum), len(want))
	}
	for _, v := range enum {
		if !want[v] {
			t.Errorf("verdict.enum contains unexpected value %q", v)
		}
	}
}

func TestCritiqueSchema_MarshalsCleanJSON(t *testing.T) {
	rf := AsResponseFormat("critique_response", CritiqueSchema())
	body, err := json.Marshal(rf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var roundtrip map[string]any
	if err := json.Unmarshal(body, &roundtrip); err != nil {
		t.Fatalf("unmarshal roundtrip: %v", err)
	}
}
```

- [ ] **Step 6: Implement CritiqueSchema (also used by VerifyGrounded)**

Add to `minimal.go`:

```go
// CritiqueSchema returns the JSON Schema for the critique and
// verify_grounded playbooks' verifier_signal output:
// {verdict: "pass"|"fail"|"issues", issues: [...]}.
func CritiqueSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"verdict": map[string]any{
				"type": "string",
				"enum": []string{"pass", "fail", "issues"},
			},
			"issues": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"severity": map[string]any{
							"type": "string",
							"enum": []string{"info", "warning", "error"},
						},
						"where": map[string]any{"type": "string"},
						"what":  map[string]any{"type": "string"},
					},
					"required": []string{"severity", "where", "what"},
				},
			},
		},
		"required":             []string{"verdict", "issues"},
		"additionalProperties": false,
	}
}
```

- [ ] **Step 7: Run all minimal tests**

Run: `cd oscillitron && go test ./pkg/adapter/minimal/ -v`
Expected: ALL PASS

- [ ] **Step 8: Add AllPlaybookFormats and its test**

Add test to `minimal_test.go`:

```go
func TestAllPlaybookFormats_CoversAllPlaybooks(t *testing.T) {
	fmts := AllPlaybookFormats()
	want := []string{"plan", "process", "critique", "verify_grounded", "compose"}
	for _, pb := range want {
		if _, ok := fmts[session.Playbook(pb)]; !ok {
			t.Errorf("AllPlaybookFormats missing playbook %q", pb)
		}
	}
	if len(fmts) != len(want) {
		t.Errorf("AllPlaybookFormats has %d entries, want %d", len(fmts), len(want))
	}
}
```

Add to `minimal.go` (with the import for `session`):

```go
import "github.com/jrlmx2/oscillitron/pkg/session"

// AllPlaybookFormats returns per-playbook response_format maps ready
// to wire into an adapter's ExecuteResponseFormats config. Each entry
// is pre-wrapped with AsResponseFormat.
func AllPlaybookFormats() map[session.Playbook]map[string]any {
	return map[session.Playbook]map[string]any{
		session.PlaybookPlan:           AsResponseFormat("plan_response", PlanSchema()),
		session.PlaybookProcess:        AsResponseFormat("process_response", ProcessSchema()),
		session.PlaybookCritique:       AsResponseFormat("critique_response", CritiqueSchema()),
		session.PlaybookVerifyGrounded: AsResponseFormat("verify_grounded_response", CritiqueSchema()),
		session.PlaybookCompose:        AsResponseFormat("compose_response", ProcessSchema()),
	}
}
```

- [ ] **Step 9: Run all minimal tests**

Run: `cd oscillitron && go test ./pkg/adapter/minimal/ -v`
Expected: ALL PASS

- [ ] **Step 10: Commit**

```bash
git add oscillitron/pkg/adapter/minimal/minimal.go oscillitron/pkg/adapter/minimal/minimal_test.go
git commit -m "feat: add per-playbook JSON schema factories to pkg/adapter/minimal

PlanSchema (enum-constrained recompose), CritiqueSchema (enum-constrained
verdict + severity), and AllPlaybookFormats convenience map. Compose reuses
ProcessSchema; VerifyGrounded reuses CritiqueSchema."
```

---

### Task 2: Add `responseFormat` parameter to ollama adapter's `oneCall`

**Files:**
- Modify: `oscillitron/pkg/adapter/ollama/ollama.go`

- [ ] **Step 1: Add `ExecuteResponseFormats` to Config**

In `ollama.go`, add a new field to the `Config` struct after `ResponseFormat`:

```go
	// ExecuteResponseFormats provides per-playbook response_format
	// schemas for Execute calls. When set, Execute looks up the
	// playbook's schema here first; if missing, falls back to
	// ResponseFormat. Evaluate always passes nil (prompt-only).
	ExecuteResponseFormats map[session.Playbook]map[string]any
```

- [ ] **Step 2: Add `responseFormat` parameter to `oneCall`**

Change the `oneCall` signature from:

```go
func (a *Adapter) oneCall(ctx context.Context, ep Endpoint, env session.Envelope, instructions, phase string) (string, string, tokenUsage, string, error) {
```

to:

```go
func (a *Adapter) oneCall(ctx context.Context, ep Endpoint, env session.Envelope, instructions, phase string, responseFormat map[string]any) (string, string, tokenUsage, string, error) {
```

And change the `chatRequest` construction inside `oneCall` from:

```go
		ResponseFormat: a.cfg.ResponseFormat,
```

to:

```go
		ResponseFormat: responseFormat,
```

- [ ] **Step 3: Update Evaluate to pass nil**

In `Evaluate`, change the `oneCall` call from:

```go
	raw, _, usage, finish, err := a.oneCall(ctx, a.cfg.EvaluateEndpoint, env, instructions, "evaluate")
```

to:

```go
	raw, _, usage, finish, err := a.oneCall(ctx, a.cfg.EvaluateEndpoint, env, instructions, "evaluate", nil)
```

- [ ] **Step 4: Add `executeResponseFormat` helper and update Execute**

Add a helper method:

```go
// executeResponseFormat returns the response_format schema for the
// given playbook. Checks ExecuteResponseFormats first, falls back to
// the global ResponseFormat.
func (a *Adapter) executeResponseFormat(pb session.Playbook) map[string]any {
	if rf, ok := a.cfg.ExecuteResponseFormats[pb]; ok {
		return rf
	}
	return a.cfg.ResponseFormat
}
```

In `Execute`, change the `oneCall` call from:

```go
	raw, reasoning, usage, _, err := a.oneCall(ctx, ep, env, instructions, "execute")
```

to:

```go
	raw, reasoning, usage, _, err := a.oneCall(ctx, ep, env, instructions, "execute", a.executeResponseFormat(pb))
```

- [ ] **Step 5: Verify compilation**

Run: `cd oscillitron && go build ./pkg/adapter/ollama/...`
Expected: clean build

- [ ] **Step 6: Run existing ollama tests**

Run: `cd oscillitron && go test ./pkg/adapter/ollama/ -v`
Expected: ALL PASS

- [ ] **Step 7: Commit**

```bash
git add oscillitron/pkg/adapter/ollama/ollama.go
git commit -m "feat(ollama): per-playbook response_format via ExecuteResponseFormats

oneCall now takes a responseFormat parameter. Execute looks up the
playbook in ExecuteResponseFormats, falls back to ResponseFormat.
Evaluate passes nil (prompt-only, no schema enforcement)."
```

---

### Task 3: Mirror the change in vllm adapter

**Files:**
- Modify: `oscillitron/pkg/adapter/vllm/vllm.go`

The vllm adapter is structurally identical to ollama. Apply the same 4 changes:

- [ ] **Step 1: Add `ExecuteResponseFormats` field to Config**

In `vllm.go`, add the field to Config after `ResponseFormat`:

```go
	// ExecuteResponseFormats provides per-playbook response_format
	// schemas for Execute calls. When set, Execute looks up the
	// playbook's schema here first; if missing, falls back to
	// ResponseFormat. Evaluate always passes nil (prompt-only).
	ExecuteResponseFormats map[session.Playbook]map[string]any
```

- [ ] **Step 2: Add `responseFormat` parameter to `oneCall` and use it**

Change signature to add `responseFormat map[string]any` as the last parameter. Change `ResponseFormat: a.cfg.ResponseFormat` to `ResponseFormat: responseFormat` in the `chatRequest` construction inside `oneCall`.

- [ ] **Step 3: Update Evaluate call to pass nil**

Change:
```go
raw, _, usage, finish, err := a.oneCall(ctx, a.cfg.EvaluateEndpoint, env, instructions, "evaluate")
```
to:
```go
raw, _, usage, finish, err := a.oneCall(ctx, a.cfg.EvaluateEndpoint, env, instructions, "evaluate", nil)
```

- [ ] **Step 4: Add `executeResponseFormat` helper and update Execute call**

Add the same `executeResponseFormat` helper method. Update Execute's `oneCall` call to pass `a.executeResponseFormat(pb)`.

- [ ] **Step 5: Verify build and tests**

Run: `cd oscillitron && go build ./pkg/adapter/vllm/... && go test ./pkg/adapter/vllm/ -v`
Expected: clean build, ALL PASS

- [ ] **Step 6: Commit**

```bash
git add oscillitron/pkg/adapter/vllm/vllm.go
git commit -m "feat(vllm): per-playbook response_format via ExecuteResponseFormats

Mirrors the ollama adapter change — oneCall takes responseFormat param,
Execute looks up per-playbook schema, Evaluate passes nil."
```

---

### Task 4: Mirror the change in lmstudio adapter

**Files:**
- Modify: `oscillitron/pkg/adapter/lmstudio/lmstudio.go`

Identical structure to Tasks 2 and 3.

- [ ] **Step 1: Add `ExecuteResponseFormats` field to Config**

In `lmstudio.go`, add the field to Config after `ResponseFormat`:

```go
	// ExecuteResponseFormats provides per-playbook response_format
	// schemas for Execute calls. When set, Execute looks up the
	// playbook's schema here first; if missing, falls back to
	// ResponseFormat. Evaluate always passes nil (prompt-only).
	ExecuteResponseFormats map[session.Playbook]map[string]any
```

- [ ] **Step 2: Add `responseFormat` parameter to `oneCall` and use it**

Change signature to add `responseFormat map[string]any` as the last parameter. Change `ResponseFormat: a.cfg.ResponseFormat` to `ResponseFormat: responseFormat` in the `chatRequest` construction inside `oneCall`.

- [ ] **Step 3: Update Evaluate call to pass nil**

Change:
```go
raw, _, usage, finish, err := a.oneCall(ctx, a.cfg.EvaluateEndpoint, env, instructions, "evaluate")
```
to:
```go
raw, _, usage, finish, err := a.oneCall(ctx, a.cfg.EvaluateEndpoint, env, instructions, "evaluate", nil)
```

- [ ] **Step 4: Add `executeResponseFormat` helper and update Execute call**

Add the same `executeResponseFormat` helper method. Update Execute's `oneCall` call to pass `a.executeResponseFormat(pb)`.

- [ ] **Step 5: Verify build and tests**

Run: `cd oscillitron && go build ./pkg/adapter/lmstudio/... && go test ./pkg/adapter/lmstudio/ -v`
Expected: clean build, ALL PASS

- [ ] **Step 6: Commit**

```bash
git add oscillitron/pkg/adapter/lmstudio/lmstudio.go
git commit -m "feat(lmstudio): per-playbook response_format via ExecuteResponseFormats

Mirrors the ollama/vllm adapter change — oneCall takes responseFormat
param, Execute looks up per-playbook schema, Evaluate passes nil."
```

---

### Task 5: Wire per-playbook formats in `cmd/bench`

**Files:**
- Modify: `oscillitron/cmd/bench/main.go`

- [ ] **Step 1: Add import for `pkg/adapter/minimal`**

The import already exists (`"github.com/jrlmx2/oscillitron/pkg/adapter/minimal"`). No change needed — verify it's present.

- [ ] **Step 2: Build per-playbook format map in `buildAdapter`**

In `buildAdapter`, change the `structuredOutput` block from:

```go
	var schemaRF map[string]any
	if structuredOutput {
		schemaRF = minimal.AsResponseFormat("process_response", minimal.ProcessSchema())
	}
```

to:

```go
	var schemaRF map[string]any
	var perPlaybookRF map[session.Playbook]map[string]any
	if structuredOutput {
		schemaRF = minimal.AsResponseFormat("process_response", minimal.ProcessSchema())
		perPlaybookRF = minimal.AllPlaybookFormats()
	}
```

Add `"github.com/jrlmx2/oscillitron/pkg/session"` to the imports if not already present.

- [ ] **Step 3: Wire `ExecuteResponseFormats` on each OpenAI-compat adapter**

In the `"ollama"` case, after `cfg.ResponseFormat = schemaRF`, add:

```go
		cfg.ExecuteResponseFormats = perPlaybookRF
```

In the `"lmstudio"` case, after `cfg.ResponseFormat = schemaRF`, add:

```go
		cfg.ExecuteResponseFormats = perPlaybookRF
```

In the `"vllm"` case, after `cfg.ResponseFormat = schemaRF`, add:

```go
		cfg.ExecuteResponseFormats = perPlaybookRF
```

- [ ] **Step 4: Verify full build**

Run: `cd oscillitron && go build ./...`
Expected: clean build

- [ ] **Step 5: Run full test suite**

Run: `cd oscillitron && go test -race ./...`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add oscillitron/cmd/bench/main.go
git commit -m "feat(bench): wire per-playbook ExecuteResponseFormats on all OpenAI-compat adapters

When --structured-output is true (default), each adapter now gets
per-playbook schemas via minimal.AllPlaybookFormats(). Plan calls get
the {sub_aps, recompose} schema with enum-constrained recompose;
critique/verify_grounded get {verdict, issues} with enum-constrained
verdict. Fixes the Tree arm's 198/198 error rate from the 2026-05-24
bench."
```

---

### Task 6: Full suite verification and smoke test

**Files:** None (verification only)

- [ ] **Step 1: Full test suite with race detector**

Run: `cd oscillitron && go test -race ./...`
Expected: ALL PASS, no races

- [ ] **Step 2: Verify go vet is clean**

Run: `cd oscillitron && go vet ./...`
Expected: clean

- [ ] **Step 3: Quick smoke test — run a 5-case Tree bench on llama3.1**

Run:
```bash
cd oscillitron && go run ./cmd/bench \
  --benchmark gpqa \
  --cases cmd/bench/cases/gpqa_diamond.json \
  --orchestrator-substrate ollama \
  --orchestrator-url http://localhost:11434 \
  --orchestrator-model llama3.1:8b-instruct-q6_K \
  --frontier-substrate ollama \
  --frontier-url http://localhost:11434 \
  --frontier-model llama3.1:8b-instruct-q6_K \
  --tree --tree-max-depth 10 \
  --vote-n 1 \
  --stakes medium \
  --minimal-output \
  --notice \
  --cases-limit 5 \
  -v 2>&1 | head -100
```

Expected: Tree cases should NOT error with `plan returned unknown recompose spec ""`. Cases should produce actual pass/fail results (not 100% adapter_error).

**Note:** If `--cases-limit` is not a supported flag, manually ctrl-C after 5 cases complete, or let it run and check the first 5 cases in the output.

- [ ] **Step 4: Verify the plan calls produce valid recompose specs**

In the `-v` output from Step 3, look for plan Execute calls. They should show the model returning JSON with `"recompose": "pairwise"` or `"sequential"` or `"none"` — never an empty string.

```bash
grep 'recompose' /tmp/smoke-tree.log  # if you redirected output
```
