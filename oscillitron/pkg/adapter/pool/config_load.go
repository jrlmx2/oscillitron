// CLAUDE GENERATED
package pool

import (
	"flag"
	"fmt"
	"os"
	"strconv"
)

// Externalized pool configuration. Same Spring-Boot precedence used
// across the project: defaults < env < flags < programmatic (via
// Adapter.SetParallel).

const EnvParallel = "OSCILLITRON_POOL_PARALLEL"
const FlagParallel = "pool-parallel"

// PoolConfig is the externalizable shape. Currently a single switch.
// Kept as a struct so adding future knobs (max-concurrency cap,
// per-backend weights) doesn't break callers.
type PoolConfig struct {
	Parallel bool
}

// DefaultPoolConfig returns the in-code defaults.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{Parallel: ParallelDefault}
}

// ApplyEnv overlays env-var values onto pc. Empty vars are treated
// as unset (so a stray empty OSCILLITRON_POOL_PARALLEL= doesn't flip
// the default). Accepts the usual truthy/falsy strings: "true",
// "false", "1", "0", "yes", "no", "on", "off" (case-insensitive).
func ApplyEnv(pc PoolConfig) (PoolConfig, error) {
	if v, ok := lookupEnv(EnvParallel); ok {
		b, err := parseBool(v)
		if err != nil {
			return pc, fmt.Errorf("pool: %s=%q: %w", EnvParallel, v, err)
		}
		pc.Parallel = b
	}
	return pc, nil
}

// RegisterFlags binds PoolConfig fields to fs.
func RegisterFlags(fs *flag.FlagSet, base *PoolConfig) {
	fs.BoolVar(&base.Parallel, FlagParallel, base.Parallel,
		"enable parallel dispatch across backing adapters; false pins all Calls to backends[0]")
}

// LoadPoolConfig is the one-shot loader: defaults → env → flags.
//
// FOOTGUN: do NOT call with args=nil intending to defer fs.Parse() to
// the caller — flag pointers dangle on return. For shared-FlagSet
// patterns, own the PoolConfig locally and call ApplyEnv +
// RegisterFlags directly. See cmd/oscillitron/main.go.
func LoadPoolConfig(fs *flag.FlagSet, args []string) (PoolConfig, error) {
	pc := DefaultPoolConfig()
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

// Apply sets the Adapter's parallel switch from a resolved PoolConfig.
// Use at startup once all externalized values are merged.
func (pc PoolConfig) Apply(a *Adapter) { a.SetParallel(pc.Parallel) }

func parseBool(s string) (bool, error) {
	// strconv.ParseBool accepts 1/0/t/f/T/F/true/false/TRUE/FALSE/True/False
	// but not yes/no/on/off, which users reach for. Cover both.
	if b, err := strconv.ParseBool(s); err == nil {
		return b, nil
	}
	switch s {
	case "yes", "YES", "Yes", "on", "ON", "On":
		return true, nil
	case "no", "NO", "No", "off", "OFF", "Off":
		return false, nil
	}
	return false, fmt.Errorf("expected true/false/yes/no/on/off/1/0, got %q", s)
}

func lookupEnv(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}
