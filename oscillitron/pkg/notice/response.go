// v3.2 response-side detectors + confidence extraction.
//
// v3.1 (notice.go) flagged signals BEFORE the call from the prompt
// shape. v3.2 flags signals AFTER the call from the response shape.
// Same package, same Assessment surface, same Inspector struct —
// just additional detectors and signals.
//
// Scope deliberately tight: we cover the language-pattern signals
// that work universally without needing a format hint (refusal,
// self-correction, internal inconsistency between claimed
// confidence and hedging). Format-aware detectors
// (FormatViolation, RequiredArtifactMissing) are valuable but
// require call-site format declaration; they land in v3.2.x or
// v3.3 when we surface confidence to the user output.

package notice

import (
	"regexp"
	"strings"
)

const (
	// RefusalLanguage fires when the response contains phrases
	// indicating the substrate declined the task. The model itself
	// is telling us it shouldn't be trusted on this one. Strongest
	// downgrade signal in EffectiveConfidence.
	RefusalLanguage Signal = "refusal_language"

	// SelfCorrection fires when the response shows the model
	// reversing or correcting itself mid-stream ("actually...",
	// "wait, let me reconsider..."). Indicates uncertainty even
	// if a final answer is given.
	SelfCorrection Signal = "self_correction"

	// InternalInconsistency fires when the response co-occurs
	// high-confidence claims ("definitely", "100%") with hedging
	// language ("I think", "maybe"). The model is contradicting
	// itself about how sure it is.
	InternalInconsistency Signal = "internal_inconsistency"
)

// ResponseInspection is the input to InspectResponse.
type ResponseInspection struct {
	// Response is the raw text the substrate produced. Required.
	Response string
}

// InspectResponse runs all v3.2 response-side detectors on the
// raw response text and returns the Assessment.
func InspectResponse(r ResponseInspection) Assessment {
	var detections []Detection

	if det, ok := detectRefusal(r.Response); ok {
		detections = append(detections, det)
	}
	if det, ok := detectSelfCorrection(r.Response); ok {
		detections = append(detections, det)
	}
	if det, ok := detectInternalInconsistency(r.Response); ok {
		detections = append(detections, det)
	}

	return Assessment{
		Detections: detections,
		Score:      computeScore(detections),
	}
}

// refusalRE catches the common "model declines" phrases. Patterns
// chosen to be conservative — false positives are worse than false
// negatives here (a false-positive refusal would unnecessarily
// downgrade an actual answer).
//
// Includes contractions explicitly because Go's regexp doesn't
// support lookahead and "I can't" needs to match alongside
// "I cannot".
var refusalRE = regexp.MustCompile(
	`(?i)\b(I cannot|I can ?not|I can't|I won't|I will not|I am unable|I'm unable|outside (my|the) (expertise|knowledge|scope)|I (do not|don't) have enough (information|context|data)|I am not equipped|I'm not equipped|I am not in a position|I'm not in a position|cannot reliably|can't reliably|unable to (provide|answer|determine))`,
)

func detectRefusal(s string) (Detection, bool) {
	loc := refusalRE.FindStringIndex(s)
	if loc == nil {
		return Detection{}, false
	}
	// The model declined. Even if it later produces an answer,
	// trust drops. Warning severity — caller decides what to do.
	return Detection{
		Signal:   RefusalLanguage,
		Severity: Warning,
		Message:  "response contains refusal language; substrate self-flagged inadequacy",
	}, true
}

// selfCorrectionRE catches mid-stream reversals. "Actually" and
// "Wait" are the high-precision ones; the others are common in
// chain-of-thought reasoning where the model walks itself back.
var selfCorrectionRE = regexp.MustCompile(
	`(?i)\b(actually,?\s|wait,?\s|on second thought|let me reconsider|let me re-?examine|I (was|may be) wrong|I (need to|should) (reconsider|rethink)|hmm,?\s|on reflection)`,
)

