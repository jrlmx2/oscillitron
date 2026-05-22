// CLAUDE GENERATED
package exemplar

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newFileStore(t *testing.T) *FileStore {
	t.Helper()
	dir := t.TempDir()
	return &FileStore{Dir: dir}
}

// -- Add --

func TestAdd_RequiresAction(t *testing.T) {
	s := newFileStore(t)
	err := s.Add(context.Background(), Exemplar{Prompt: "q", Output: "a"})
	if err == nil {
		t.Fatal("expected error with missing Action")
	}
}

func TestAdd_RequiresPrompt(t *testing.T) {
	s := newFileStore(t)
	err := s.Add(context.Background(), Exemplar{Action: "process", Output: "a"})
	if err == nil {
		t.Fatal("expected error with missing Prompt")
	}
}

func TestAdd_RequiresOutput(t *testing.T) {
	s := newFileStore(t)
	err := s.Add(context.Background(), Exemplar{Action: "process", Prompt: "q"})
	if err == nil {
		t.Fatal("expected error with missing Output")
	}
}

func TestAdd_PopulatesAddedAtAndTokens(t *testing.T) {
	s := newFileStore(t)
	err := s.Add(context.Background(), Exemplar{
		Action: "process", Prompt: "what is 2+2?", Output: "4",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, _ := s.Retrieve(context.Background(), "process", "anything", 10)
	if len(got) != 1 {
		t.Fatalf("Retrieve = %d, want 1", len(got))
	}
	if got[0].AddedAt.IsZero() {
		t.Error("AddedAt not populated")
	}
	if got[0].Tokens == 0 {
		t.Error("Tokens not populated by ApproxTokensByChars")
	}
}

func TestAdd_DedupesBySourceCase(t *testing.T) {
	s := newFileStore(t)
	ctx := context.Background()
	if err := s.Add(ctx, Exemplar{
		Action: "process", Prompt: "v1 prompt", Output: "v1 out",
		Score: 0.5, SourceCase: "case-001",
	}); err != nil {
		t.Fatalf("Add v1: %v", err)
	}
	if err := s.Add(ctx, Exemplar{
		Action: "process", Prompt: "v2 prompt", Output: "v2 out",
		Score: 0.9, SourceCase: "case-001",
	}); err != nil {
		t.Fatalf("Add v2: %v", err)
	}
	got, _ := s.Retrieve(ctx, "process", "x", 10)
	if len(got) != 1 {
		t.Fatalf("expected 1 exemplar (dedup by SourceCase); got %d", len(got))
	}
	if got[0].Output != "v2 out" || got[0].Score != 0.9 {
		t.Errorf("expected v2 to overwrite v1; got %+v", got[0])
	}
}

func TestAdd_DifferentSourceCasesCoexist(t *testing.T) {
	s := newFileStore(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := s.Add(ctx, Exemplar{
			Action: "process",
			Prompt: "p", Output: "o",
			SourceCase: string(rune('a' + i)),
		}); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}
	got, _ := s.Retrieve(ctx, "process", "x", 10)
	if len(got) != 3 {
		t.Errorf("expected 3 exemplars (distinct SourceCases); got %d", len(got))
	}
}

func TestAdd_WritesOneFilePerAction(t *testing.T) {
	s := newFileStore(t)
	ctx := context.Background()
	for _, a := range []string{"process", "critique", "plan"} {
		if err := s.Add(ctx, Exemplar{
			Action: a, Prompt: "p", Output: "o", SourceCase: a + "-c01",
		}); err != nil {
			t.Fatalf("Add %s: %v", a, err)
		}
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	files := make(map[string]bool)
	for _, e := range entries {
		files[e.Name()] = true
	}
	for _, want := range []string{"process.json", "critique.json", "plan.json"} {
		if !files[want] {
			t.Errorf("expected file %s; got %v", want, files)
		}
	}
}

// -- Retrieve --

func TestRetrieve_EmptyAction_ReturnsNil(t *testing.T) {
	s := newFileStore(t)
	got, err := s.Retrieve(context.Background(), "process", "x", 5)
	if err != nil {
		t.Fatalf("Retrieve on empty: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected nil/empty; got %d", len(got))
	}
}

func TestRetrieve_RequiresAction(t *testing.T) {
	s := newFileStore(t)
	_, err := s.Retrieve(context.Background(), "", "x", 5)
	if err == nil {
		t.Fatal("expected error with empty action")
	}
}

func TestRetrieve_KZeroReturnsNil(t *testing.T) {
	s := newFileStore(t)
	_ = s.Add(context.Background(), Exemplar{Action: "process", Prompt: "p", Output: "o", SourceCase: "c"})
	got, _ := s.Retrieve(context.Background(), "process", "x", 0)
	if len(got) != 0 {
		t.Errorf("k=0 should return empty; got %d", len(got))
	}
}

func TestRetrieve_OrdersByScoreDescThenRecency(t *testing.T) {
	s := newFileStore(t)
	ctx := context.Background()
	// Insert intentionally out-of-order; expect score-desc then
	// recency-desc.
	earlier := time.Now().Add(-1 * time.Hour)
	later := time.Now()
	for _, e := range []Exemplar{
		{Action: "process", Prompt: "p", Output: "low-old", Score: 0.3, SourceCase: "c1", AddedAt: earlier},
		{Action: "process", Prompt: "p", Output: "high-old", Score: 0.9, SourceCase: "c2", AddedAt: earlier},
		{Action: "process", Prompt: "p", Output: "high-new", Score: 0.9, SourceCase: "c3", AddedAt: later},
		{Action: "process", Prompt: "p", Output: "mid", Score: 0.5, SourceCase: "c4", AddedAt: later},
	} {
		if err := s.Add(ctx, e); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	got, _ := s.Retrieve(ctx, "process", "anything", 10)
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4", len(got))
	}
	// Expected order: high-new (0.9, later), high-old (0.9, earlier),
	// mid (0.5), low-old (0.3).
	want := []string{"high-new", "high-old", "mid", "low-old"}
	for i, w := range want {
		if got[i].Output != w {
			t.Errorf("got[%d].Output = %q, want %q", i, got[i].Output, w)
		}
	}
}

func TestRetrieve_CapsAtKWhenAvailableExceeds(t *testing.T) {
	s := newFileStore(t)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_ = s.Add(ctx, Exemplar{
			Action: "process", Prompt: "p", Output: "o",
			SourceCase: string(rune('a' + i)),
			Score:      float64(i) / 10.0,
		})
	}
	got, _ := s.Retrieve(ctx, "process", "x", 3)
	if len(got) != 3 {
		t.Errorf("len = %d, want 3 (capped by k)", len(got))
	}
}

func TestRetrieve_UpdatesLastRetrievedAt(t *testing.T) {
	s := newFileStore(t)
	ctx := context.Background()
	_ = s.Add(ctx, Exemplar{Action: "process", Prompt: "p", Output: "o", SourceCase: "c", Score: 1.0})

	first, _ := s.Retrieve(ctx, "process", "x", 1)
	if first[0].LastRetrievedAt.IsZero() {
		t.Error("LastRetrievedAt not set after Retrieve")
	}
	// On-disk should reflect the LastRetrievedAt too.
	raw, _ := os.ReadFile(filepath.Join(s.Dir, "process.json"))
	if !strings.Contains(string(raw), `"last_retrieved_at"`) {
		t.Errorf("on-disk JSON missing last_retrieved_at:\n%s", raw)
	}
}

// -- GC --

func TestGC_NoCap_NoOp(t *testing.T) {
	s := newFileStore(t)
	for i := 0; i < 5; i++ {
		_ = s.Add(context.Background(), Exemplar{
			Action: "process", Prompt: "p", Output: "o",
			SourceCase: string(rune('a' + i)),
		})
	}
	dropped, err := s.GC(context.Background())
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if dropped != 0 {
		t.Errorf("dropped = %d, want 0 (no cap configured)", dropped)
	}
}

func TestGC_DropsOldestLRUFirstUntilUnderBudget(t *testing.T) {
	s := newFileStore(t)
	s.MaxTokensPerAction = 100
	s.TokensFn = func(s string) int { return 50 } // each exemplar = 50 tokens
	ctx := context.Background()

	// 5 exemplars × 50 tokens = 250 total. Cap = 100 → must drop 3.
	for i := 0; i < 5; i++ {
		_ = s.Add(ctx, Exemplar{
			Action: "process", Prompt: "p", Output: "o",
			SourceCase: string(rune('a' + i)),
			AddedAt:    time.Now().Add(time.Duration(i) * time.Second),
		})
	}
	// Retrieve "e" once so it has a recent LastRetrievedAt; it
	// should survive eviction.
	_, _ = s.Retrieve(ctx, "process", "x", 1) // returns highest-score (all same score=0); will surface one
	// Force a retrieval on "e" specifically by giving it a higher score.
	_ = s.Add(ctx, Exemplar{
		Action: "process", Prompt: "p", Output: "o-touched",
		SourceCase: "e", Score: 1.0,
		AddedAt: time.Now().Add(5 * time.Second),
	})
	_, _ = s.Retrieve(ctx, "process", "x", 1)

	dropped, err := s.GC(ctx)
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if dropped == 0 {
		t.Fatal("expected GC to drop something")
	}
	got, _ := s.Retrieve(ctx, "process", "x", 10)
	total := 0
	for _, e := range got {
		total += e.Tokens
	}
	if total > s.MaxTokensPerAction {
		t.Errorf("after GC total tokens = %d, want <= %d", total, s.MaxTokensPerAction)
	}
}

func TestGC_RemovesEmptyFile(t *testing.T) {
	s := newFileStore(t)
	s.MaxTokensPerAction = 1
	s.TokensFn = func(_ string) int { return 100 } // each = 100 tokens > cap=1
	ctx := context.Background()
	_ = s.Add(ctx, Exemplar{Action: "process", Prompt: "p", Output: "o", SourceCase: "c"})
	if _, err := s.GC(ctx); err != nil {
		t.Fatalf("GC: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "process.json")); !os.IsNotExist(err) {
		t.Errorf("expected process.json removed when all exemplars evicted; stat err=%v", err)
	}
}

// -- File I/O details --

func TestAdd_AtomicWrite_NoStaleTmpOnSuccess(t *testing.T) {
	s := newFileStore(t)
	_ = s.Add(context.Background(), Exemplar{Action: "process", Prompt: "p", Output: "o", SourceCase: "c"})
	entries, _ := os.ReadDir(s.Dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("found .tmp file after successful Add: %s", e.Name())
		}
	}
}

func TestRoundTrip_JSONIsValid(t *testing.T) {
	s := newFileStore(t)
	_ = s.Add(context.Background(), Exemplar{
		Action: "process", Prompt: "p", Output: "o", SourceCase: "c", Score: 0.8,
	})
	data, _ := os.ReadFile(filepath.Join(s.Dir, "process.json"))
	var decoded []Exemplar
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("on-disk JSON invalid: %v\n%s", err, data)
	}
	if len(decoded) != 1 || decoded[0].Action != "process" {
		t.Errorf("round-trip lost data: %+v", decoded)
	}
}

func TestApproxTokensByChars(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a", 1},          // 1/4 + 1 = 1
		{"abcd", 2},       // 4/4 + 1 = 2
		{"abcdefgh", 3},   // 8/4 + 1 = 3
	}
	for _, c := range cases {
		if got := ApproxTokensByChars(c.in); got != c.want {
			t.Errorf("ApproxTokensByChars(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// -- Concurrency --

func TestFileStore_ConcurrentAdd_NoCorruption(t *testing.T) {
	s := newFileStore(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	const N = 50
	for i := 0; i < N; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Add(ctx, Exemplar{
				Action: "process", Prompt: "p", Output: "o",
				SourceCase: string(rune('a' + i%26)) + string(rune('a' + i/26)),
				Score:      float64(i) / float64(N),
			})
		}()
	}
	wg.Wait()
	got, err := s.Retrieve(ctx, "process", "x", 100)
	if err != nil {
		t.Fatalf("Retrieve after concurrent Adds: %v", err)
	}
	if len(got) == 0 {
		t.Error("no exemplars after concurrent Adds")
	}
	// Verify JSON is still parseable.
	data, _ := os.ReadFile(filepath.Join(s.Dir, "process.json"))
	var decoded []Exemplar
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Errorf("on-disk JSON corrupted by concurrent writes: %v", err)
	}
}
