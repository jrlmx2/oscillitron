// CLAUDE GENERATED
package cost

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
)

// Externalized pricing configuration, following the same Spring-Boot
// precedence as elsewhere: defaults < env < flags < programmatic.
//
// Pricing is per-model so flat env vars don't scale. The model table
// is loaded from a single JSON map (one env var, one flag); the
// frontier baseline (the counterfactual benchmark) is two simple
// floats.
//
// Example JSON:
//
//   {"qwen2.5-coder:7b": {"in": 0.0, "out": 0.0},
//    "claude-sonnet-4.6": {"in": 3.0, "out": 15.0}}
//
// Rates are USD per million tokens (matches how every provider
// publishes prices).

const (
	EnvFrontierInput  = "OSCILLITRON_COST_FRONTIER_INPUT_USD_PER_MTOK"
	EnvFrontierOutput = "OSCILLITRON_COST_FRONTIER_OUTPUT_USD_PER_MTOK"
	EnvModels         = "OSCILLITRON_COST_MODELS"
)

const (
	FlagFrontierInput  = "cost-frontier-input"
	FlagFrontierOutput = "cost-frontier-output"
	FlagModels         = "cost-models"
)

// PricingConfig is the externalizable shape: a frontier baseline plus
// a model -> Pricing table. NewTrackerFromConfig builds a Tracker
// from this.
type PricingConfig struct {
	Frontier Pricing
	Models   map[string]Pricing
}

// DefaultPricingConfig returns the in-code defaults. Frontier defaults
// to Claude Sonnet 4.6 list pricing ($3/$15 per MTok) — the
// counterfactual the Phase 1 GTM thesis is measured against. Override
// via env or flag for a different baseline.
func DefaultPricingConfig() PricingConfig {
	return PricingConfig{
		Frontier: Pricing{InputUSDPerMTok: 3.0, OutputUSDPerMTok: 15.0},
		Models:   map[string]Pricing{},
	}
}

// pricingJSON is the on-wire shape of one entry in the Models map.
// Shorter field names for terser JSON since users may set these via env.
type pricingJSON struct {
	In  float64 `json:"in"`
	Out float64 `json:"out"`
}

// ApplyEnv overlays env-var values onto pc. Empty vars are treated
// as unset.
func ApplyEnv(pc PricingConfig) (PricingConfig, error) {
	if v, ok := lookupEnv(EnvFrontierInput); ok {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return pc, fmt.Errorf("cost: %s=%q is not a float: %w", EnvFrontierInput, v, err)
		}
		pc.Frontier.InputUSDPerMTok = f
	}
	if v, ok := lookupEnv(EnvFrontierOutput); ok {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return pc, fmt.Errorf("cost: %s=%q is not a float: %w", EnvFrontierOutput, v, err)
		}
		pc.Frontier.OutputUSDPerMTok = f
	}
	if v, ok := lookupEnv(EnvModels); ok {
		models, err := parseModelsJSON(v)
		if err != nil {
			return pc, fmt.Errorf("cost: %s parse: %w", EnvModels, err)
		}
		// Env-supplied entries OVERLAY defaults (don't wipe), so callers
		// can preload pricing programmatically and add more via env.
		for k, p := range models {
			pc.Models[k] = p
		}
	}
	return pc, nil
}

// RegisterFlags binds PricingConfig fields to fs. Defaults come from base.
func RegisterFlags(fs *flag.FlagSet, base *PricingConfig) {
	fs.Float64Var(&base.Frontier.InputUSDPerMTok, FlagFrontierInput, base.Frontier.InputUSDPerMTok,
		"frontier baseline input price (USD per million tokens)")
	fs.Float64Var(&base.Frontier.OutputUSDPerMTok, FlagFrontierOutput, base.Frontier.OutputUSDPerMTok,
		"frontier baseline output price (USD per million tokens)")
	// Models is a map — represented as a JSON string flag.
	fs.Func(FlagModels,
		`per-model pricing as JSON, e.g. '{"name": {"in": 0.5, "out": 1.0}}'`,
		func(s string) error {
			if s == "" {
				return nil
			}
			m, err := parseModelsJSON(s)
			if err != nil {
				return fmt.Errorf("%s: %w", FlagModels, err)
			}
			if base.Models == nil {
				base.Models = map[string]Pricing{}
			}
			for k, p := range m {
				base.Models[k] = p
			}
			return nil
		})
}

// LoadPricingConfig is the one-shot loader: defaults → env → flags.
//
// FOOTGUN: do NOT call with args=nil intending to defer fs.Parse() to
// the caller — flag pointers bind to LoadPricingConfig's stack frame
// and become dangling on return. For shared-FlagSet patterns, the
// caller must own the PricingConfig value and call ApplyEnv +
// RegisterFlags directly. See cmd/oscillitron/main.go.
func LoadPricingConfig(fs *flag.FlagSet, args []string) (PricingConfig, error) {
	pc := DefaultPricingConfig()
	pc, err := ApplyEnv(pc)
	if err != nil {
		return pc, err
	}
	RegisterFlags(fs, &pc)
	if args != nil {
		if err := fs.Parse(args); err != nil {
			return pc, err
		}
	}
	return pc, nil
}

// NewTrackerFromConfig builds a Tracker from a fully-resolved
// PricingConfig — the typical end of LoadPricingConfig.
func NewTrackerFromConfig(pc PricingConfig) *Tracker {
	t := New(pc.Frontier)
	for name, p := range pc.Models {
		t.Register(name, p)
	}
	return t
}

func parseModelsJSON(s string) (map[string]Pricing, error) {
	if s == "" {
		return nil, nil
	}
	var raw map[string]pricingJSON
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, fmt.Errorf("expected JSON object map: %w", err)
	}
	if len(raw) == 0 {
		return nil, errors.New("empty model map")
	}
	out := make(map[string]Pricing, len(raw))
	for k, v := range raw {
		out[k] = Pricing{InputUSDPerMTok: v.In, OutputUSDPerMTok: v.Out}
	}
	return out, nil
}

func lookupEnv(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}
