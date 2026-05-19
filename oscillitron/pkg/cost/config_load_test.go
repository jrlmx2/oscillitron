// CLAUDE GENERATED
package cost

import (
	"flag"
	"testing"
)

func clearEnv(t *testing.T) {
	for _, k := range []string{EnvFrontierInput, EnvFrontierOutput, EnvModels} {
		t.Setenv(k, "")
	}
}

func TestDefaultPricingConfig(t *testing.T) {
	pc := DefaultPricingConfig()
	if pc.Frontier.InputUSDPerMTok != 3.0 || pc.Frontier.OutputUSDPerMTok != 15.0 {
		t.Errorf("frontier default = %+v, want $3 in / $15 out (Claude Sonnet 4.6)", pc.Frontier)
	}
	if len(pc.Models) != 0 {
		t.Errorf("Models default should be empty, got %v", pc.Models)
	}
}

func TestApplyEnv_Frontier(t *testing.T) {
	t.Setenv(EnvFrontierInput, "0.5")
	t.Setenv(EnvFrontierOutput, "2.0")
	pc, err := ApplyEnv(DefaultPricingConfig())
	if err != nil {
		t.Fatal(err)
	}
	if pc.Frontier.InputUSDPerMTok != 0.5 || pc.Frontier.OutputUSDPerMTok != 2.0 {
		t.Errorf("frontier = %+v, want $0.50 / $2.00", pc.Frontier)
	}
}

func TestApplyEnv_ModelsJSON(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvModels, `{"qwen-7b": {"in": 0.0, "out": 0.0}, "claude": {"in": 3.0, "out": 15.0}}`)
	pc, err := ApplyEnv(DefaultPricingConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(pc.Models) != 2 {
		t.Fatalf("Models = %v, want 2 entries", pc.Models)
	}
	if pc.Models["qwen-7b"].InputUSDPerMTok != 0 || pc.Models["claude"].OutputUSDPerMTok != 15.0 {
		t.Errorf("unmarshal lost data: %+v", pc.Models)
	}
}

func TestApplyEnv_BadJSON(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvModels, `not-json`)
	if _, err := ApplyEnv(DefaultPricingConfig()); err == nil {
		t.Error("expected error on bad JSON")
	}
}

func TestApplyEnv_BadFloat(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvFrontierInput, "not-a-number")
	if _, err := ApplyEnv(DefaultPricingConfig()); err == nil {
		t.Error("expected error on non-float frontier input")
	}
}

func TestLoadPricingConfig_FlagOverridesEnv(t *testing.T) {
	t.Setenv(EnvFrontierInput, "1.0")
	t.Setenv(EnvModels, `{"foo": {"in": 0.1, "out": 0.2}}`)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	pc, err := LoadPricingConfig(fs, []string{
		"--" + FlagFrontierInput, "5.0",
		"--" + FlagModels, `{"bar": {"in": 0.5, "out": 1.0}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pc.Frontier.InputUSDPerMTok != 5.0 {
		t.Errorf("Frontier.Input = %v, want 5.0 (flag should beat env)", pc.Frontier.InputUSDPerMTok)
	}
	// Both env-loaded and flag-loaded entries should be present (flag overlays).
	if _, ok := pc.Models["foo"]; !ok {
		t.Errorf("env-loaded model 'foo' missing: %v", pc.Models)
	}
	if _, ok := pc.Models["bar"]; !ok {
		t.Errorf("flag-loaded model 'bar' missing: %v", pc.Models)
	}
}

func TestNewTrackerFromConfig_RegistersAll(t *testing.T) {
	pc := PricingConfig{
		Frontier: Pricing{InputUSDPerMTok: 3.0, OutputUSDPerMTok: 15.0},
		Models: map[string]Pricing{
			"local": {InputUSDPerMTok: 0, OutputUSDPerMTok: 0},
		},
	}
	tr := NewTrackerFromConfig(pc)
	entry := tr.Record("local", 1_000_000, 500_000)
	if entry.ActualUSD != 0 {
		t.Errorf("ActualUSD = %v, want 0 ('local' registered at zero)", entry.ActualUSD)
	}
	// Frontier should be 1M * 3 + 500K * 15 = 3 + 7.5 = $10.50.
	if entry.FrontierUSD < 10.49 || entry.FrontierUSD > 10.51 {
		t.Errorf("FrontierUSD = %v, want ~10.50", entry.FrontierUSD)
	}
}
