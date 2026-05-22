// CLAUDE GENERATED
package benchmark

import "fmt"

// Pricing assigns a blended USD-per-million-tokens rate to a single
// orchestrator's model. The bench uses one number (blended over the
// typical input/output split) rather than separate input/output rates
// because Answer.TokensUsed is aggregated — the underlying adapter
// doesn't surface the I/O split through the benchmark interface.
//
// Operators who need exact I/O accounting should bypass this and use
// pkg/cost.Pricing directly via a custom orchestrator wrapper.
//
// For typical Anthropic models the blended rate works out close to:
//
//   - Sonnet 4.6: ~$4.50 / Mtok (blended; $3 in + $15 out at typical mix)
//   - Haiku 4.5:  ~$1.30 / Mtok (blended; $0.80 in + $4 out at typical mix)
//   - Opus 4.x:   ~$22 / Mtok
//
// Local models (Hermes-served gemma, etc.) typically price near zero —
// effectively electricity-only. Operators set whatever they want for
// the comparison frame.
type Pricing struct {
	// USDPerMTok is the blended cost in USD per million tokens.
	USDPerMTok float64
}

// Cost returns the USD cost for the given token count.
func (p Pricing) Cost(tokens int) float64 {
	return float64(tokens) * p.USDPerMTok / 1_000_000
}

// PricingMap registers per-orchestrator pricing keyed by Orchestrator.Name().
// Lookup returns zero Pricing for unmapped names (i.e., unpriced = $0).
type PricingMap map[string]Pricing

// Cost returns USD for the given orchestrator+tokens, or 0 if the
// orchestrator isn't priced.
func (m PricingMap) Cost(orchestratorName string, tokens int) float64 {
	if p, ok := m[orchestratorName]; ok {
		return p.Cost(tokens)
	}
	return 0
}

// ParsePricingFlag parses a CLI value like "orchestrator-vote-3=1.30"
// into one PricingMap entry. Returns the orchestrator name and the
// rate as a Pricing.
func ParsePricingFlag(s string) (name string, p Pricing, err error) {
	for i, ch := range s {
		if ch == '=' {
			name = s[:i]
			var rate float64
			_, scanErr := fmt.Sscanf(s[i+1:], "%f", &rate)
			if scanErr != nil {
				return "", Pricing{}, fmt.Errorf("benchmark.ParsePricingFlag: parse rate from %q: %w", s, scanErr)
			}
			if name == "" {
				return "", Pricing{}, fmt.Errorf("benchmark.ParsePricingFlag: empty name in %q", s)
			}
			return name, Pricing{USDPerMTok: rate}, nil
		}
	}
	return "", Pricing{}, fmt.Errorf("benchmark.ParsePricingFlag: missing '=' in %q (want NAME=RATE)", s)
}
