// CLAUDE GENERATED
// Package semanticpool implements the shared semantic pool from the
// parent CLAUDE.md "Per-instance vs. shared resources" lock
// (2026-05-18):
//
//	"Retrieval — per-instance episodic, shared semantic (like
//	hippocampal vs. semantic memory). ... A small shared semantic pool
//	— stable general knowledge, cross-node facts, agreed terminology —
//	is readable by all. Writes to the shared pool are gated by the
//	curation layer; nodes don't pollute the commons unilaterally."
//
// v0 design (locked 2026-05-21):
//
//   - Read shape: whole pool injected on every adapter call as a
//     stable preamble (cache-friendly; first call pays the prompt
//     cost, later calls in the session reuse the KV cache).
//   - Write shape: operator-curated JSON file. No programmatic writes
//     in v0; the curation layer is its own component.
//   - Reload: mtime-based. The Pool re-reads the file when its mtime
//     advances. Cheap, no goroutines, no file watchers.
//
// Soft size cap: 2000 tokens (~8000 bytes at ~4 chars/token). Past
// that, the runner emits a warning trace event; nothing breaks but
// the operator should consider trimming or moving to per-call
// retrieval (option 3 from the original design discussion).
package semanticpool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// SoftTokenBudget is the recommended cap on pool size. ~2000 tokens
// keeps cold-call cost reasonable and VRAM footprint bounded across
// the v0 five-playbook substrate (~800 MB extra KV with the slim
// persona; see references/hermes-persona-slim-down.md).
const SoftTokenBudget = 2000

// SoftByteBudget is the heuristic byte equivalent of SoftTokenBudget
// (~4 chars/token, English-leaning). Used by IsOverBudget for a
// cheap stdlib-only check before the operator has actual tokenizer
// numbers.
const SoftByteBudget = SoftTokenBudget * 4

