// CLAUDE GENERATED
package config

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestParseBasic(t *testing.T) {
	in := `
# comment line
! also a comment
hermes.url=http://localhost:8642
hermes.model = openrouter:openai/gpt-4o-mini

timeout=2m
trailing.equal=a=b=c
empty.key=
no.equals.line
`
	p, err := parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.String("hermes.url", "") != "http://localhost:8642" {
		t.Errorf("hermes.url = %q", p["hermes.url"])
	}
	if p.String("hermes.model", "") != "openrouter:openai/gpt-4o-mini" {
		t.Errorf("hermes.model = %q", p["hermes.model"])
	}
	if got := p.Duration("timeout", 0); got != 2*time.Minute {
		t.Errorf("timeout = %v, want 2m", got)
	}
	if p["trailing.equal"] != "a=b=c" {
		t.Errorf("first '=' should split: got %q", p["trailing.equal"])
	}
	if _, ok := p["empty.key"]; !ok {
		t.Error("empty value should still register the key")
	}
	if _, ok := p["no.equals.line"]; !ok {
		t.Error("line without '=' should register as empty value")
	}
}

func TestStringDefault(t *testing.T) {
	p := Properties{"present": "v"}
	if p.String("present", "x") != "v" {
		t.Error("present key should win over default")
	}
	if p.String("absent", "x") != "x" {
		t.Error("absent key should use default")
	}
	if p.String("blank", "x") != "x" {
		t.Error("present-but-blank value should use default")
	}
}

func TestDurationParseFailureReturnsDefault(t *testing.T) {
	p := Properties{"t": "not a duration"}
	if got := p.Duration("t", time.Second); got != time.Second {
		t.Errorf("parse failure should yield default, got %v", got)
	}
}

func TestBoolParsesCommonForms(t *testing.T) {
	cases := map[string]bool{
		"true": true, "TRUE": true, "1": true, "yes": true, "on": true,
		"false": false, "0": false, "no": false, "off": false,
	}
	for v, want := range cases {
		p := Properties{"b": v}
		if got := p.Bool("b", !want); got != want {
			t.Errorf("Bool(%q) = %v, want %v", v, got, want)
		}
	}
	// Unrecognized → default.
	p := Properties{"b": "maybe"}
	if got := p.Bool("b", true); got != true {
		t.Errorf("unrecognized value should yield default, got %v", got)
	}
}

func TestIntParseFailureReturnsDefault(t *testing.T) {
	p := Properties{"n": "abc"}
	if got := p.Int("n", 42); got != 42 {
		t.Errorf("parse failure should yield default, got %d", got)
	}
}

func TestPrefixedKeysAndSubset(t *testing.T) {
	p := Properties{
		"hermes.url":                       "u",
		"hermes.endpoints.reasoning.url":   "r-u",
		"hermes.endpoints.reasoning.model": "r-m",
		"hermes.endpoints.critic.url":      "c-u",
		"other":                            "z",
	}
	keys := p.PrefixedKeys("hermes.endpoints")
	sort.Strings(keys)
	want := []string{"critic.url", "reasoning.model", "reasoning.url"}
	if !equalSlice(keys, want) {
		t.Errorf("PrefixedKeys = %v, want %v", keys, want)
	}

	sub := p.Subset("hermes.endpoints")
	if sub["critic.url"] != "c-u" || sub["reasoning.model"] != "r-m" {
		t.Errorf("Subset missing entries: %+v", sub)
	}
	if _, has := sub["hermes.url"]; has {
		t.Error("Subset should not include non-prefixed keys")
	}
}

func TestLoadIfExistsAbsentIsEmpty(t *testing.T) {
	p, err := LoadIfExists(filepath.Join(t.TempDir(), "nope.properties"))
	if err != nil {
		t.Fatalf("absent file should not error: %v", err)
	}
	if len(p) != 0 {
		t.Errorf("absent should yield empty bag, got %+v", p)
	}
}

func TestLoadReadsRealFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.properties")
	if err := os.WriteFile(path, []byte("k=v\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p["k"] != "v" {
		t.Errorf("k = %q", p["k"])
	}
}

func TestLoadMissingFileErrors(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.properties")); err == nil {
		t.Error("Load on missing file should error (use LoadIfExists for tolerant mode)")
	}
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
