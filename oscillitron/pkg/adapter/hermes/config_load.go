// CLAUDE GENERATED
package hermes

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Externalized configuration follows Spring-Boot-style precedence,
// lowest to highest:
//
//   1. Code defaults             (DefaultConfig)
//   2. Environment variables     (ApplyEnv)
//   3. Command-line flags        (RegisterFlags + fs.Parse)
//   4. Programmatic overrides    (caller mutates the returned Config)
//
// Use LoadConfig for the common case (env + flags + return). For
// finer control, call DefaultConfig / ApplyEnv / RegisterFlags
// individually.

// Environment-variable names. Keep these as constants so callers
// (and the setup scripts) can reference them by symbol.
const (
	EnvName                 = "OSCILLITRON_HERMES_NAME"
	EnvBinPath              = "OSCILLITRON_HERMES_BIN"
	EnvCwd                  = "OSCILLITRON_HERMES_CWD"
	EnvArgs                 = "OSCILLITRON_HERMES_ARGS"
	EnvMaxContextTokens     = "OSCILLITRON_HERMES_MAX_CONTEXT_TOKENS"
	EnvMinConnectionTimeout = "OSCILLITRON_HERMES_MIN_CONNECTION_TIMEOUT"
	EnvMinCallTimeout       = "OSCILLITRON_HERMES_MIN_CALL_TIMEOUT"
)

// Flag names registered by RegisterFlags. Exposed so external test
// harnesses can assert against them.
const (
	FlagName                 = "hermes-name"
	FlagBinPath              = "hermes-bin"
	FlagCwd                  = "hermes-cwd"
	FlagArgs                 = "hermes-args"
	FlagMaxContextTokens     = "hermes-max-context-tokens"
	FlagMinConnectionTimeout = "hermes-min-connection-timeout"
	FlagMinCallTimeout       = "hermes-min-call-timeout"
)

// DefaultConfig returns the in-code defaults. BinPath and Cwd are
// intentionally empty — they have no useful default, the caller must
// supply via env / flag / code. MaxContextTokens default of 48000 is
// derived from Hermes' 64K floor minus a 25% margin for its own
// system-prompt + scaffolding.
func DefaultConfig() Config {
	return Config{
		Name:                 "code",
		MaxContextTokens:     48_000,
		MinConnectionTimeout: DefaultMinConnectionTimeout,
		MinCallTimeout:       DefaultMinCallTimeout,
	}
}

// ApplyEnv overlays environment-variable values onto cfg. Unset (or
// empty) env vars leave the corresponding field unchanged. Returns
// the modified Config and a parse error if any numeric field is
// malformed.
func ApplyEnv(cfg Config) (Config, error) {
	if v, ok := lookup(EnvName); ok {
		cfg.Name = v
	}
	if v, ok := lookup(EnvBinPath); ok {
		cfg.BinPath = v
	}
	if v, ok := lookup(EnvCwd); ok {
		cfg.Cwd = v
	}
	if v, ok := lookup(EnvArgs); ok {
		cfg.Args = splitArgs(v)
	}
	if v, ok := lookup(EnvMaxContextTokens); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("hermes: %s=%q is not an integer: %w", EnvMaxContextTokens, v, err)
		}
		cfg.MaxContextTokens = n
	}
	if v, ok := lookup(EnvMinConnectionTimeout); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("hermes: %s=%q is not a Go duration (e.g. \"30s\", \"2m\"): %w", EnvMinConnectionTimeout, v, err)
		}
		cfg.MinConnectionTimeout = d
	}
	if v, ok := lookup(EnvMinCallTimeout); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("hermes: %s=%q is not a Go duration (e.g. \"30s\", \"2m\"): %w", EnvMinCallTimeout, v, err)
		}
		cfg.MinCallTimeout = d
	}
	return cfg, nil
}

// RegisterFlags binds Config fields to fs. The flag defaults are
// pulled from base — so calling RegisterFlags after ApplyEnv (the
// LoadConfig pattern) means env-supplied values become the visible
// flag defaults, and explicit flag arguments override them. The
// resulting Config is written back through the pointer when
// fs.Parse() runs.
func RegisterFlags(fs *flag.FlagSet, base *Config) {
	fs.StringVar(&base.Name, FlagName, base.Name,
		"oscillator name and Adapter.Name() value")
	fs.StringVar(&base.BinPath, FlagBinPath, base.BinPath,
		"path to the Hermes ACP-server executable")
	fs.StringVar(&base.Cwd, FlagCwd, base.Cwd,
		"working directory passed to the Hermes process (must be absolute)")
	// Args is a slice — represented as a single space-separated string
	// on the CLI to match how shells typically pass them. Joined via
	// shell-style splitArgs (no quoting; arguments containing spaces
	// must be supplied via code).
	var argsStr string
	if len(base.Args) > 0 {
		argsStr = strings.Join(base.Args, " ")
	}
	fs.Func(FlagArgs, "extra args to the Hermes binary (space-separated)",
		func(s string) error {
			base.Args = splitArgs(s)
			return nil
		})
	_ = argsStr // populated as default via Func; here for readability
	fs.IntVar(&base.MaxContextTokens, FlagMaxContextTokens, base.MaxContextTokens,
		"hard ceiling on prompt tokens (chars/4 estimate); 0 disables")
	fs.DurationVar(&base.MinConnectionTimeout, FlagMinConnectionTimeout, base.MinConnectionTimeout,
		"minimum runway required on the caller's ctx for the New() handshake; 0=default, negative=disable")
	fs.DurationVar(&base.MinCallTimeout, FlagMinCallTimeout, base.MinCallTimeout,
		"minimum runway required on the caller's ctx for each Call; 0=default, negative=disable")
}

// LoadConfig is the one-shot loader: defaults → env → flags. Parses
// args against fs. The caller can then mutate the returned Config
// further (highest-precedence programmatic overrides) before passing
// it to New.
//
// Pass flag.CommandLine and os.Args[1:] for the standard wiring; use
// a dedicated FlagSet for library/test cases.
//
// FOOTGUN: do NOT call LoadConfig with args=nil intending to defer
// fs.Parse() to the caller — the flag pointers bind into LoadConfig's
// stack frame, which is invalid by the time the caller parses. For a
// shared-FlagSet pattern (multiple packages registering flags into
// one FlagSet), the caller must own the Config and call ApplyEnv +
// RegisterFlags directly. See cmd/oscillitron/main.go for the
// idiomatic pattern.
func LoadConfig(fs *flag.FlagSet, args []string) (Config, error) {
	cfg := DefaultConfig()
	cfg, err := ApplyEnv(cfg)
	if err != nil {
		return cfg, err
	}
	RegisterFlags(fs, &cfg)
	if args != nil {
		if err := fs.Parse(args); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

// lookup returns the env var's value only if it's set AND non-empty.
// An empty env var is treated as unset so a stray `OSCILLITRON_HERMES_BIN=`
// in the environment doesn't wipe out the default or a flag value.
func lookup(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// splitArgs is a minimal space-splitter for the Args field. Doesn't
// handle shell quoting — callers needing args with embedded spaces
// must set Args programmatically (Config.Args = []string{...}).
func splitArgs(s string) []string {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return nil
	}
	return parts
}