// Entry is one item in the pool. Stable IDs let the operator track
// what's in vs. out without diffing prose; tags are an organizational
// hint, not used for retrieval in v0 (whole-pool injection).
type Entry struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Tags      []string  `json:"tags,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// Snapshot is a read-consistent view of the pool at one instant. The
// adapter consumes this as a stable preamble — the byte order of
// rendered entries must be deterministic for the KV cache to hit, so
// callers should iterate via All() rather than sorting locally.
type Snapshot struct {
	// Entries are returned in insertion (file) order. Stable across
	// re-reads as long as the file hasn't changed.
	Entries []Entry
	// Source identifies where this snapshot came from (file path,
	// "in-memory", etc.) for trace records.
	Source string
	// LoadedAt is when the snapshot was produced.
	LoadedAt time.Time
}

// TotalBytes returns the sum of Content lengths — a cheap proxy for
// "how much prefix does this pool add" before we have real tokenizer
// numbers.
func (s Snapshot) TotalBytes() int {
	n := 0
	for _, e := range s.Entries {
		n += len(e.Content)
	}
	return n
}

// IsOverBudget reports whether the snapshot exceeds SoftByteBudget.
// Callers (typically the Hermes adapter) should emit a warning trace
// when this returns true.
func (s Snapshot) IsOverBudget() bool {
	return s.TotalBytes() > SoftByteBudget
}

// Pool is the read-only interface. Implementations: FilePool (JSON
// file on disk), and in-memory for tests / programmatic seeding.
type Pool interface {
	// All returns the current snapshot. Cheap to call; implementations
	// cache aggressively and reload only when the underlying source
	// has changed.
	All(ctx context.Context) (Snapshot, error)
	// Get returns one entry by ID. Returns ErrNotFound when missing.
	Get(ctx context.Context, id string) (Entry, error)
}

// ErrNotFound is returned by Get for missing IDs.
var ErrNotFound = errors.New("semanticpool: entry not found")

// fileShape is the on-disk JSON shape: {"entries": [...]}. The outer
// object lets us add metadata later (version, last_curated_by, etc.)
// without breaking the schema.
type fileShape struct {
	Entries []Entry `json:"entries"`
}

// FilePool is the disk-backed Pool. Re-reads the file when its mtime
// advances. Safe for concurrent use.
type FilePool struct {
	path string

	mu       sync.Mutex
	snapshot Snapshot
	mtime    time.Time
	loaded   bool
}

// NewFile constructs a FilePool. The file is NOT read at construction
// time — All() does the first read. This lets callers wire a pool
// path that doesn't exist yet (treated as empty pool until written).
func NewFile(path string) *FilePool {
	return &FilePool{path: path}
}

// Path returns the configured file path. Useful for trace records.
func (p *FilePool) Path() string { return p.path }

// All implements Pool. Returns the cached snapshot if the file's
// mtime hasn't advanced; otherwise re-reads.
func (p *FilePool) All(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	stat, statErr := os.Stat(p.path)
	switch {
	case errors.Is(statErr, os.ErrNotExist):
		// Missing file is fine — treat as empty pool. Cache the empty
		// snapshot so we don't stat repeatedly.
		if !p.loaded {
			p.snapshot = Snapshot{Source: p.path, LoadedAt: time.Now()}
			p.loaded = true
		}
		return p.snapshot, nil
	case statErr != nil:
		return Snapshot{}, fmt.Errorf("semanticpool: stat %s: %w", p.path, statErr)
	}

	if p.loaded && !stat.ModTime().After(p.mtime) {
		// Cached snapshot is up-to-date.
		return p.snapshot, nil
	}

	data, err := os.ReadFile(p.path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("semanticpool: read %s: %w", p.path, err)
	}
	var parsed fileShape
	if len(data) > 0 {
		if err := json.Unmarshal(data, &parsed); err != nil {
			return Snapshot{}, fmt.Errorf("semanticpool: parse %s: %w", p.path, err)
		}
	}
	p.snapshot = Snapshot{
		Entries:  parsed.Entries,
		Source:   p.path,
		LoadedAt: time.Now(),
	}
	p.mtime = stat.ModTime()
	p.loaded = true
	return p.snapshot, nil
}

// Get implements Pool.
func (p *FilePool) Get(ctx context.Context, id string) (Entry, error) {
	snap, err := p.All(ctx)
	if err != nil {
		return Entry{}, err
	}
	for _, e := range snap.Entries {
		if e.ID == id {
			return e, nil
		}
	}
	return Entry{}, fmt.Errorf("%w: %s", ErrNotFound, id)
}

// Static is an in-memory Pool useful for tests, demos, and
// programmatic seeding where a file is overkill.
type Static struct {
	entries []Entry
	source  string
}

// NewStatic constructs an in-memory Pool from the given entries. The
// entries slice is copied — callers can mutate their slice afterward
// without affecting the pool.
func NewStatic(entries ...Entry) *Static {
	cp := make([]Entry, len(entries))
	copy(cp, entries)
	return &Static{entries: cp, source: "in-memory"}
}

// All implements Pool.
func (s *Static) All(_ context.Context) (Snapshot, error) {
	cp := make([]Entry, len(s.entries))
	copy(cp, s.entries)
	return Snapshot{Entries: cp, Source: s.source, LoadedAt: time.Now()}, nil
}

// Get implements Pool.
func (s *Static) Get(_ context.Context, id string) (Entry, error) {
	for _, e := range s.entries {
		if e.ID == id {
			return e, nil
		}
	}
	return Entry{}, fmt.Errorf("%w: %s", ErrNotFound, id)
}

// RenderPreamble formats a snapshot into the stable text preamble
// adapters prepend to their instructions. Format is deterministic
// (entries in snapshot order) so the resulting prefix is byte-stable
// across calls — that's what keeps the KV cache warm.
//
// Format:
//
//	[semantic-pool: N entries]
//	- id1: content1
//	- id2: content2
//	...
//	[/semantic-pool]
//
// Empty snapshots return the empty string (no preamble emitted).
func RenderPreamble(s Snapshot) string {
	if len(s.Entries) == 0 {
		return ""
	}
	var b stringBuilder
	fmt.Fprintf(&b, "[semantic-pool: %d entries]\n", len(s.Entries))
	for _, e := range s.Entries {
		fmt.Fprintf(&b, "- %s: %s\n", e.ID, e.Content)
	}
	b.WriteString("[/semantic-pool]\n")
	return b.String()
}

// stringBuilder is a small wrapper so fmt.Fprintf works on our value.
// strings.Builder works fine here; using a typed wrapper avoids
// importing strings just for this — net zero, but the inline type
// stays close to the call site.
type stringBuilder = stringBuilderImpl

type stringBuilderImpl struct {
	buf []byte
}

func (b *stringBuilderImpl) Write(p []byte) (int, error) {
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *stringBuilderImpl) WriteString(s string) (int, error) {
	b.buf = append(b.buf, s...)
	return len(s), nil
}

func (b *stringBuilderImpl) String() string { return string(b.buf) }
