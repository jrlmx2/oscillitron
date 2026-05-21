// CLAUDE GENERATED
package vram

import (
	"context"
	"errors"
	"testing"
)

// fakeProbe is a Probe controlled by tests.
type fakeProbe struct {
	name   string
	report Report
	err    error
}

func (f fakeProbe) Name() string { return f.name }
func (f fakeProbe) Probe(_ context.Context) (Report, error) {
	if f.err != nil {
		return Report{}, f.err
	}
	return f.report, nil
}

func TestAutoChain_FirstSuccessWins(t *testing.T) {
	want := Report{Source: "winner", AvailableBytes: 99}
	chain := AutoChain{Probes: []Probe{
		fakeProbe{name: "fails-1", err: errors.New("nope")},
		fakeProbe{name: "wins", report: want},
		fakeProbe{name: "should-not-run", err: errors.New("unreachable")},
	}}
	got, err := chain.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if got.AvailableBytes != want.AvailableBytes || got.Source != want.Source {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestAutoChain_NoneSucceedsReturnsErrNoSource(t *testing.T) {
	chain := AutoChain{Probes: []Probe{
		fakeProbe{name: "a", err: errors.New("a")},
		fakeProbe{name: "b", err: errors.New("b")},
	}}
	_, err := chain.Probe(context.Background())
	if !errors.Is(err, ErrNoSource) {
		t.Errorf("got %v, want wraps ErrNoSource", err)
	}
}

func TestAutoChain_EmptyReturnsErrNoSource(t *testing.T) {
	_, err := AutoChain{}.Probe(context.Background())
	if !errors.Is(err, ErrNoSource) {
		t.Errorf("got %v, want wraps ErrNoSource", err)
	}
}

func TestAutoChain_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	chain := AutoChain{Probes: []Probe{fakeProbe{name: "x"}}}
	_, err := chain.Probe(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

func TestOverride_AlwaysWins(t *testing.T) {
	SetOverride(0)
	defer SetOverride(0)
	SetOverride(8 * 1024 * 1024 * 1024) // 8 GiB
	probe := Auto()
	report, err := probe.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if report.Source != SourceOverride {
		t.Errorf("Source = %q, want override", report.Source)
	}
	if report.AvailableBytes != 8*1024*1024*1024 {
		t.Errorf("AvailableBytes = %d, want 8 GiB", report.AvailableBytes)
	}
}

func TestOverride_ZeroClearsIt(t *testing.T) {
	SetOverride(1024)
	SetOverride(0)
	probe := Auto()
	// Now Auto() should not include the override probe — the real
	// platform probes get a chance. Depending on the build environment
	// some of them may succeed; we only assert the source is NOT
	// "override".
	report, err := probe.Probe(context.Background())
	if err == nil && report.Source == SourceOverride {
		t.Errorf("override should be cleared; got source=%q", report.Source)
	}
}
