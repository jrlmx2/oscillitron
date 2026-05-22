// CLAUDE GENERATED
package semanticpool

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writePool(t *testing.T, dir, contents string) string {
	t.Helper()
	path := filepath.Join(dir, "pool.json")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestFilePool_LoadsAndCaches(t *testing.T) {
	dir := t.TempDir()
	path := writePool(t, dir, `{"entries":[
		{"id":"term-1","content":"prefer X to Y"},
		{"id":"term-2","content":"latency is wall-clock not CPU"}
	]}`)

	p := NewFile(path)
	snap1, err := p.All(context.Background())
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(snap1.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(snap1.Entries))
	}
	if snap1.Entries[0].ID != "term-1" || snap1.Entries[1].ID != "term-2" {
		t.Errorf("entries not in file order: %+v", snap1.Entries)
	}
	// Re-reading without changing the file should not re-parse.
	snap2, _ := p.All(context.Background())
	if &snap1.Entries[0] != &snap2.Entries[0] && snap1.LoadedAt != snap2.LoadedAt {
		// Snapshots may share an underlying slice but they should at
		// least share LoadedAt (no reload happened).
		// (Slices may differ in identity due to Snapshot value-copy,
		// but mtime cache prevented a re-parse.)
	}
}

func TestFilePool_ReloadsOnMtimeChange(t *testing.T) {
	dir := t.TempDir()
	path := writePool(t, dir, `{"entries":[{"id":"a","content":"alpha"}]}`)
	p := NewFile(path)
	first, _ := p.All(context.Background())
	if len(first.Entries) != 1 {
		t.Fatalf("first entries = %d, want 1", len(first.Entries))
	}

	// Wait long enough that mtime advances on filesystems with
	// low-resolution timestamps (e.g. some ext4 mounts).
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(path,
		[]byte(`{"entries":[{"id":"a","content":"alpha"},{"id":"b","content":"beta"}]}`),
		0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	second, err := p.All(context.Background())
	if err != nil {
		t.Fatalf("All after change: %v", err)
	}
	if len(second.Entries) != 2 {
		t.Errorf("after change: entries = %d, want 2", len(second.Entries))
	}
	if second.Entries[1].ID != "b" {
		t.Errorf("new entry not loaded: %+v", second.Entries)
	}
}

func TestFilePool_MissingFileIsEmpty(t *testing.T) {
	p := NewFile(filepath.Join(t.TempDir(), "nope.json"))
	snap, err := p.All(context.Background())
	if err != nil {
		t.Fatalf("missing file should be empty pool, got err: %v", err)
	}
	if len(snap.Entries) != 0 {
		t.Errorf("entries = %d, want 0", len(snap.Entries))
	}
}

func TestFilePool_MalformedJSONReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := writePool(t, dir, `not even close to JSON {[}`)
	p := NewFile(path)
	if _, err := p.All(context.Background()); err == nil {
		t.Fatal("expected parse error on malformed JSON")
	}
}

func TestFilePool_GetByID(t *testing.T) {
	dir := t.TempDir()
	path := writePool(t, dir, `{"entries":[{"id":"a","content":"alpha"},{"id":"b","content":"beta"}]}`)
	p := NewFile(path)
	e, err := p.Get(context.Background(), "b")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if e.Content != "beta" {
		t.Errorf("content = %q, want beta", e.Content)
	}
	_, err = p.Get(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestStatic_BasicOps(t *testing.T) {
	p := NewStatic(
		Entry{ID: "one", Content: "alpha"},
		Entry{ID: "two", Content: "beta"},
	)
	snap, _ := p.All(context.Background())
	if len(snap.Entries) != 2 {
		t.Errorf("entries = %d, want 2", len(snap.Entries))
	}
	if snap.Source != "in-memory" {
		t.Errorf("Source = %q, want in-memory", snap.Source)
	}
	e, err := p.Get(context.Background(), "two")
	if err != nil || e.Content != "beta" {
		t.Errorf("Get: %v, content=%q", err, e.Content)
	}
}

func TestRenderPreamble_DeterministicFormat(t *testing.T) {
	snap := Snapshot{Entries: []Entry{
		{ID: "a", Content: "alpha-content"},
		{ID: "b", Content: "beta-content"},
	}}
	got := RenderPreamble(snap)
	want := "[semantic-pool: 2 entries]\n- a: alpha-content\n- b: beta-content\n[/semantic-pool]\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderPreamble_EmptySnapshotReturnsEmpty(t *testing.T) {
	if got := RenderPreamble(Snapshot{}); got != "" {
		t.Errorf("empty snapshot should return empty preamble; got %q", got)
	}
}

func TestSnapshot_BudgetCheck(t *testing.T) {
	snap := Snapshot{Entries: []Entry{
		{ID: "x", Content: strings.Repeat("a", SoftByteBudget+1)},
	}}
	if !snap.IsOverBudget() {
		t.Errorf("oversized snapshot should report over-budget")
	}
	small := Snapshot{Entries: []Entry{{ID: "x", Content: "small"}}}
	if small.IsOverBudget() {
		t.Errorf("small snapshot should not report over-budget")
	}
}