func detectSelfCorrection(s string) (Detection, bool) {
	// Count matches — one "actually" might be incidental in
	// reasoning ("actually let me think"). Multiple matches are a
	// stronger signal.
	matches := selfCorrectionRE.FindAllStringIndex(s, -1)
	if len(matches) == 0 {
		return Detection{}, false
	}
	sev := Info
	if len(matches) >= 2 {
		sev = Warning
	}
	return Detection{
		Signal:   SelfCorrection,
		Severity: sev,
		Message:  "response includes self-correction phrasing; mid-stream uncertainty",
		Metric:   float64(len(matches)),
	}, true
}

// hedgingRE catches uncertainty language. Used by
// InternalInconsistency to detect co-occurrence with
// high-confidence claims.
var hedgingRE = regexp.MustCompile(
	`(?i)\b(I think|maybe|perhaps|possibly|might be|not (entirely )?sure|uncertain|I (believe|suspect|guess)|likely|probably|appears? to|seems? to|could be|sort of|kind of)\b`,
)

// highConfidenceRE catches strong-claim language.
var highConfidenceRE = regexp.MustCompile(
	`(?i)\b(definitely|certainly|absolutely|undoubtedly|without (a )?doubt|no doubt|completely sure|100\s?%|guaranteed|clearly the (correct|right) answer)\b`,
)

func detectInternalInconsistency(s string) (Detection, bool) {
	hedges := len(hedgingRE.FindAllStringIndex(s, -1))
	confident := len(highConfidenceRE.FindAllStringIndex(s, -1))

	// Two ways to fire:
	//   1. Both hedges AND high-confidence claims appear — direct
	//      contradiction.
	//   2. A numeric confidence is high (≥ 0.9) but hedges ≥ 2 —
	//      the model says "I'm 95% sure" while hedging repeatedly.
	if hedges > 0 && confident > 0 {
		return Detection{
			Signal:   InternalInconsistency,
			Severity: Warning,
			Message:  "response contains both hedging and high-confidence language",
			Metric:   float64(hedges + confident),
		}, true
	}
	if conf, ok := ExtractConfidence(s); ok && conf >= 0.9 && hedges >= 2 {
		return Detection{
			Signal:   InternalInconsistency,
			Severity: Warning,
			Message:  "numerical confidence is high but response hedges repeatedly",
			Metric:   conf,
		}, true
	}
	return Detection{}, false
}

// confidenceRE extracts a "confidence: X.Y" or "confidence = X.Y"
// line from text. Case-insensitive. Tolerates whitespace.
// Captures the numeric portion. The leading \b prevents matching
// inside larger words — without it, "overconfidence: 0.2" would
// match the "confidence: 0.2" suffix and clobber the real value
// (or worse, become the last-match-wins answer when the model
// reasons through calibration explicitly).
var confidenceRE = regexp.MustCompile(`(?i)\bconfidence\s*[:=]\s*([0-9]*\.?[0-9]+)`)

// confidenceLineRE matches a standalone "confidence: X.X" line for stripping.
var confidenceLineRE = regexp.MustCompile(`(?im)^\s*confidence\s*[:=]\s*[0-9]*\.?[0-9]+\s*$`)

// StripConfidenceLine removes the "confidence: X.X" annotation line
// from model output so downstream consumers see only the answer.
func StripConfidenceLine(s string) string {
	return strings.TrimSpace(confidenceLineRE.ReplaceAllString(s, ""))
}

// ExtractConfidence parses a "confidence: 0.X" annotation from raw
// response text. Case-insensitive, last match wins. Returns
// (value, true) on hit; (0, false) on miss. Value normalized via
// NormalizeConfidence.
//
// Designed for the minimal-output prompt; tolerates inline usage.
func ExtractConfidence(raw string) (float64, bool) {
	matches := confidenceRE.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return 0, false
	}
	last := matches[len(matches)-1]
	if len(last) < 2 {
		return 0, false
	}
	return NormalizeConfidence(parseFloat(last[1])), true
}

