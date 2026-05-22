// CLAUDE GENERATED
package vram

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// mutableProbe is a configurable Probe for tests. Reading available/err
// is mutex-guarded so concurrent Acquires don't race on the test
// helper.
type mutableProbe struct {
	mu        sync.Mutex
	available uint64
	err       error
	calls     int
}

func (p *mutableProbe) Name() string { return "fake" }

func (p *mutableProbe) Probe(_ context.Context) (Report, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.err != nil {
		return Report{}, p.err
	}
	return Report{
		Source:         "fake",
		AvailableBytes: p.available,
	}, nil
}

func (p *mutableProbe) set(available uint64, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.available = available
	p.err = err
}

// modelSpecFor returns a ModelSpec whose Estimator yields exactly
// perCallBytes per call. Math: Estimate = BytesPerToken × min(prefix,
// ctx) where BytesPerToken = 2×Layers×KVHiddenDim×KVDtypeBytes. With
// Layers=1, KVDtypeBytes=2, ContextSize=1, PrefixTokens=1, the per-call
// estimate is 4×KVHiddenDim. So KVHiddenDim = perCallBytes / 4.
func modelSpecFor(perCallBytes int) ModelSpec {
	if perCallBytes < 4 {
		perCallBytes = 4
	}
	return ModelSpec{
		Name:         "test",
		Layers:       1,
		KVHiddenDim:  perCallBytes / 4,
		KVDtypeBytes: 2,
		ContextSize:  1,
		PrefixTokens: 1,
	}
}

// newGov is a test helper that fails the test on construction error.
func newGov(t *testing.T, cfg GovernorConfig) *Governor {
	t.Helper()
	g, err := NewGovernor(cfg)
	if err != nil {
		t.Fatalf("NewGovernor: %v", err)
	}
	return g
}

// --- tests ---

func TestGovernor_NilSafe(t *testing.T) {
	var g *Governor
	lease, err := g.Acquire(context.Background())
	if err != nil {
		t.Fatalf("nil governor Acquire returned error: %v", err)
	}
	if lease == nil {
		t.Fatal("nil governor returned nil lease")
	}
	lease.Release()
	lease.Release() // double-release safe
}

func TestGovernor_RejectsInvalidModelSpec(t *testing.T) {
	cases := []struct {
		name string
		spec ModelSpec
	}{
		{"zero layers", ModelSpec{Layers: 0, KVHiddenDim: 256, KVDtypeBytes: 2, ContextSize: 4096}},
		{"zero kv", ModelSpec{Layers: 32, KVHiddenDim: 0, KVDtypeBytes: 2, ContextSize: 4096}},
		{"zero dtype", ModelSpec{Layers: 32, KVHiddenDim: 256, KVDtypeBytes: 0, ContextSize: 4096}},
		{"zero context", ModelSpec{Layers: 32, KVHiddenDim: 256, KVDtypeBytes: 2, ContextSize: 0}},
		{"negative prefix", ModelSpec{Layers: 32, KVHiddenDim: 256, KVDtypeBytes: 2, ContextSize: 4096, PrefixTokens: -1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewGovernor(GovernorConfig{Model: c.spec})
			if err == nil {
				t.Errorf("NewGovernor should reject %+v", c.spec)
			}
		})
	}
}

func TestGovernor_RejectsReserveFractionOutOfRange(t *testing.T) {
	_, err := NewGovernor(GovernorConfig{
		Model:           modelSpecFor(1024),
		ReserveFraction: 1.0,
	})
	if err == nil {
		t.Error("NewGovernor should reject ReserveFraction >= 1")
	}
}

func TestGovernor_FastPath_CeilingOnly(t *testing.T) {
	// Probe override returns an error → governor falls back to
	// ceiling-only enforcement.
	probe := &mutableProbe{err: errors.New("no probe")}
	g := newGov(t, GovernorConfig{
		Model:         modelSpecFor(1024),
		Ceiling:       2,
		ProbeOverride: probe,
	})
	ctx := context.Background()

	l1, err := g.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire 1 err: %v", err)
	}
	l2, err := g.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire 2 err: %v", err)
	}

	// 3rd blocks on ceiling.
	ctx3, cancel3 := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel3()
	_, err = g.Acquire(ctx3)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("3rd acquire err = %v, want DeadlineExceeded", err)
	}

	l1.Release()
	l4, err := g.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire after release err: %v", err)
	}
	l2.Release()
	l4.Release()
}

