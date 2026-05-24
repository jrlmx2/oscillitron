// CLAUDE GENERATED
package notice

import (
	"testing"
)

// --- detectRefusal ---

func TestDetectRefusal_FiresOnCommonPhrases(t *testing.T) {
	cases := []string{
		"I cannot answer that question.",
		"I can't help with this.",
		"I won't provide an answer.",
		"Sorry, I am unable to provide that.",
		"I'm unable to give a reliable response.",
		"That's outside my expertise.",
		"I don't have enough information to answer.",
		"I am not equipped to handle that.",
		"I cannot reliably answer this.",
		"I'm not in a position to advise on this.",
	}
	for _, raw := range cases {
		a := InspectResponse(ResponseInspection{Response: raw})
		if !hasSignal(a, RefusalLanguage) {
			t.Errorf("expected RefusalLanguage on %q; got %+v", raw, a.Detections)
		}
	}
}

func TestDetectRefusal_DoesNotFireOnSubstantiveAnswers(t *testing.T) {
	cases := []string{
		"The answer is A. Quantum states require...",
		"Looking at the chemistry, the product is C.",
		"I think the answer is B based on the periodic table.",
	}
	for _, raw := range cases {
		a := InspectResponse(ResponseInspection{Response: raw})
		if hasSignal(a, RefusalLanguage) {
			t.Errorf("false positive: RefusalLanguage fired on substantive answer %q", raw)
		}
	}
}

// --- detectSelfCorrection ---

func TestDetectSelfCorrection_SingleMatchIsInfo(t *testing.T) {
	raw := "I think the answer is B. Actually, looking more carefully, it's A."
	a := InspectResponse(ResponseInspection{Response: raw})
	d := findSignal(a, SelfCorrection)
	if d == nil {
		t.Fatalf("expected SelfCorrection; got %+v", a.Detections)
	}
	if d.Severity != Info {
		t.Errorf("Severity = %q, want Info for single match", d.Severity)
	}
}

func TestDetectSelfCorrection_MultipleMatchesIsWarning(t *testing.T) {
	raw := "Actually, wait, on second thought, let me reconsider. The answer might be A. Actually no, it's B."
	a := InspectResponse(ResponseInspection{Response: raw})
	d := findSignal(a, SelfCorrection)
	if d == nil {
		t.Fatalf("expected SelfCorrection")
	}
	if d.Severity != Warning {
		t.Errorf("Severity = %q, want Warning for multiple matches (was %.0f)", d.Severity, d.Metric)
	}
}

func TestDetectSelfCorrection_NoMatch(t *testing.T) {
	raw := "The answer is C because of the chemical reaction."
	a := InspectResponse(ResponseInspection{Response: raw})
	if hasSignal(a, SelfCorrection) {
		t.Errorf("false positive on clean reasoning")
	}
}

// --- detectInternalInconsistency ---

func TestDetectInternalInconsistency_HedgesPlusHighConfidence(t *testing.T) {
	raw := "I think the answer is definitely A. I'm absolutely sure, though maybe B is possible."
	a := InspectResponse(ResponseInspection{Response: raw})
	if !hasSignal(a, InternalInconsistency) {
		t.Fatalf("expected InternalInconsistency on hedge+confidence mix; got %+v", a.Detections)
	}
}

func TestDetectInternalInconsistency_NumericalHighConfidenceWithMultipleHedges(t *testing.T) {
	// Model claims confidence: 0.95 but uses 2+ hedges.
	raw := "I think the answer might be A. It seems to be the right choice.\nconfidence: 0.95"
	a := InspectResponse(ResponseInspection{Response: raw})
	if !hasSignal(a, InternalInconsistency) {
		t.Fatalf("expected InternalInconsistency on high numeric conf + hedges; got %+v", a.Detections)
	}
}

func TestDetectInternalInconsistency_OnlyHedges_NoFire(t *testing.T) {
	raw := "I think the answer might be A. It seems to be the right choice.\nconfidence: 0.5"
	a := InspectResponse(ResponseInspection{Response: raw})
	if hasSignal(a, InternalInconsistency) {
		t.Errorf("hedges-only at low confidence should not fire inconsistency")
	}
}

// --- ExtractConfidence ---

