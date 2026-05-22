// CLAUDE GENERATED
package curated

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/exemplar"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// recordingAdapter remembers exactly what env was passed to
// Execute/Evaluate so tests can assert on prompt augmentation.
type recordingAdapter struct {
	name        string
	lastExec    session.Envelope
	lastEval    session.Envelope
	executeErr  error
	evaluateErr error
	calls       int
}

func (r *recordingAdapter) Name() string { return r.name }

func (r *recordingAdapter) Evaluate(_ context.Context, env session.Envelope) (session.Envelope, error) {
	r.calls++
	r.lastEval = env
	return env, r.evaluateErr
}

func (r *recordingAdapter) Execute(_ context.Context, env session.Envelope) (session.Envelope, error) {
	r.calls++
	r.lastExec = env
	return env, r.executeErr
}

var _ adapter.Adapter = (*recordingAdapter)(nil)

// captureTracer is a trace.Tracer that records every event.
type captureTracer struct {
	mu     sync.Mutex
	events []captured
}

type captured struct {
	name  string
	level slog.Level
	attrs map[string]any
}

func (c *captureTracer) Event(_ context.Context, level slog.Level, name string, attrs ...slog.Attr) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := make(map[string]any, len(attrs))
	for _, a := range attrs {
		m[a.Key] = a.Value.Any()
	}
	c.events = append(c.events, captured{name: name, level: level, attrs: m})
}

func (c *captureTracer) findOne(name string) *captured {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.events {
		if c.events[i].name == name {
			return &c.events[i]
		}
	}
	return nil
}

// envWithPlaybook builds an Envelope wired for the curated adapter:
// has a populated Evaluate.Playbook so Execute knows which action's
// store partition to consult.
func envWithPlaybook(playbook session.Playbook, prompt string) session.Envelope {
	return session.Envelope{
		Input:    session.Payload{Content: prompt},
		Evaluate: &session.Evaluate{Playbook: playbook, Confidence: 1.0},
	}
}

// --- Construction + name ---

func TestWithStore_HappyPath(t *testing.T) {
	inner := &recordingAdapter{name: "stub"}
	store := newMemStore()
	a := WithStore(inner, store)
	if a.Inner != inner {
		t.Errorf("Inner not set")
	}
	if a.Store == nil {
		t.Errorf("Store not set")
	}
}

func TestName_WrapsInner(t *testing.T) {
	a := WithStore(&recordingAdapter{name: "hermes"}, nil)
	if got := a.Name(); got != "curated(hermes)" {
		t.Errorf("Name() = %q, want curated(hermes)", got)
	}
}

func TestName_NilInner_ReturnsSentinel(t *testing.T) {
	a := &Adapter{}
	if got := a.Name(); got != "curated(<nil>)" {
		t.Errorf("Name() = %q, want curated(<nil>)", got)
	}
}

// --- Evaluate ---

func TestEvaluate_PassesThrough_Unchanged(t *testing.T) {
	inner := &recordingAdapter{name: "stub"}
	a := WithStore(inner, newMemStore(exemplar.Exemplar{
		Action: "process", Prompt: "p", Output: "o", SourceCase: "c1",
	}))
	original := envWithPlaybook(session.PlaybookProcess, "should not be augmented")

	_, err := a.Evaluate(context.Background(), original)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if inner.lastEval.Input.Content != "should not be augmented" {
		t.Errorf("Evaluate should not augment input; got %q", inner.lastEval.Input.Content)
	}
}

func TestEvaluate_NilInner_Errors(t *testing.T) {
	a := &Adapter{}
	_, err := a.Evaluate(context.Background(), session.Envelope{})
	if err == nil {
		t.Fatal("expected error with nil Inner")
	}
}

// --- Execute pass-through cases ---