func TestGovernor_HeadroomEnforcement(t *testing.T) {
	// Available 4 GiB, ReserveFraction 0.0625 (= 1/16), per-call 1 GiB.
	// Budget = 4 × (1 − 0.0625) = 3.75 GiB.
	// First lease: free pass. Inflight=1 GiB.
	// Lease 2: 1+1=2 ≤ 3.75 → grant.
	// Lease 3: 2+1=3 ≤ 3.75 → grant.
	// Lease 4: 3+1=4 > 3.75 → BLOCK.
	probe := &mutableProbe{available: 4 * 1024 * 1024 * 1024}
	g := newGov(t, GovernorConfig{
		Model:           modelSpecFor(1024 * 1024 * 1024),
		Ceiling:         10,
		ReserveFraction: 0.0625,
		ProbeOverride:   probe,
	})
	ctx := context.Background()

	leases := make([]*Lease, 0, 3)
	for i := 0; i < 3; i++ {
		l, err := g.Acquire(ctx)
		if err != nil {
			t.Fatalf("Acquire %d err: %v", i+1, err)
		}
		leases = append(leases, l)
	}

	ctx4, cancel4 := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel4()
	_, err := g.Acquire(ctx4)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("4th acquire err = %v, want DeadlineExceeded", err)
	}

	leases[0].Release()

	got := make(chan *Lease, 1)
	go func() {
		l, err := g.Acquire(ctx)
		if err != nil {
			t.Errorf("queued acquire err: %v", err)
			got <- nil
			return
		}
		got <- l
	}()
	select {
	case l := <-got:
		if l == nil {
			t.Fatal("queued acquire returned nil")
		}
		l.Release()
	case <-time.After(500 * time.Millisecond):
		t.Fatal("queued acquire didn't proceed after release")
	}

	for _, l := range leases[1:] {
		l.Release()
	}
}

func TestGovernor_ProbeFailure_FallsBackToCeiling(t *testing.T) {
	probe := &mutableProbe{err: errors.New("probe boom")}
	g := newGov(t, GovernorConfig{
		Model:         modelSpecFor(1024 * 1024 * 1024),
		Ceiling:       2,
		ProbeOverride: probe,
	})
	ctx := context.Background()

	l1, _ := g.Acquire(ctx)
	l2, _ := g.Acquire(ctx)

	ctx3, cancel3 := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel3()
	_, err := g.Acquire(ctx3)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("3rd acquire err = %v, want DeadlineExceeded", err)
	}

	l1.Release()
	l2.Release()
}

func TestGovernor_FIFO_WakeOrder(t *testing.T) {
	g := newGov(t, GovernorConfig{
		Model:         modelSpecFor(1024),
		Ceiling:       1,
		ProbeOverride: &mutableProbe{err: errors.New("no probe")},
	})
	ctx := context.Background()

	head, err := g.Acquire(ctx)
	if err != nil {
		t.Fatalf("head Acquire err: %v", err)
	}

	const N = 3
	completed := make(chan int64, N)

	for i := 0; i < N; i++ {
		i := i
		go func() {
			l, err := g.Acquire(ctx)
			if err != nil {
				t.Errorf("waiter %d err: %v", i, err)
				return
			}
			completed <- int64(i)
			time.Sleep(5 * time.Millisecond)
			l.Release()
		}()
		deadline := time.After(time.Second)
		for g.QueuedWaiters() < i+1 {
			select {
			case <-deadline:
				t.Fatalf("waiter %d failed to enqueue (queued=%d)", i, g.QueuedWaiters())
			default:
				time.Sleep(time.Millisecond)
			}
		}
	}

	head.Release()

	order := make([]int64, 0, N)
	for len(order) < N {
		select {
		case got := <-completed:
			order = append(order, got)
		case <-time.After(2 * time.Second):
			t.Fatalf("only got %d completions: %v", len(order), order)
		}
	}

	for i, v := range order {
		if v != int64(i) {
			t.Errorf("wake order = %v, want strict FIFO [0,1,2]", order)
			break
		}
	}
}

func TestGovernor_CancelWhileWaiting(t *testing.T) {
	g := newGov(t, GovernorConfig{
		Model:         modelSpecFor(1024),
		Ceiling:       1,
		ProbeOverride: &mutableProbe{err: errors.New("no probe")},
	})
	ctx := context.Background()

	head, err := g.Acquire(ctx)
	if err != nil {
		t.Fatalf("head Acquire err: %v", err)
	}
	defer head.Release()

	cancelCtx, cancel := context.WithCancel(ctx)
	errCh := make(chan error, 1)
	go func() {
		_, err := g.Acquire(cancelCtx)
		errCh <- err
	}()

	for g.QueuedWaiters() == 0 {
		time.Sleep(time.Millisecond)
	}

	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled Acquire err = %v, want Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled Acquire didn't return")
	}

	if q := g.QueuedWaiters(); q != 0 {
		t.Errorf("QueuedWaiters after cancel = %d, want 0", q)
	}
}

