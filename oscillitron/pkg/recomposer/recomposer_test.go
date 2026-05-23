// CLAUDE GENERATED
package recomposer

import (
	"context"
	"errors"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

func payload(content string, confidence float64) session.ReturnResultPayload {
	return session.ReturnResultPayload{
		Result:     session.Payload{Kind: "result", Content: content},
		Confidence: confidence,
	}
}

func TestConcat_None(t *testing.T) {
	c := Concat{}
	got, err := c.Recompose(context.Background(), session.RecomposeNone, []session.ReturnResultPayload{
		payload("a", 0.9), payload("b", 0.8),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Result.Content != "" || got.Confidence != 0 {
		t.Errorf("RecomposeNone should return zero payload; got %+v", got)
	}
}

func TestConcat_Sequential(t *testing.T) {
	c := Concat{Separator: " | "}
	got, err := c.Recompose(context.Background(), session.RecomposeSequential, []session.ReturnResultPayload{
		payload("alpha", 0.9),
		payload("beta", 0.7),
		payload("gamma", 0.85),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Result.Content != "alpha | beta | gamma" {
		t.Errorf("Content = %q", got.Result.Content)
	}
	if got.Confidence != 0.7 {
		t.Errorf("Confidence = %v, want 0.7 (min)", got.Confidence)
	}
	if got.Result.Kind != "result" {
		t.Errorf("Kind = %q, want result", got.Result.Kind)
	}
}

func TestConcat_Pairwise_SameStringAsSequential(t *testing.T) {
	// Concat is commutative+associative w.r.t. ordered children, so
	// sequential and pairwise must produce the same content string.
	c := Concat{Separator: "|"}
	children := []session.ReturnResultPayload{
		payload("a", 0.9), payload("b", 0.8), payload("c", 0.7), payload("d", 0.6),
	}
	seq, _ := c.Recompose(context.Background(), session.RecomposeSequential, children)
	pair, _ := c.Recompose(context.Background(), session.RecomposePairwise, children)
	if seq.Result.Content != pair.Result.Content {
		t.Errorf("sequential=%q pairwise=%q (should match for Concat)", seq.Result.Content, pair.Result.Content)
	}
	if seq.Confidence != pair.Confidence {
		t.Errorf("confidence diverged: seq=%v pair=%v", seq.Confidence, pair.Confidence)
	}
}

func TestConcat_Pairwise_OddCount(t *testing.T) {
	// Odd count: trailing element passes through to next round.
	// For 3 children (a, b, c): round 1 reduces (a,b)→ab, passes c.
	// Round 2 reduces (ab, c) → abc.
	c := Concat{Separator: ""}
	got, err := c.Recompose(context.Background(), session.RecomposePairwise, []session.ReturnResultPayload{
		payload("a", 0.5), payload("b", 0.5), payload("c", 0.5),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Result.Content != "abc" {
		t.Errorf("Content = %q, want abc", got.Result.Content)
	}
}

func TestConcat_DefaultSeparator(t *testing.T) {
	// Concat honors Separator as-is — including empty string. Callers
	// wanting the conventional " | " join opt in via DefaultSeparator.
	c := Concat{Separator: DefaultSeparator}
	got, _ := c.Recompose(context.Background(), session.RecomposeSequential, []session.ReturnResultPayload{
		payload("x", 1), payload("y", 1),
	})
	if got.Result.Content != "x | y" {
		t.Errorf("Content = %q, want %q", got.Result.Content, "x | y")
	}

	empty := Concat{} // empty separator means literally no separator
	got2, _ := empty.Recompose(context.Background(), session.RecomposeSequential, []session.ReturnResultPayload{
		payload("a", 1), payload("b", 1),
	})
	if got2.Result.Content != "ab" {
		t.Errorf("empty-separator Content = %q, want %q", got2.Result.Content, "ab")
	}
}

func TestConcat_SingleChild(t *testing.T) {
	c := Concat{Separator: " | "}
	in := payload("solo", 0.42)
	got, err := c.Recompose(context.Background(), session.RecomposeSequential, []session.ReturnResultPayload{in})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Result.Content != "solo" {
		t.Errorf("Content = %q, want %q", got.Result.Content, "solo")
	}
	if got.Confidence != 0.42 {
		t.Errorf("Confidence = %v, want 0.42", got.Confidence)
	}
}

func TestConcat_NoChildren(t *testing.T) {
	c := Concat{}
	_, err := c.Recompose(context.Background(), session.RecomposeSequential, nil)
	if !errors.Is(err, ErrNoChildren) {
		t.Errorf("got %v, want ErrNoChildren", err)
	}
}

func TestConcat_UnknownSpec(t *testing.T) {
	c := Concat{}
	_, err := c.Recompose(context.Background(), session.RecomposeSpec("bogus"), []session.ReturnResultPayload{
		payload("x", 0.5),
	})
	if !errors.Is(err, ErrUnknownSpec) {
		t.Errorf("got %v, want ErrUnknownSpec", err)
	}
}

func TestConcat_ContextCancelled(t *testing.T) {
	c := Concat{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Recompose(ctx, session.RecomposeSequential, []session.ReturnResultPayload{payload("x", 1)})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

func TestConcat_SignalsMerging(t *testing.T) {
	trueV := true
	falseV := false

	cases := []struct {
		name         string
		children     []session.ReturnResultPayload
		wantGrounded *bool
		wantContras  []string
		wantOpenQs   []string
	}{
		{
			name: "both grounded true → true",
			children: []session.ReturnResultPayload{
				{Result: session.Payload{Content: "a"}, Confidence: 1,
					Signals: session.Signals{GroundedPass: &trueV}},
				{Result: session.Payload{Content: "b"}, Confidence: 1,
					Signals: session.Signals{GroundedPass: &trueV}},
			},
			wantGrounded: &trueV,
		},
		{
			name: "one false → false",
			children: []session.ReturnResultPayload{
				{Result: session.Payload{Content: "a"}, Confidence: 1,
					Signals: session.Signals{GroundedPass: &trueV}},
				{Result: session.Payload{Content: "b"}, Confidence: 1,
					Signals: session.Signals{GroundedPass: &falseV}},
			},
			wantGrounded: &falseV,
		},
		{
			name: "one nil → nil (don't synthesize)",
			children: []session.ReturnResultPayload{
				{Result: session.Payload{Content: "a"}, Confidence: 1,
					Signals: session.Signals{GroundedPass: &trueV}},
				{Result: session.Payload{Content: "b"}, Confidence: 1},
			},
			wantGrounded: nil,
		},
		{
			name: "contradictions and open_questions union",
			children: []session.ReturnResultPayload{
				{Result: session.Payload{Content: "a"}, Confidence: 1,
					Signals: session.Signals{
						Contradictions: []string{"x"},
						OpenQuestions:  []string{"p?"},
					}},
				{Result: session.Payload{Content: "b"}, Confidence: 1,
					Signals: session.Signals{
						Contradictions: []string{"y"},
						OpenQuestions:  []string{"q?"},
					}},
			},
			wantContras: []string{"x", "y"},
			wantOpenQs:  []string{"p?", "q?"},
		},
	}

	c := Concat{Separator: ""}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.Recompose(context.Background(), session.RecomposeSequential, tc.children)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			switch {
			case tc.wantGrounded == nil && got.Signals.GroundedPass != nil:
				t.Errorf("GroundedPass = %v, want nil", *got.Signals.GroundedPass)
			case tc.wantGrounded != nil && got.Signals.GroundedPass == nil:
				t.Errorf("GroundedPass = nil, want %v", *tc.wantGrounded)
			case tc.wantGrounded != nil && *got.Signals.GroundedPass != *tc.wantGrounded:
				t.Errorf("GroundedPass = %v, want %v", *got.Signals.GroundedPass, *tc.wantGrounded)
			}
			if len(tc.wantContras) != len(got.Signals.Contradictions) {
				t.Errorf("Contradictions: got %v, want %v", got.Signals.Contradictions, tc.wantContras)
			}
			if len(tc.wantOpenQs) != len(got.Signals.OpenQuestions) {
				t.Errorf("OpenQuestions: got %v, want %v", got.Signals.OpenQuestions, tc.wantOpenQs)
			}
		})
	}
}

func TestConcat_PairwiseFoldOrder(t *testing.T) {
	// Verify the pairwise fold reduces pairs in order: (0,1)(2,3) etc.
	// Using a non-commutative reducer would be more illustrative;
	// here we verify via the Confidence min sequence — round 1 takes
	// min(c0,c1) and min(c2,c3); round 2 takes the min of those.
	c := Concat{Separator: ""}
	got, _ := c.Recompose(context.Background(), session.RecomposePairwise, []session.ReturnResultPayload{
		payload("a", 0.9),
		payload("b", 0.8),
		payload("c", 0.7),
		payload("d", 0.6),
	})
	// Final min should be 0.6 regardless of fold order.
	if got.Confidence != 0.6 {
		t.Errorf("Confidence = %v, want 0.6", got.Confidence)
	}
	if got.Result.Content != "abcd" {
		t.Errorf("Content = %q, want abcd", got.Result.Content)
	}
}

// Compile-time check.
var _ Recomposer = Concat{}