func TestExtractConfidence(t *testing.T) {
	cases := []struct {
		raw     string
		wantVal float64
		wantOK  bool
	}{
		{"confidence: 0.8", 0.8, true},
		{"Confidence: 0.95", 0.95, true},
		{"CONFIDENCE = 1.0", 1.0, true},
		{"my confidence: 0.42 in this answer", 0.42, true},
		{"The answer is A.\nconfidence: 0.7", 0.7, true},
		// Last match wins (model walked through then committed)
		{"I'd estimate confidence: 0.6 initially, then refined to confidence: 0.9", 0.9, true},
		// v3.5 percent-scale normalization.
		{"confidence: 1.5", 0.015, true},
		{"confidence: 50", 0.5, true},
		{"confidence: 95", 0.95, true},
		{"confidence: 100", 1.0, true},
		{"confidence: 200", 1.0, true},
		// Missing → not found
		{"The answer is A. Done.", 0, false},
		{"", 0, false},
		// Integer
		{"confidence: 1", 1.0, true},
		// Leading dot
		{"confidence: .5", 0.5, true},
		// Bug #3 (2026-05-23): the regex previously matched inside
		// "overconfidence" and other -confidence suffix words. With
		// \b leading, these should no longer match — or, when a
		// legitimate "confidence: X" appears alongside the suffix
		// word, that legitimate match wins.
		{"Watch for overconfidence: 0.2", 0, false},
		{"This shows underconfidence: 0.3", 0, false},
		{"hyperconfidence: 0.5", 0, false},
		{"Watch for overconfidence; my confidence: 0.7 in this", 0.7, true},
		{"My confidence: 0.4 — but watch for overconfidence: 0.9", 0.4, true},
	}
	for _, tc := range cases {
		val, ok := ExtractConfidence(tc.raw)
		if ok != tc.wantOK {
			t.Errorf("ExtractConfidence(%q) ok = %v, want %v", tc.raw, ok, tc.wantOK)
		}
		if ok && (val < tc.wantVal-0.001 || val > tc.wantVal+0.001) {
			t.Errorf("ExtractConfidence(%q) = %v, want ~%v", tc.raw, val, tc.wantVal)
		}
	}
}

// --- EffectiveConfidence ---

func TestEffectiveConfidence_NoDetections_PassesRaw(t *testing.T) {
	if got := EffectiveConfidence(0.85, Assessment{}); got != 0.85 {
		t.Errorf("EffectiveConfidence with no detections = %v, want 0.85", got)
	}
}

func TestEffectiveConfidence_RefusalDowngradesHard(t *testing.T) {
	a := Assessment{Detections: []Detection{{Signal: RefusalLanguage, Severity: Warning}}}
	got := EffectiveConfidence(0.9, a)
	// 0.9 × 0.3 = 0.27
	if got < 0.26 || got > 0.28 {
		t.Errorf("EffectiveConfidence with refusal = %v, want ~0.27", got)
	}
}

func TestEffectiveConfidence_MultipleDetections_Multiplicative(t *testing.T) {
	a := Assessment{Detections: []Detection{
		{Signal: SelfCorrection, Severity: Warning},        // × 0.8
		{Signal: InternalInconsistency, Severity: Warning}, // × 0.6
	}}
	got := EffectiveConfidence(1.0, a)
	// 1.0 × 0.8 × 0.6 = 0.48
	if got < 0.47 || got > 0.49 {
		t.Errorf("EffectiveConfidence with multiple = %v, want ~0.48", got)
	}
}

func TestEffectiveConfidence_RawClamped(t *testing.T) {
	a := Assessment{}
	if got := EffectiveConfidence(1.5, a); got != 1.0 {
		t.Errorf("raw 1.5 should clamp to 1.0; got %v", got)
	}
	if got := EffectiveConfidence(-0.5, a); got != 0.0 {
		t.Errorf("raw -0.5 should clamp to 0.0; got %v", got)
	}
}

// --- NormalizeConfidence (v3.5 percent-scale handling) ---

