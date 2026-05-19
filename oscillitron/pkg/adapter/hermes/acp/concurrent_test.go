// CLAUDE GENERATED
package acp

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// TestClient_ConcurrentCalls fires N parallel Call invocations against
// a mock server that echoes a unique sentinel per request ID. The
// client must (a) demux responses correctly to the originating caller
// without crossing wires, and (b) not data-race under -race.
//
// Two pieces are under test:
//   - The pending sync.Map keyed by request ID
//   - The serialized writer (wMu) under N concurrent writers
func TestClient_ConcurrentCalls(t *testing.T) {
	r, w, srv, closeAll := newPair()
	defer closeAll()
	c := NewClient(r, w, nil)
	defer c.Close()

	// Server loop: read every frame, echo a result with the same id
	// and a payload that includes the request's params so the test can
	// verify no wire-crossing happened.
	srvDone := make(chan struct{})
	go func() {
		defer close(srvDone)
		for {
			if !srv.in.Scan() {
				return
			}
			var m rawMessage
			if err := json.Unmarshal(srv.in.Bytes(), &m); err != nil {
				continue
			}
			if len(m.ID) == 0 {
				continue
			}
			// Mirror back params as the result for verification.
			srv.writeRaw(t, map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(m.ID),
				"result":  map[string]any{"echo": json.RawMessage(m.Params)},
			})
		}
	}()

	const N = 64
	var wg sync.WaitGroup
	wg.Add(N)
	mismatches := make([]string, 0)
	var mu sync.Mutex
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			params := map[string]any{"sentinel": i}
			var out struct {
				Echo struct {
					Sentinel int `json:"sentinel"`
				} `json:"echo"`
			}
			if err := c.Call(ctx, "test/echo", params, &out); err != nil {
				mu.Lock()
				mismatches = append(mismatches, "call err: "+err.Error())
				mu.Unlock()
				return
			}
			if out.Echo.Sentinel != i {
				mu.Lock()
				mismatches = append(mismatches,
					"wire-crossing: sent "+itoa(i)+" got "+itoa(out.Echo.Sentinel))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(mismatches) > 0 {
		for _, m := range mismatches {
			t.Error(m)
		}
	}
	// Closing the client lets the read loop exit; the mock server loop
	// exits on EOF from the pipe close.
}

func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = digits[i%10]
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
