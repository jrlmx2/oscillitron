// CLAUDE GENERATED
package acp

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// BenchmarkClient_RoundTrip measures pure protocol overhead: JSON
// marshal + write + scan + JSON unmarshal + demux. The mock server
// echoes immediately, so any number here is the floor below which
// no real-world ACP exchange can go on this hardware.
func BenchmarkClient_RoundTrip(b *testing.B) {
	r, w, srv, closeAll := newPair()
	defer closeAll()
	c := NewClient(r, w, nil)
	defer c.Close()

	// Server loop: echo every request as an empty result.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for srv.in.Scan() {
			var m rawMessage
			if json.Unmarshal(srv.in.Bytes(), &m) != nil {
				return
			}
			if len(m.ID) == 0 {
				continue
			}
			out := []byte(`{"jsonrpc":"2.0","id":`)
			out = append(out, m.ID...)
			out = append(out, `,"result":{}}`+"\n"...)
			if _, err := srv.out.Write(out); err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	params := map[string]any{"k": "v"}
	var out struct{}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := c.Call(ctx, "bench/echo", params, &out); err != nil {
			b.Fatal(err)
		}
	}
}