func TestExecute_NilStore_PassesThrough(t *testing.T) {
	inner := &recordingAdapter{name: "stub"}
	tracer := &captureTracer{}
	a := &Adapter{Inner: inner, Tracer: tracer}

	_, err := a.Execute(context.Background(), envWithPlaybook(session.PlaybookProcess, "raw"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if inner.lastExec.Input.Content != "raw" {
		t.Errorf("nil-store should pass through unchanged; got %q", inner.lastExec.Input.Content)
	}
	if e := tracer.findOne("curated.passthrough"); e == nil || e.attrs["reason"] != "nil_store" {
		t.Errorf("expected passthrough trace with reason=nil_store; got %+v", tracer.events)
	}
}

func TestExecute_NoEvaluate_PassesThrough(t *testing.T) {
	inner := &recordingAdapter{name: "stub"}
	tracer := &captureTracer{}
	a := &Adapter{Inner: inner, Store: newMemStore(), Tracer: tracer}

	env := session.Envelope{Input: session.Payload{Content: "raw"}}
	_, _ = a.Execute(context.Background(), env)

	if inner.lastExec.Input.Content != "raw" {
		t.Errorf("missing Evaluate should pass through; got %q", inner.lastExec.Input.Content)
	}
	if e := tracer.findOne("curated.passthrough"); e == nil || e.attrs["reason"] != "no_playbook" {
		t.Errorf("expected passthrough/no_playbook; got %+v", tracer.events)
	}
}

func TestExecute_EmptyPlaybook_PassesThrough(t *testing.T) {
	inner := &recordingAdapter{name: "stub"}
	a := WithStore(inner, newMemStore())
	env := envWithPlaybook("", "raw")

	_, _ = a.Execute(context.Background(), env)
	if inner.lastExec.Input.Content != "raw" {
		t.Errorf("empty playbook should pass through; got %q", inner.lastExec.Input.Content)
	}
}

func TestExecute_EmptyCorpus_PassesThrough(t *testing.T) {
	inner := &recordingAdapter{name: "stub"}
	tracer := &captureTracer{}
	a := &Adapter{Inner: inner, Store: newMemStore(), Tracer: tracer}

	_, _ = a.Execute(context.Background(), envWithPlaybook(session.PlaybookProcess, "raw"))
	if inner.lastExec.Input.Content != "raw" {
		t.Errorf("empty corpus should pass through; got %q", inner.lastExec.Input.Content)
	}
	if e := tracer.findOne("curated.passthrough"); e == nil || e.attrs["reason"] != "empty_corpus" {
		t.Errorf("expected passthrough/empty_corpus; got %+v", tracer.events)
	}
}

// --- Execute augmentation ---

func TestExecute_RetrievesAndPrepends(t *testing.T) {
	inner := &recordingAdapter{name: "stub"}
	store := newMemStore(exemplar.Exemplar{
		Action: "process", Prompt: "what is 2+2?", Output: "4",
		SourceCase: "c1", Score: 1.0,
	})
	a := WithStore(inner, store)

	_, _ = a.Execute(context.Background(), envWithPlaybook(session.PlaybookProcess, "what is 3+3?"))

	got := inner.lastExec.Input.Content
	if !strings.Contains(got, "[similar-examples count=1]") {
		t.Errorf("expected exemplar block header in augmented input; got:\n%s", got)
	}
	if !strings.Contains(got, "Q: what is 2+2?") || !strings.Contains(got, "A: 4") {
		t.Errorf("expected exemplar Q/A in augmented input; got:\n%s", got)
	}
	if !strings.HasSuffix(got, "what is 3+3?") {
		t.Errorf("expected original prompt to remain at end; got tail %q", got[len(got)-40:])
	}
}

func TestExecute_RetrievesOnlyCorrectAction(t *testing.T) {
	// Two exemplars in different actions. Execute with playbook="process"
	// should only see the process one.
	inner := &recordingAdapter{name: "stub"}
	store := newMemStore(
		exemplar.Exemplar{Action: "process", Prompt: "process Q", Output: "process A", SourceCase: "p1"},
		exemplar.Exemplar{Action: "critique", Prompt: "critique Q", Output: "critique A", SourceCase: "c1"},
	)
	a := WithStore(inner, store)
	_, _ = a.Execute(context.Background(), envWithPlaybook(session.PlaybookProcess, "fresh"))

	got := inner.lastExec.Input.Content
	if !strings.Contains(got, "process Q") {
		t.Errorf("expected process exemplar; got:\n%s", got)
	}
	if strings.Contains(got, "critique Q") {
		t.Errorf("critique exemplar should NOT appear when playbook=process; got:\n%s", got)
	}
}

func TestExecute_RespectsTopK(t *testing.T) {
	inner := &recordingAdapter{name: "stub"}
	store := newMemStore()
	for i := 0; i < 10; i++ {
		store.add(exemplar.Exemplar{
			Action: "process", Prompt: "physics " + string(rune('a'+i)),
			Output: "ans", SourceCase: string(rune('a' + i)), Score: 0.5,
		})
	}
	a := &Adapter{Inner: inner, Store: store, TopK: 2}

	_, _ = a.Execute(context.Background(), envWithPlaybook(session.PlaybookProcess, "physics question"))

	got := inner.lastExec.Input.Content
	if !strings.Contains(got, "[similar-examples count=2]") {
		t.Errorf("expected count=2 header; got:\n%s", got)
	}
}

func TestExecute_RespectsMaxExemplarTokens(t *testing.T) {
	inner := &recordingAdapter{name: "stub"}
	// Three exemplars, each ~50 tokens by our Tokens field.
	store := newMemStore()
	for i := 0; i < 3; i++ {
		store.add(exemplar.Exemplar{
			Action: "process", Prompt: "physics " + string(rune('a'+i)),
			Output: "ans", SourceCase: string(rune('a' + i)), Score: 0.5,
			Tokens: 50,
		})
	}
	a := &Adapter{
		Inner: inner, Store: store, TopK: 10, MaxExemplarTokens: 120,
	}

	_, _ = a.Execute(context.Background(), envWithPlaybook(session.PlaybookProcess, "physics"))

	got := inner.lastExec.Input.Content
	// 120 / 50 = 2 exemplars fit (third would push to 150 > 120).
	if !strings.Contains(got, "[similar-examples count=2]") {
		t.Errorf("expected MaxExemplarTokens=120 to trim to 2 exemplars (50 each); got:\n%s", got)
	}
}

func TestExecute_BudgetExcludesAll_PassesThrough(t *testing.T) {
	// Single exemplar of 1000 tokens, budget of 100 → can't fit any.
	inner := &recordingAdapter{name: "stub"}
	store := newMemStore(exemplar.Exemplar{
		Action: "process", Prompt: "p", Output: "o", SourceCase: "c", Tokens: 1000,
	})
	tracer := &captureTracer{}
	a := &Adapter{Inner: inner, Store: store, MaxExemplarTokens: 100, Tracer: tracer}

	_, _ = a.Execute(context.Background(), envWithPlaybook(session.PlaybookProcess, "raw"))
	if inner.lastExec.Input.Content != "raw" {
		t.Errorf("budget-excluded-all should pass through; got %q", inner.lastExec.Input.Content)
	}
	if e := tracer.findOne("curated.passthrough"); e == nil || e.attrs["reason"] != "budget_excluded_all" {
		t.Errorf("expected passthrough/budget_excluded_all; got %+v", tracer.events)
	}
}

// --- Error semantics ---

func TestExecute_InnerErrorPropagates(t *testing.T) {
	inner := &recordingAdapter{name: "stub", executeErr: errors.New("substrate boom")}
	a := WithStore(inner, newMemStore())
	_, err := a.Execute(context.Background(), envWithPlaybook(session.PlaybookProcess, "raw"))
	if err == nil || err.Error() != "substrate boom" {
		t.Errorf("expected Inner's err to propagate verbatim; got %v", err)
	}
}

func TestExecute_RetrievalErrorPassesThroughUnaugmented(t *testing.T) {
	inner := &recordingAdapter{name: "stub"}
	store := &failingStore{}
	tracer := &captureTracer{}
	a := &Adapter{Inner: inner, Store: store, Tracer: tracer}

	_, err := a.Execute(context.Background(), envWithPlaybook(session.PlaybookProcess, "raw"))
	if err != nil {
		t.Fatalf("retrieval failure should not fail Execute; got %v", err)
	}
	if inner.lastExec.Input.Content != "raw" {
		t.Errorf("on retrieval failure should pass through unchanged; got %q", inner.lastExec.Input.Content)
	}
	if e := tracer.findOne("curated.retrieval_failed"); e == nil {
		t.Errorf("expected curated.retrieval_failed trace; got %+v", tracer.events)
	}
}

// --- Custom Formatter ---

func TestExecute_CustomFormatter_Wins(t *testing.T) {
	inner := &recordingAdapter{name: "stub"}
	store := newMemStore(exemplar.Exemplar{
		Action: "process", Prompt: "p1", Output: "o1", SourceCase: "c1",
	})
	a := &Adapter{
		Inner: inner, Store: store,
		Formatter: func(es []exemplar.Exemplar) string {
			return "<<CUSTOM PREFIX FOR " + es[0].Output + ">>\n"
		},
	}
	_, _ = a.Execute(context.Background(), envWithPlaybook(session.PlaybookProcess, "raw"))

	got := inner.lastExec.Input.Content
	if !strings.HasPrefix(got, "<<CUSTOM PREFIX FOR o1>>") {
		t.Errorf("expected custom formatter output; got:\n%s", got)
	}
}

// --- Default formatter ---

func TestDefaultFormatter_EmptyReturnsEmpty(t *testing.T) {
	if got := DefaultFormatter(nil); got != "" {
		t.Errorf("DefaultFormatter(nil) = %q, want empty", got)
	}
}

func TestDefaultFormatter_StableAcrossCalls(t *testing.T) {
	// Byte-stable output for the same input — KV-cache prefix hits
	// across consecutive calls depend on this.
	es := []exemplar.Exemplar{
		{Action: "x", Prompt: "Q1", Output: "A1"},
		{Action: "x", Prompt: "Q2", Output: "A2"},
	}
	a := DefaultFormatter(es)
	b := DefaultFormatter(es)
	if a != b {
		t.Errorf("DefaultFormatter not byte-stable:\n a=%q\n b=%q", a, b)
	}
}

func TestTrimToBudget_StopsAtCap(t *testing.T) {
	es := []exemplar.Exemplar{
		{Tokens: 30}, {Tokens: 40}, {Tokens: 50},
	}
	got := trimToBudget(es, 75) // 30 + 40 = 70 fits; 70 + 50 = 120 over
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestTrimToBudget_SkipsLargerKeepsSmallerLater(t *testing.T) {
	// 50 fits (budget 100), 60 doesn't (50+60=110), but next 30 does
	// (50+30=80). The greedy individual-check keeps it.
	es := []exemplar.Exemplar{
		{Tokens: 50}, {Tokens: 60}, {Tokens: 30},
	}
	got := trimToBudget(es, 100)
	if len(got) != 2 || got[1].Tokens != 30 {
		t.Errorf("got %+v, want [50, 30]", got)
	}
}

// --- Test helpers: in-memory store ---

// memStore is an in-memory exemplar.Store for tests. Keeps things
// out of the filesystem so tests run fast and don't leak temp dirs.
type memStore struct {
	mu        sync.Mutex
	byAction  map[string][]exemplar.Exemplar
	failOnAdd error
}

func newMemStore(seed ...exemplar.Exemplar) *memStore {
	m := &memStore{byAction: map[string][]exemplar.Exemplar{}}
	for _, e := range seed {
		m.add(e)
	}
	return m
}

func (m *memStore) add(e exemplar.Exemplar) {
	if e.AddedAt.IsZero() {
		e.AddedAt = time.Now()
	}
	if e.Tokens == 0 {
		e.Tokens = exemplar.ApproxTokensByChars(e.Prompt + e.Output)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byAction[e.Action] = append(m.byAction[e.Action], e)
}

func (m *memStore) Add(_ context.Context, e exemplar.Exemplar) error {
	if m.failOnAdd != nil {
		return m.failOnAdd
	}
	m.add(e)
	return nil
}

func (m *memStore) Retrieve(_ context.Context, action, _ string, k int) ([]exemplar.Exemplar, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	all := m.byAction[action]
	if k > len(all) {
		k = len(all)
	}
	out := make([]exemplar.Exemplar, k)
	copy(out, all[:k])
	return out, nil
}

func (m *memStore) GC(_ context.Context) (int, error) { return 0, nil }

var _ exemplar.Store = (*memStore)(nil)

// failingStore.Retrieve always errors. Used to verify the
// pass-through-on-error semantic.
type failingStore struct{}

func (failingStore) Add(_ context.Context, _ exemplar.Exemplar) error { return nil }
func (failingStore) Retrieve(_ context.Context, _, _ string, _ int) ([]exemplar.Exemplar, error) {
	return nil, errors.New("store down")
}
func (failingStore) GC(_ context.Context) (int, error) { return 0, nil }

var _ exemplar.Store = (*failingStore)(nil)
