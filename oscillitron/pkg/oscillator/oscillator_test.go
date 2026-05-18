// CLAUDE GENERATED
package oscillator

import (
	"context"
	"testing"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

func TestOscillatorEmitsWithSourceID(t *testing.T) {
	a := stub.New("code", stub.ModeDone).WithConfidence(0.7)
	o := New("code-osc", a, nil)

	in := make(chan session.Envelope, 1)
	out := make(chan Emission, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	go o.Run(ctx, in, out)

	in <- session.Envelope{ID: "s1", Objective: "review"}
	close(in)

	select {
	case em := <-out:
		if em.From != "code-osc" {
			t.Errorf("Emission.From = %q, want %q", em.From, "code-osc")
		}
		got := em.Envelope
		if got.Outcome == nil {
			t.Fatal("outcome not populated")
		}
		if got.Outcome.ExitReason != session.ExitDone {
			t.Errorf("ExitReason = %q, want %q", got.Outcome.ExitReason, session.ExitDone)
		}
		if got.Routing.Model != "code" {
			t.Errorf("Routing.Model = %q, want %q", got.Routing.Model, "code")
		}
	case <-ctx.Done():
		t.Fatal("oscillator did not emit before timeout")
	}
}

func TestOscillatorSurfacesAdapterErrorAsInhibited(t *testing.T) {
	a := stub.New("flaky", stub.ModeError)
	o := New("flaky-osc", a, nil)

	in := make(chan session.Envelope, 1)
	out := make(chan Emission, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	go o.Run(ctx, in, out)

	in <- session.Envelope{ID: "s1"}
	close(in)

	em := <-out
	if em.Envelope.Outcome == nil {
		t.Fatal("outcome should be set even on error")
	}
	if em.Envelope.Outcome.ExitReason != session.ExitInhibited {
		t.Errorf("ExitReason = %q, want %q (errors surface as inhibited)",
			em.Envelope.Outcome.ExitReason, session.ExitInhibited)
	}
}

func TestOscillatorStopsOnContextCancel(t *testing.T) {
	a := stub.New("x", stub.ModeDone)
	o := New("x-osc", a, nil)

	in := make(chan session.Envelope) // no buffer, no sender
	out := make(chan Emission, 1)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		o.Run(ctx, in, out)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("oscillator did not exit after context cancel")
	}
}
