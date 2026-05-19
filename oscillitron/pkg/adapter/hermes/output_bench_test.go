// CLAUDE GENERATED
package hermes

import "testing"

// BenchmarkExtractStructured covers the parser that runs after every
// Hermes Call. With long verdicts (thousands of chars) the LastIndex
// scan could dominate per-call latency if the prompt-output gets big —
// this benchmark catches that.
func BenchmarkExtractStructured_Typical(b *testing.B) {
	raw := "Here is the analysis with some detail.\n\n" +
		"There are several considerations to discuss.\n" +
		"And another paragraph here.\n\n" +
		"```json\n" +
		`{"confidence": 0.85, "signals": ["x", "y"], "open_questions": []}` + "\n" +
		"```\n"
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		extractStructured(raw)
	}
}

func BenchmarkExtractStructured_NoBlock(b *testing.B) {
	raw := "A plain answer without any JSON fences at all, just text content describing the off-by-one bug found in the snippet."
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		extractStructured(raw)
	}
}
