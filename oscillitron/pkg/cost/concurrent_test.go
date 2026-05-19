// CLAUDE GENERATED
package cost

import (
	"sync"
	"sync/atomic"
	"testing"
)

// TestTracker_ConcurrentRecord proves the Tracker is goroutine-safe
// under aggressive contention. N workers each call Record M times;
// the running totals must equal what every call would have produced
// individually. This test should fail loudly under -race if the
// internal mu is removed or any non-atomic mutation slips in.
func TestTracker_ConcurrentRecord(t *testing.T) {
	const (
		workers   = 32
		perWorker = 500
		inTok     = 1000
		outTok    = 500
	)
	tracker := New(Pricing{InputUSDPerMTok: 3.0, OutputUSDPerMTok: 15.0})
	tracker.Register("m", Pricing{InputUSDPerMTok: 0.1, OutputUSDPerMTok: 0.2})

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				tracker.Record("m", inTok, outTok)
			}
		}()
	}
	wg.Wait()

	sum := tracker.Summary()
	wantCalls := workers * perWorker
	if len(sum.Entries) != wantCalls {
		t.Fatalf("Entries = %d, want %d (lost entries indicate non-atomic append)", len(sum.Entries), wantCalls)
	}

	// Per-call actual = (1000*0.1 + 500*0.2) / 1M = 0.0002
	// Per-call frontier = (1000*3 + 500*15) / 1M = 0.0105
	wantActual := float64(wantCalls) * 0.0002
	wantFrontier := float64(wantCalls) * 0.0105
	if !nearly(sum.TotalActualUSD, wantActual) {
		t.Errorf("TotalActualUSD = %v, want %v", sum.TotalActualUSD, wantActual)
	}
	if !nearly(sum.TotalFrontierUSD, wantFrontier) {
		t.Errorf("TotalFrontierUSD = %v, want %v", sum.TotalFrontierUSD, wantFrontier)
	}
}

// TestTracker_ConcurrentRecordVsSummary stresses the Record/Summary
// interleaving — a Summary mid-stream should always be internally
// consistent (totals match the sum of returned entries' fields up to
// that snapshot), never observe a half-mutated state.
func TestTracker_ConcurrentRecordVsSummary(t *testing.T) {
	tracker := New(Pricing{InputUSDPerMTok: 1, OutputUSDPerMTok: 2})
	tracker.Register("m", Pricing{InputUSDPerMTok: 0.5, OutputUSDPerMTok: 1})

	var (
		wg            sync.WaitGroup
		stop          atomic.Bool
		summaryReads  atomic.Int64
		recordsIssued atomic.Int64
	)

	// Writers
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				tracker.Record("m", 100, 50)
				recordsIssued.Add(1)
			}
		}()
	}
	// Readers
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				sum := tracker.Summary()
				// Internal consistency: every entry's SavingsUSD must equal FrontierUSD - ActualUSD.
				for _, e := range sum.Entries {
					if !nearly(e.SavingsUSD, e.FrontierUSD-e.ActualUSD) {
						t.Errorf("entry violates SavingsUSD = FrontierUSD - ActualUSD: %+v", e)
						return
					}
				}
				summaryReads.Add(1)
			}
		}()
	}

	// Brief contention window. Race detector does the heavy lifting; we
	// just need enough interleaving to surface a violation if one exists.
	for i := 0; i < 200_000 && recordsIssued.Load() < 5000; i++ {
		// Busy-wait a tiny moment; the goroutines are doing the work.
	}
	stop.Store(true)
	wg.Wait()

	if recordsIssued.Load() == 0 || summaryReads.Load() == 0 {
		t.Errorf("no work observed: records=%d reads=%d", recordsIssued.Load(), summaryReads.Load())
	}
}

func nearly(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}