func TestGovernor_CancelAfterWake_DoesNotStrandSlot(t *testing.T) {
	g := newGov(t, GovernorConfig{
		Model:         modelSpecFor(1024),
		Ceiling:       1,
		ProbeOverride: &mutableProbe{err: errors.New("no probe")},
	})
	ctx := context.Background()

	head, err := g.Acquire(ctx)
	if err != nil {
		t.Fatalf("head Acquire err: %v", err)
	}

	ctxA, cancelA := context.WithCancel(ctx)
	aWaiting := make(chan struct{})
	aResult := make(chan error, 1)
	go func() {
		close(aWaiting)
		_, err := g.Acquire(ctxA)
		aResult <- err
	}()
	<-aWaiting
	for g.QueuedWaiters() < 1 {
		time.Sleep(time.Millisecond)
	}

	bGot := make(chan *Lease, 1)
	go func() {
		l, err := g.Acquire(ctx)
		if err != nil {
			t.Errorf("waiter B err: %v", err)
			bGot <- nil
			return
		}
		bGot <- l
	}()
	for g.QueuedWaiters() < 2 {
		time.Sleep(time.Millisecond)
	}

	cancelA()
	head.Release()

	select {
	case err := <-aResult:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("waiter A err = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter A didn't return")
	}

	select {
	case l := <-bGot:
		if l == nil {
			t.Fatal("waiter B returned nil")
		}
		l.Release()
	case <-time.After(2 * time.Second):
		t.Fatal("waiter B starved — cancel-after-wake didn't compensate")
	}
}

func TestGovernor_DoubleRelease_IsNoop(t *testing.T) {
	g := newGov(t, GovernorConfig{
		Model:         modelSpecFor(1024),
		Ceiling:       1,
		ProbeOverride: &mutableProbe{err: errors.New("no probe")},
	})
	ctx := context.Background()
	l, err := g.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire err: %v", err)
	}
	l.Release()
	l.Release()

	snap := g.Snapshot(ctx)
	if snap.ActiveLeases != 0 {
		t.Errorf("ActiveLeases after double-release = %d, want 0", snap.ActiveLeases)
	}
}

func TestGovernor_FirstLease_AlwaysGrantedEvenIfTooBig(t *testing.T) {
	// 1 GiB available, 4 GiB per call. The first lease must still be
	// granted — refusing it would deadlock the runtime.
	probe := &mutableProbe{available: 1024 * 1024 * 1024}
	g := newGov(t, GovernorConfig{
		Model:           modelSpecFor(4 * 1024 * 1024 * 1024),
		Ceiling:         10,
		ReserveFraction: 0.05,
		ProbeOverride:   probe,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	l, err := g.Acquire(ctx)
	if err != nil {
		t.Fatalf("first acquire err: %v", err)
	}
	if l == nil {
		t.Fatal("first acquire returned nil")
	}
	l.Release()
}

func TestGovernor_SnapshotShape(t *testing.T) {
	probe := &mutableProbe{available: 8 * 1024 * 1024 * 1024}
	g := newGov(t, GovernorConfig{
		Model:           modelSpecFor(1024 * 1024 * 1024),
		Ceiling:         4,
		ReserveFraction: 0.0625,
		ProbeOverride:   probe,
	})
	ctx := context.Background()

	l1, _ := g.Acquire(ctx)
	l2, _ := g.Acquire(ctx)
	defer l1.Release()
	defer l2.Release()

	snap := g.Snapshot(ctx)
	if snap.ActiveLeases != 2 {
		t.Errorf("ActiveLeases = %d, want 2", snap.ActiveLeases)
	}
	if snap.PerCallBytes != 1024*1024*1024 {
		t.Errorf("PerCallBytes = %d, want 1 GiB", snap.PerCallBytes)
	}
	if snap.AvailableBytes != probe.available {
		t.Errorf("AvailableBytes = %d, want %d", snap.AvailableBytes, probe.available)
	}
	if snap.Ceiling != 4 {
		t.Errorf("Ceiling = %d, want 4", snap.Ceiling)
	}
	if snap.ReserveFraction != 0.0625 {
		t.Errorf("ReserveFraction = %v, want 0.0625", snap.ReserveFraction)
	}
	if snap.Model.Layers == 0 {
		t.Error("Snapshot.Model should carry the configured ModelSpec")
	}
}

func TestGovernor_ProbeRecovery_ReleasesQueuedWaiters(t *testing.T) {
	probe := &mutableProbe{err: errors.New("boom")}
	g := newGov(t, GovernorConfig{
		Model:         modelSpecFor(1024 * 1024 * 1024),
		Ceiling:       1,
		ProbeOverride: probe,
	})
	ctx := context.Background()

	l1, err := g.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire 1 err: %v", err)
	}

	got := make(chan *Lease, 1)
	go func() {
		l, err := g.Acquire(ctx)
		if err != nil {
			t.Errorf("queued Acquire err: %v", err)
			got <- nil
			return
		}
		got <- l
	}()
	for g.QueuedWaiters() == 0 {
		time.Sleep(time.Millisecond)
	}

	probe.set(16*1024*1024*1024, nil)

	l1.Release()
	select {
	case l := <-got:
		if l == nil {
			t.Fatal("queued acquire returned nil")
		}
		l.Release()
	case <-time.After(time.Second):
		t.Fatal("queued waiter didn't wake")
	}
}

func TestGovernor_Ceiling_DefaultsToNumCPU(t *testing.T) {
	g := newGov(t, GovernorConfig{
		Model: modelSpecFor(1024),
		// Ceiling unset → defaults to runtime.NumCPU()
	})
	if g.Ceiling() <= 0 {
		t.Errorf("Ceiling() = %d, want > 0 (NumCPU default)", g.Ceiling())
	}
}
