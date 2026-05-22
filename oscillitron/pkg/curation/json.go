// CLAUDE GENERATED
package curation

import "strings"

// extractFirstJSONObject is the same brace-balancing scanner used by
// pkg/grader, pkg/judge, pkg/recomposer, and pkg/adapter/anthropic
// — extracts the first top-level JSON object from text that may be
// fenced with markdown or preceded by prose. Inlined here so this
// package stays decoupled from the others.
//
// Returns "" if no balanced object is present.
func extractFirstJSONObject(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			switch ch {
			case '\\':
				esc = true
			case '"':
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}
