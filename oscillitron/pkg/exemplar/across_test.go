package exemplar

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func seedAcross(t *testing.T) *FileStore {
	t.Helper()
	s := &FileStore{Dir: t.TempDir()}
	ctx := context.Background()
	add := func(action, prompt, output string, score float64) {
		if err := s.Add(ctx, Exemplar{Action: action, Prompt: prompt, Output: output, Score: score, SourceCase: prompt}); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	// process exemplars
	add("process", "compute the derivative of a polynomial", "...", 0.9)
	add("process", "what is the capital of france", "...", 0.8)
	// plan exemplars
	add("plan", "lay out the steps to deploy a service", "...", 0.9)
	add("plan", "plan a research study protocol", "...", 0.8)
	return s
}

func TestRetrieveAcross_RanksByBM25AcrossActions(t *testing.T) {
	s := seedAcross(t)
	nbrs, err := s.RetrieveAcross(context.Background(), "compute the derivative of x cubed", 4)
	if err != nil {
		t.Fatalf("RetrieveAcross: %v", err)
	}
	if len(nbrs) == 0 {
		t.Fatal("expected neighbors, got none")
	}
	// The lexically-closest exemplar is the process derivative one.
	top := nbrs[0]
	if top.Exemplar.Action != "process" {
		t.Errorf("top neighbor action = %q, want process", top.Exemplar.Action)
	}
	if top.Sim <= 0 {
		t.Errorf("top neighbor Sim = %v, want > 0", top.Sim)
	}
	// Sims are sorted descending.
	for i := 1; i < len(nbrs); i++ {
		if nbrs[i].Sim > nbrs[i-1].Sim {
			t.Errorf("neighbors not sorted by Sim desc at %d", i)
		}
	}
}

func TestRetrieveAcross_EmptyStoreAndKLE0(t *testing.T) {
	// Empty dir → nil, nil.
	empty := &FileStore{Dir: t.TempDir()}
	nbrs, err := empty.RetrieveAcross(context.Background(), "anything", 4)
	if err != nil || nbrs != nil {
		t.Errorf("empty store: got (%v, %v), want (nil, nil)", nbrs, err)
	}
	// k <= 0 → nil, nil.
	s := seedAcross(t)
	if nbrs, err := s.RetrieveAcross(context.Background(), "x", 0); err != nil || nbrs != nil {
		t.Errorf("k=0: got (%v, %v), want (nil, nil)", nbrs, err)
	}
	// Missing dir → error.
	missing := &FileStore{Dir: filepath.Join(t.TempDir(), "does-not-exist")}
	if _, err := missing.RetrieveAcross(context.Background(), "x", 4); err == nil {
		t.Error("missing dir: expected error, got nil")
	}
}

func TestRetrieveAcross_DoesNotBumpLastRetrievedAt(t *testing.T) {
	s := seedAcross(t)
	// Read the process file's LastRetrievedAt before.
	readLRA := func() time.Time {
		data, err := os.ReadFile(filepath.Join(s.Dir, "process.json"))
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var ex []Exemplar
		if err := json.Unmarshal(data, &ex); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(ex) == 0 {
			t.Fatal("no process exemplars on disk")
		}
		return ex[0].LastRetrievedAt
	}
	before := readLRA()
	if _, err := s.RetrieveAcross(context.Background(), "compute the derivative", 4); err != nil {
		t.Fatalf("RetrieveAcross: %v", err)
	}
	if after := readLRA(); !after.Equal(before) {
		t.Errorf("RetrieveAcross bumped LastRetrievedAt: before=%v after=%v (must be read-only)", before, after)
	}
}

func TestStoreInterfaceUnchanged_AndAcrossCapability(t *testing.T) {
	// Store is still satisfied (no interface widening), and FileStore
	// additionally satisfies the optional AcrossRetriever capability.
	var _ Store = (*FileStore)(nil)
	var _ AcrossRetriever = (*FileStore)(nil)
}
