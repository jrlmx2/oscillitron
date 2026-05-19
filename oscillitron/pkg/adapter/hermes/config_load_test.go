// CLAUDE GENERATED
package hermes

import (
	"flag"
	"reflect"
	"testing"
)

// withEnv sets env vars for the duration of the test, restoring them
// afterward. t.Setenv is the modern stdlib way and handles teardown.
func withEnv(t *testing.T, kvs map[string]string) {
	for k, v := range kvs {
		t.Setenv(k, v)
	}
}

func TestDefaultConfig_Defaults(t *testing.T) {
	c := DefaultConfig()
	if c.Name != "code" {
		t.Errorf("Name = %q, want %q", c.Name, "code")
	}
	if c.MaxContextTokens != 48_000 {
		t.Errorf("MaxContextTokens = %d, want 48000", c.MaxContextTokens)
	}
	if c.BinPath != "" || c.Cwd != "" || len(c.Args) != 0 {
		t.Errorf("non-default fields should be zero-valued: %+v", c)
	}
}

func TestApplyEnv_OverridesDefaults(t *testing.T) {
	withEnv(t, map[string]string{
		EnvName:             "writer",
		EnvBinPath:          "/usr/local/bin/hermes-acp",
		EnvCwd:              "/work",
		EnvArgs:             "--accept-hooks --foo",
		EnvMaxContextTokens: "120000",
	})
	c, err := ApplyEnv(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	// Start from defaults so fields not set in env (MinConnectionTimeout,
	// MinCallTimeout) keep their default values rather than zeroes.
	want := DefaultConfig()
	want.Name = "writer"
	want.BinPath = "/usr/local/bin/hermes-acp"
	want.Cwd = "/work"
	want.Args = []string{"--accept-hooks", "--foo"}
	want.MaxContextTokens = 120000
	if !reflect.DeepEqual(c, want) {
		t.Errorf("ApplyEnv:\n  got  %+v\n  want %+v", c, want)
	}
}

func TestApplyEnv_EmptyVarTreatedAsUnset(t *testing.T) {
	// An empty env var shouldn't wipe out the default; treat as unset.
	withEnv(t, map[string]string{EnvName: ""})
	c, err := ApplyEnv(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "code" {
		t.Errorf("empty env wiped default: Name=%q", c.Name)
	}
}

func TestApplyEnv_BadInt(t *testing.T) {
	withEnv(t, map[string]string{EnvMaxContextTokens: "not-a-number"})
	if _, err := ApplyEnv(DefaultConfig()); err == nil {
		t.Fatal("expected parse error on non-integer MaxContextTokens")
	}
}

func TestLoadConfig_PrecedenceOrder(t *testing.T) {
	// Env sets name=fromenv and max=11111. Flag overrides name=fromflag.
	// MaxContextTokens should remain from env (no flag passed).
	withEnv(t, map[string]string{
		EnvName:             "fromenv",
		EnvMaxContextTokens: "11111",
	})
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	c, err := LoadConfig(fs, []string{
		"--" + FlagName, "fromflag",
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "fromflag" {
		t.Errorf("Name precedence wrong: got %q, want %q (flag should beat env)", c.Name, "fromflag")
	}
	if c.MaxContextTokens != 11111 {
		t.Errorf("MaxContextTokens precedence wrong: got %d, want 11111 (env should beat default)", c.MaxContextTokens)
	}
}

func TestLoadConfig_NoEnvNoFlag_KeepsDefaults(t *testing.T) {
	// Explicitly clear the env vars in case the host has them set.
	for _, k := range []string{EnvName, EnvBinPath, EnvCwd, EnvArgs, EnvMaxContextTokens} {
		t.Setenv(k, "")
	}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	c, err := LoadConfig(fs, []string{})
	if err != nil {
		t.Fatal(err)
	}
	want := DefaultConfig()
	if !reflect.DeepEqual(c, want) {
		t.Errorf("got %+v, want defaults %+v", c, want)
	}
}

func TestLoadConfig_ArgsFromFlag(t *testing.T) {
	for _, k := range []string{EnvName, EnvBinPath, EnvCwd, EnvArgs, EnvMaxContextTokens} {
		t.Setenv(k, "")
	}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	c, err := LoadConfig(fs, []string{
		"--" + FlagArgs, "--accept-hooks --version",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--accept-hooks", "--version"}
	if !reflect.DeepEqual(c.Args, want) {
		t.Errorf("Args = %v, want %v", c.Args, want)
	}
}

func TestLoadConfig_ProgrammaticOverrideWins(t *testing.T) {
	// The caller can always mutate the returned Config — there's no
	// special API needed; this test is here as the contract documentation.
	withEnv(t, map[string]string{EnvName: "fromenv"})
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	c, err := LoadConfig(fs, []string{"--" + FlagName, "fromflag"})
	if err != nil {
		t.Fatal(err)
	}
	c.Name = "programmatic" // highest precedence
	if c.Name != "programmatic" {
		t.Errorf("programmatic override should beat flag: got %q", c.Name)
	}
}