func TestNormalizeConfidence(t *testing.T) {
	cases := []struct {
		in   float64
		want float64
	}{
		// Decimal scale (expected).
		{0.0, 0.0},
		{0.5, 0.5},
		{0.95, 0.95},
		{1.0, 1.0},
		// Percent scale (qwen2.5:7b's pattern when schema bounds
		// aren't engine-enforced).
		{50, 0.5},
		{95, 0.95},
		{100, 1.0},
		{1.5, 0.015},
		// Out of range above 100 — clamp.
		{200, 1.0},
		// Negative — clamp.
		{-0.5, 0.0},
	}
	for _, tc := range cases {
		got := NormalizeConfidence(tc.in)
		if got < tc.want-0.001 || got > tc.want+0.001 {
			t.Errorf("NormalizeConfidence(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// --- Inspector.InspectResponse ---

func TestInspector_InspectResponse_FlowsToResponseDetectors(t *testing.T) {
	i := &Inspector{}
	a := i.InspectResponse("I cannot reliably answer this.")
	if !hasSignal(a, RefusalLanguage) {
		t.Errorf("Inspector.InspectResponse didn't fire response-side detectors")
	}
}

func TestInspector_InspectResponse_NilSafe(t *testing.T) {
	var i *Inspector
	a := i.InspectResponse("anything")
	if len(a.Detections) != 0 {
		t.Errorf("nil Inspector should return empty assessment; got %+v", a.Detections)
	}
}

// --- v3.3 EffectiveConfidenceFromRaw (adapter-agnostic stamping) ---

func TestEffectiveConfidenceFromRaw_NoConfidenceLine_ReturnsFalse(t *testing.T) {
	got, ok := EffectiveConfidenceFromRaw("Just A.", nil)
	if ok {
		t.Errorf("expected ok=false when no confidence: line; got val=%v", got)
	}
}

func TestEffectiveConfidenceFromRaw_NilInspector_ReturnsRaw(t *testing.T) {
	// No Inspector → no signal adjustments. Returns the raw extracted
	// value unchanged.
	got, ok := EffectiveConfidenceFromRaw("A\nconfidence: 0.7", nil)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if got < 0.69 || got > 0.71 {
		t.Errorf("got %v, want 0.7 (unadjusted)", got)
	}
}

func TestEffectiveConfidenceFromRaw_WithInspector_AppliesSignalAdjustments(t *testing.T) {
	// Response has a refusal phrase. With Inspector wired,
	// EffectiveConfidence should multiply by 0.3.
	insp := &Inspector{}
	got, ok := EffectiveConfidenceFromRaw("I cannot reliably answer this.\nA\nconfidence: 0.9", insp)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	// 0.9 × 0.3 = 0.27
	if got < 0.26 || got > 0.28 {
		t.Errorf("got %v, want ~0.27 (refusal × 0.3 downgrade)", got)
	}
}

func TestEffectiveConfidenceFromRaw_CleanResponse_NoAdjustment(t *testing.T) {
	insp := &Inspector{}
	got, ok := EffectiveConfidenceFromRaw("The answer is A.\nconfidence: 0.85", insp)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if got < 0.84 || got > 0.86 {
		t.Errorf("clean response should pass extracted value through; got %v", got)
	}
}

// --- combined scenario ---

func TestInspectResponse_RealisticBenchOutput(t *testing.T) {
	// What a phi4-mini might emit on a hard question — chain of
	// thought, mid-stream uncertainty, eventual commit, low
	// confidence.
	raw := `Looking at the chemistry, I think it might be A. Actually wait, let me reconsider. The trans-cinnamaldehyde + methylmagnesium bromide reaction... possibly C is correct. I'm not entirely sure.
C
confidence: 0.4`
	a := InspectResponse(ResponseInspection{Response: raw})
	if !hasSignal(a, SelfCorrection) {
		t.Errorf("expected SelfCorrection on this realistic mid-stream uncertainty")
	}
	// Should be self-correction Warning (2 markers: "Actually wait" + "let me reconsider")
	d := findSignal(a, SelfCorrection)
	if d.Severity != Warning {
		t.Errorf("Severity = %q, want Warning (multiple corrections)", d.Severity)
	}
	// And the confidence extracts correctly.
	conf, ok := ExtractConfidence(raw)
	if !ok || conf != 0.4 {
		t.Errorf("ExtractConfidence = (%v, %v), want (0.4, true)", conf, ok)
	}
	// Effective confidence: raw 0.4 × SelfCorrection 0.8 = 0.32
	eff := EffectiveConfidence(conf, a)
	if eff < 0.31 || eff > 0.33 {
		t.Errorf("Effective = %v, want ~0.32", eff)
	}
}
