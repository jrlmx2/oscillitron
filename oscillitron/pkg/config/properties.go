// CLAUDE GENERATED
// Package config provides a tiny .properties-style configuration
// loader. Inspired by Spring Boot's application.properties but
// deliberately a fraction of the surface — no profiles, no relaxed
// binding, no @ConfigurationProperties, no SpEL. Just key=value
// lines with dotted keys for hierarchy.
//
// Grammar:
//
//   - One key=value per line.
//   - Lines starting with '#' or '!' are comments (Java .properties
//     convention).
//   - Blank lines are ignored.
//   - Leading and trailing whitespace are stripped from both key and
//     value.
//   - Duplicate keys: last write wins.
//   - No escape sequences, no line continuations, no multi-line
//     values. If you need a literal '=' in a value, the first '=' on
//     a line is the separator and the rest is value.
//
// Typed accessors return the supplied default when a key is absent;
// they also return the default (silently) on parse errors of
// well-formed values — by design, since config files should not
// crash the program. Use String + manual parsing if you want strict
// validation.
package config

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// Properties is a flat key/value bag loaded from a .properties file.
// The zero value is a usable empty bag.
type Properties map[string]string

// Load reads a properties file from disk. A non-existent path is an
// error; an empty file returns an empty Properties.
func Load(path string) (Properties, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parse(f)
}

// LoadIfExists is the convenience for "use the file when it's there,
// otherwise return an empty bag." A missing file is not an error;
// other I/O or parse errors still propagate.
func LoadIfExists(path string) (Properties, error) {
	p, err := Load(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Properties{}, nil
		}
		return nil, err
	}
	return p, nil
}

func parse(r io.Reader) (Properties, error) {
	out := Properties{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			// Tolerant: lines without '=' are treated as keys mapped to
			// the empty string. Java does the same.
			out[strings.TrimSpace(line)] = ""
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if key == "" {
			continue
		}
		out[key] = val
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// String returns the value for key, or def when the key is absent
// or empty.
func (p Properties) String(key, def string) string {
	v, ok := p[key]
	if !ok || v == "" {
		return def
	}
	return v
}

// Duration parses the value as a time.Duration (Go syntax: "5m",
// "200ms"). Returns def when absent, empty, or unparseable.
func (p Properties) Duration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(p[key])
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

// Bool parses the value as true/false/1/0/yes/no (case-insensitive).
// Returns def when absent or unrecognized.
func (p Properties) Bool(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(p[key]))
	switch v {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return def
	}
}

// Int parses the value as a base-10 integer. Returns def when
// absent or unparseable.
func (p Properties) Int(key string, def int) int {
	v := strings.TrimSpace(p[key])
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// PrefixedKeys returns every key under the given prefix, with the
// prefix stripped. Useful for iterating dotted-key sub-trees like
// "hermes.endpoints.<bf>.url". Sub-keys may still contain dots.
func (p Properties) PrefixedKeys(prefix string) []string {
	if prefix == "" {
		out := make([]string, 0, len(p))
		for k := range p {
			out = append(out, k)
		}
		return out
	}
	if !strings.HasSuffix(prefix, ".") {
		prefix += "."
	}
	var out []string
	for k := range p {
		if strings.HasPrefix(k, prefix) {
			out = append(out, strings.TrimPrefix(k, prefix))
		}
	}
	return out
}

// Subset returns the subset of keys under prefix, with the prefix
// stripped. Returned bag is independent of the receiver.
func (p Properties) Subset(prefix string) Properties {
	if !strings.HasSuffix(prefix, ".") {
		prefix += "."
	}
	out := Properties{}
	for k, v := range p {
		if strings.HasPrefix(k, prefix) {
			out[strings.TrimPrefix(k, prefix)] = v
		}
	}
	return out
}
