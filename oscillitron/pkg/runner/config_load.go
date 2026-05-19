// CLAUDE GENERATED
package runner

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"
)

// Externalized configuration for the runner's tunable knobs, following
// the same Spring-Boot-style precedence used elsewhere in the project:
//
//   1. Code defaults             (DefaultTunables)
//   2. Environment variables     (ApplyEnv)
//   3. Command-line flags        (RegisterFlags + fs.Parse)
//   4. Programmatic overrides    (caller mutates the returned Config)
//
// Most of runner.Config is programmatic plumbing (Topology, Router,
// Oscillators, Inhibitor, Initial). Only the *tunable* primitives are
// externalizable. Callers build the full Config in code, then merge
// the loaded Tunables via Apply.

const (
	EnvBufferSize   = "OSCILLITRON_RUNNER_BUFFER_SIZE"
	EnvChainTimeout = "OSCILLITRON_RUNNER_CHAIN_TIMEOUT"
)

const (
	FlagBufferSize   = "runner-buffer-size"
	FlagChainTimeout = "runner-chain-timeout"
)

// Tunables is the externalizable subset of Config. Defaults match
// what the runner uses when these fields are zero.
type Tunables struct {
	BufferSize   int
	ChainTimeout time.Duration
}

// DefaultTunables returns the in-code defaults.
func DefaultTunables() Tunables {
	return Tunables{
		BufferSize:   8,
		ChainTimeout: 0, // zero = no timeout wrap; caller's ctx is authoritative
	}
}

// ApplyEnv overlays env-var values onto t. Unset (or empty) env vars
// leave the corresponding field unchanged.
func ApplyEnv(t Tunables) (Tunables, error) {
	if v, ok := lookupEnv(EnvBufferSize); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return t, fmt.Errorf("runner: %s=%q is not an integer: %w", EnvBufferSize, v, err)
		}
		t.BufferSize = n
	}
	if v, ok := lookupEnv(EnvChainTimeout); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return t, fmt.Errorf("runner: %s=%q is not a Go duration (e.g. \"30s\", \"5m\"): %w", EnvChainTimeout, v, err)
		}
		t.ChainTimeout = d
	}
	return t, nil
}

// RegisterFlags binds Tunables fields to fs. Defaults come from base,
// so calling RegisterFlags after ApplyEnv (the LoadTunables pattern)
// makes env-supplied values the visible flag defaults — explicit flag
// arguments then override them.
func RegisterFlags(fs *flag.FlagSet, base *Tunables) {
	fs.IntVar(&base.BufferSize, FlagBufferSize, base.BufferSize,
		"channel buffer size per oscillator")
	fs.DurationVar(&base.ChainTimeout, FlagChainTimeout, base.ChainTimeout,
		"max chain duration as a Go duration (e.g. \"30s\", \"5m\"); 0 disables")
}

// LoadTunables is the one-shot loader: defaults → env → flags.
// Parses args against fs.
//
// FOOTGUN: do NOT call with args=nil intending to defer fs.Parse() to
// the caller — flag pointers bind to LoadTunables' stack frame and
// become dangling on return. For shared-FlagSet patterns, the caller
// must own the Tunables value and call ApplyEnv + RegisterFlags
// directly. See cmd/oscillitron/main.go for the idiomatic flow.
func LoadTunables(fs *flag.FlagSet, args []string) (Tunables, error) {
	t := DefaultTunables()
	t, err := ApplyEnv(t)
	if err != nil {
		return t, err
	}
	RegisterFlags(fs, &t)
	if args != nil {
		if err := fs.Parse(args); err != nil {
			return t, err
		}
	}
	return t, nil
}

// Apply merges Tunables into a Config, setting only the externalized
// fields. Caller-provided values on the Config are overwritten — so
// LoadTunables → Apply is the normal flow.
func (t Tunables) Apply(cfg Config) Config {
	cfg.BufferSize = t.BufferSize
	cfg.ChainTimeout = t.ChainTimeout
	return cfg
}

func lookupEnv(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}
