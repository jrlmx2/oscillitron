package exemplar

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// --- tokenize ---

func TestTokenize_BasicCases(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"Hello World", []string{"hello", "world"}},
		{"Mixed CASE words", []string{"mixed", "case", "words"}},
		{"with-hyphens and_underscores", []string{"with", "hyphens", "and", "underscores"}},
		{"punctuation, lots! of? it.", []string{"punctuation", "lots", "of", "it"}},
		{"numbers 42 and 3.14", []string{"numbers", "42", "and", "14"}}, // "3" dropped (len<2)
		{"a b c only longer", []string{"only", "longer"}},               // single-char tokens dropped
		{"E1 E2 quantum", []string{"e1", "e2", "quantum"}},              // tech-y tokens preserved
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := tokenize(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("tokenize(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestTokenize_UnicodeLettersAndDigits(t *testing.T) {
	// Non-ASCII letters should split into tokens normally.
	got := tokenize("café 北京 résumé")
	want := []string{"café", "北京", "résumé"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// --- BM25 index ---

func TestBuildBM25Index_CorpusStats(t *testing.T) {
	corpus := []Exemplar{
		{Action: "process", Prompt: "alpha beta gamma"},  // 3 tokens
		{Action: "process", Prompt: "alpha delta"},       // 2 tokens
		{Action: "process", Prompt: "beta epsilon zeta"}, // 3 tokens
	}
	idx := buildBM25Index(corpus)
	if idx.totalDocs != 3 {
		t.Errorf("totalDocs = %d, want 3", idx.totalDocs)
	}
	// avg doc len = (3 + 2 + 3) / 3 = 2.666...
	if idx.avgDocLen < 2.66 || idx.avgDocLen > 2.67 {
		t.Errorf("avgDocLen = %f, want ~2.667", idx.avgDocLen)
	}
	// Document frequencies:
	//   alpha: in 2 docs
	//   beta: in 2 docs
	//   gamma, delta, epsilon, zeta: in 1 doc each
	wantDF := map[string]int{"alpha": 2, "beta": 2, "gamma": 1, "delta": 1, "epsilon": 1, "zeta": 1}
	for term, want := range wantDF {
		if got := idx.docFreqs[term]; got != want {
			t.Errorf("docFreqs[%q] = %d, want %d", term, got, want)
		}
	}
}

func TestBuildBM25Index_RepeatedTermsCountOnceForDF(t *testing.T) {
	// "alpha" appears twice in the doc but contributes 1 to df.
	corpus := []Exemplar{
		{Action: "x", Prompt: "alpha alpha alpha beta"},
	}
	idx := buildBM25Index(corpus)
	if idx.docFreqs["alpha"] != 1 || idx.docFreqs["beta"] != 1 {
		t.Errorf("repeated terms should count once for df; got alpha=%d beta=%d",
			idx.docFreqs["alpha"], idx.docFreqs["beta"])
	}
}

// --- BM25 scoring ---

func TestBM25Score_HigherWithMoreTermOverlap(t *testing.T) {
	corpus := []Exemplar{
		{Action: "x", Prompt: "quantum physics energy"},     // matches all 3 query terms
		{Action: "x", Prompt: "quantum chemistry"},          // matches 1 query term
		{Action: "x", Prompt: "biology evolution genetics"}, // matches 0 query terms
	}
	idx := buildBM25Index(corpus)
	q := tokenize("quantum physics energy levels")

	scores := make([]float64, len(corpus))
	for i := range corpus {
		scores[i] = idx.score(q, i)
	}
	if !(scores[0] > scores[1]) {
		t.Errorf("more overlap should score higher: 3-match=%v vs 1-match=%v", scores[0], scores[1])
	}
	if scores[2] != 0 {
		t.Errorf("no-overlap doc should score 0; got %v", scores[2])
	}
}

func TestBM25Score_RareTermsScoreHigher_IDF(t *testing.T) {
	// "alpha" appears in every doc → IDF ≈ 0.
	// "zeta" appears in one doc → IDF high.
	// A query for "zeta" against the unique doc should score much
	// higher than a query for "alpha" against the same doc.
	corpus := []Exemplar{
		{Action: "x", Prompt: "alpha beta"},
		{Action: "x", Prompt: "alpha gamma"},
		{Action: "x", Prompt: "alpha zeta"},
	}
	idx := buildBM25Index(corpus)

	commonScore := idx.score(tokenize("alpha"), 2)
	rareScore := idx.score(tokenize("zeta"), 2)
	if !(rareScore > commonScore) {
		t.Errorf("rare term should outweigh common: zeta=%v common=%v", rareScore, commonScore)
	}
}

func TestBM25Score_EmptyQuery_ReturnsZero(t *testing.T) {
	idx := buildBM25Index([]Exemplar{{Action: "x", Prompt: "alpha beta"}})
	if got := idx.score(nil, 0); got != 0 {
		t.Errorf("empty query → score=0; got %v", got)
	}
	if got := idx.score(tokenize(""), 0); got != 0 {
		t.Errorf("blank query → score=0; got %v", got)
	}
}

func TestBM25Score_EmptyDoc_ReturnsZero(t *testing.T) {
	idx := buildBM25Index([]Exemplar{{Action: "x", Prompt: ""}})
	if got := idx.score(tokenize("anything"), 0); got != 0 {
		t.Errorf("empty doc → score=0; got %v", got)
	}
}

// --- End-to-end via FileStore.Retrieve ---

func TestRetrieve_BM25_MostRelevantFirst(t *testing.T) {
	s := newFileStore(t)
	ctx := context.Background()

	// All same curation Score so BM25 alone decides ranking.
	for i, p := range []string{
		"the speed of light is roughly 3e8 meters per second",
		"photosynthesis converts light into chemical energy",
		"galileo demonstrated falling bodies accelerate uniformly",
	} {
		_ = s.Add(ctx, Exemplar{
			Action: "process", Prompt: p, Output: "stub", Score: 0.5,
			SourceCase: string(rune('a' + i)),
		})
	}

	// Query strongly matches the first exemplar (light, speed, meters).
	got, err := s.Retrieve(ctx, "process", "what is the speed of light in meters?", 3)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("Retrieve returned 0 hits")
	}
	if !strings.Contains(got[0].Prompt, "speed of light") {
		t.Errorf("expected speed-of-light exemplar first; got %q", got[0].Prompt)
	}
}

func TestRetrieve_BM25_FallbackOrderWhenAllZero(t *testing.T) {
	s := newFileStore(t)
	ctx := context.Background()

	// Add three with no token overlap with the query.
	for i, e := range []Exemplar{
		{Action: "p", Prompt: "alpha beta gamma", Output: "o", SourceCase: "low", Score: 0.2},
		{Action: "p", Prompt: "delta epsilon zeta", Output: "o", SourceCase: "high", Score: 0.9},
		{Action: "p", Prompt: "eta theta iota", Output: "o", SourceCase: "mid", Score: 0.5},
	} {
		_ = s.Add(ctx, e)
		_ = i
	}

	// Query shares no terms → all BM25 scores zero → fallback to
	// Score desc, AddedAt desc.
	got, _ := s.Retrieve(ctx, "p", "completely unrelated jargon", 3)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	wantOrder := []string{"high", "mid", "low"}
	for i, w := range wantOrder {
		if got[i].SourceCase != w {
			t.Errorf("got[%d].SourceCase = %q, want %q (fallback ordering broken)",
				i, got[i].SourceCase, w)
		}
	}
}

func TestRetrieve_BM25_PrefersRelevanceOverCurationScore(t *testing.T) {
	// A high-curation-Score exemplar that doesn't match the query
	// must NOT outrank a low-Score exemplar that DOES. Relevance is
	// the primary signal; curation Score is the tiebreaker.
	s := newFileStore(t)
	ctx := context.Background()
	_ = s.Add(ctx, Exemplar{
		Action: "p", Prompt: "unrelated philosophy of mind", Output: "o",
		SourceCase: "high-score-but-irrelevant", Score: 0.99,
	})
	_ = s.Add(ctx, Exemplar{
		Action: "p", Prompt: "newton's second law of motion force mass acceleration", Output: "o",
		SourceCase: "low-score-but-relevant", Score: 0.10,
	})

	got, _ := s.Retrieve(ctx, "p", "newton's second law force acceleration", 1)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].SourceCase != "low-score-but-relevant" {
		t.Errorf("relevance should outrank curation Score; got %q", got[0].SourceCase)
	}
}

func TestRetrieve_BM25_CuratorScoreTiebreaker(t *testing.T) {
	// Two exemplars with identical Prompts → BM25 ties → curation
	// Score breaks the tie.
	s := newFileStore(t)
	ctx := context.Background()
	_ = s.Add(ctx, Exemplar{
		Action: "p", Prompt: "shared content here", Output: "o",
		SourceCase: "lower", Score: 0.3,
	})
	_ = s.Add(ctx, Exemplar{
		Action: "p", Prompt: "shared content here", Output: "o",
		SourceCase: "higher", Score: 0.8,
	})
	got, _ := s.Retrieve(ctx, "p", "shared content", 2)
	if len(got) != 2 || got[0].SourceCase != "higher" {
		t.Errorf("higher Score should win the tie; got order=%v",
			[]string{got[0].SourceCase, got[1].SourceCase})
	}
}

func TestRetrieve_BM25_QueryOnlySingleCharsTokenized(t *testing.T) {
	// "a x y z" tokenizes to nothing (all single-char filtered).
	// Should behave like an empty query — fall back to Score order.
	s := newFileStore(t)
	ctx := context.Background()
	_ = s.Add(ctx, Exemplar{Action: "p", Prompt: "physics problem one", Output: "o", SourceCase: "a", Score: 0.5})
	_ = s.Add(ctx, Exemplar{Action: "p", Prompt: "physics problem two", Output: "o", SourceCase: "b", Score: 0.9})
	got, _ := s.Retrieve(ctx, "p", "a x y z", 2)
	if got[0].SourceCase != "b" {
		t.Errorf("single-char-only query should fall back to Score order; got %q", got[0].SourceCase)
	}
}