// NormalizeConfidence maps a parsed-or-extracted confidence value
// to [0, 1] with percent-scale handling:
//
//	val < 0          → 0 (clamp; defensive)
//	val ≤ 1          → val (already a decimal)
//	1 < val ≤ 100    → val / 100 (percent — qwen2.5:7b's pattern)
//	val > 100        → 1 (clamp; out of range either way)
//
// Used by ExtractConfidence (text-parsed path) AND by adapter
// parseReturnResultJSON (JSON-envelope path). Schema-enforced JSON
// confidence still needs this — Ollama's strict mode enforces type
// but NOT numeric bounds, so qwen2.5:7b emits 95/100 thinking
// percent despite the schema saying max=1.
func NormalizeConfidence(val float64) float64 {
	if val < 0 {
		return 0
	}
	if val > 1 && val <= 100 {
		return val / 100
	}
	if val > 1 {
		return 1
	}
	return val
}

// parseFloat is a tiny stdlib-free float parser for [0, 1] range.
// We don't need scientific notation or negative values.
func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	var (
		whole  float64
		frac   float64
		div    float64 = 1
		sawDot bool
	)
	for _, c := range s {
		switch {
		case c == '.':
			if sawDot {
				return -1
			}
			sawDot = true
		case c >= '0' && c <= '9':
			d := float64(c - '0')
			if sawDot {
				div *= 10
				frac += d / div
			} else {
				whole = whole*10 + d
			}
		default:
			return -1
		}
	}
	return whole + frac
}

// EffectiveConfidence applies signal-based downgrades to a raw
// self-reported confidence. Multiplicative across detections;
// each detector contributes a fixed factor:
//
//	RefusalLanguage       × 0.3   (model self-flagged; trust it)
//	InternalInconsistency × 0.6
//	SelfCorrection        × 0.8 (Warning) or × 0.95 (Info)
//	FormatViolation       × 0.7   (when v3.2.x adds it)
//	Other Warnings        × 0.85
//	Other Errors          × 0.5
//
// Floors at 0.0; returns the unmodified raw if Assessment has no
// detections.
func EffectiveConfidence(raw float64, a Assessment) float64 {
	if raw < 0 {
		raw = 0
	}
	if raw > 1 {
		raw = 1
	}
	if len(a.Detections) == 0 {
		return raw
	}
	out := raw
	for _, d := range a.Detections {
		out *= confidenceFactor(d)
	}
	if out < 0 {
		out = 0
	}
	return out
}

// confidenceFactor returns the multiplicative downgrade factor for
// a single detection. Centralized so callers can reason about the
// math without grepping for magic numbers.
func confidenceFactor(d Detection) float64 {
	switch d.Signal {
	case RefusalLanguage:
		return 0.3
	case InternalInconsistency:
		return 0.6
	case SelfCorrection:
		if d.Severity == Warning {
			return 0.8
		}
		return 0.95
	}
	// Generic per-severity factor for any future signal.
	switch d.Severity {
	case Error:
		return 0.5
	case Warning:
		return 0.85
	default:
		return 0.97
	}
}

// InspectResponse is the response-side companion to Inspect on
// the Inspector shim. Adapters call this after the substrate
// returns a raw response.
func (i *Inspector) InspectResponse(response string) Assessment {
	if i == nil {
		return Assessment{}
	}
	return InspectResponse(ResponseInspection{Response: response})
}

// EffectiveConfidenceFromRaw is the one-call helper adapters use to
// recover confidence from a raw response and apply v3.2 signal
// adjustments. Returns (value, true) when a `confidence: X.X`
// annotation was found in the response; (0, false) otherwise.
//
// When inspector is nil, the extracted value is returned unadjusted
// — better than a stub but without v3.2 signal awareness.
//
// Adapter-agnostic by design: pkg/adapter/ollama (and any future
// adapter that wants to recover confidence from unstructured
// minimal-output responses) calls this without coupling to anything
// adapter-specific.
func EffectiveConfidenceFromRaw(raw string, inspector *Inspector) (float64, bool) {
	extracted, ok := ExtractConfidence(raw)
	if !ok {
		return 0, false
	}
	if inspector == nil {
		return extracted, true
	}
	assess := inspector.InspectResponse(raw)
	return EffectiveConfidence(extracted, assess), true
}
