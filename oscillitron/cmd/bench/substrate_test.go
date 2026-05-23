// CLAUDE GENERATED
package main

import "testing"

func TestResolveSubstrate_AutoPicksOllamaForSmallModels(t *testing.T) {
	cases := []struct {
		model string
		want  string
	}{
		// Small models → ollama
		{"phi4-mini:latest", "ollama"},
		{"phi4-mini", "ollama"},
		{"phi3:mini", "ollama"},
		{"llama3.2:3b", "ollama"},
		{"llama3.2:1b", "ollama"},
		{"gemma:7b", "ollama"},
		{"gemma2:9b", "ollama"},
		{"gemma2:2b", "ollama"},
		{"qwen2.5:3b", "ollama"},
		{"qwen2.5-coder:7b", "ollama"},

		// Capable models → hermes (the agentic envelope is fine here)
		{"gemma4:31b", "hermes"},
		{"qwen2.5:72b", "hermes"},
		{"qwen2.5-coder:32b", "hermes"},
		{"llama3.3:70b", "hermes"},
		{"deepseek-r1:70b", "hermes"},

		// Empty model → hermes (let downstream complain about the missing model)
		{"", "hermes"},

		// Case insensitivity
		{"PHI4-MINI:LATEST", "ollama"},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			if got := resolveSubstrate("orchestrator", tc.model); got != tc.want {
				t.Errorf("resolveSubstrate(orchestrator, %q) = %q, want %q", tc.model, got, tc.want)
			}
		})
	}
}

func TestResolveSubstrate_FrontierNeverPicksOllama(t *testing.T) {
	// Even if the model name looks small, the frontier role should
	// not silently downgrade to local Ollama. Frontier is the
	// baseline comparison — making it local-by-default would erase
	// the comparison.
	if got := resolveSubstrate("frontier", "phi4-mini:latest"); got != "hermes" {
		t.Errorf("frontier role should pick hermes regardless of model name; got %q", got)
	}
}
