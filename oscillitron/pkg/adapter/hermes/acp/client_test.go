// CLAUDE GENERATED
package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockServer is a trivial ACP-server stand-in. It reads newline-delimited
// JSON-RPC frames from cReader (the client's stdout view of us) and
// writes replies / notifications to cWriter (the client's stdin view).
type mockServer struct {
	in  *bufio.Scanner
	out io.Writer
}

func newPair() (clientR io.Reader, clientW io.Writer, srv *mockServer, closeAll func()) {
	// Client reads what server writes (a); client writes what server reads (b).
	aR, aW := io.Pipe()
	bR, bW := io.Pipe()
	srv = &mockServer{
		in:  bufio.NewScanner(bR),
		out: aW,
	}
	srv.in.Buffer(make([]byte, 64*1024), 1<<20)
	closeAll = func() {
		_ = aW.Close()
		_ = aR.Close()
		_ = bW.Close()
		_ = bR.Close()
	}
	return aR, bW, srv, closeAll
}

// readFrame pulls one JSON-RPC frame from the client.
func (s *mockServer) readFrame(t *testing.T) rawMessage {
	t.Helper()
	if !s.in.Scan() {
		t.Fatalf("mock server: no frame: %v", s.in.Err())
	}
	var m rawMessage
	if err := json.Unmarshal(s.in.Bytes(), &m); err != nil {
		t.Fatalf("mock server: decode: %v (line=%q)", err, s.in.Text())
	}
	return m
}

func (s *mockServer) writeRaw(t *testing.T, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("mock server: marshal: %v", err)
	}
	if _, err := s.out.Write(append(b, '\n')); err != nil {
		t.Fatalf("mock server: write: %v", err)
	}
}

func TestInitializeRoundTrip(t *testing.T) {
	r, w, srv, closeAll := newPair()
	defer closeAll()

	c := NewClient(r, w, nil)
	defer c.Close()

	// Server side: expect one request, reply with a result.
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := srv.readFrame(t)
		if req.Method != "initialize" {
			t.Errorf("got method %q, want initialize", req.Method)
			return
		}
		srv.writeRaw(t, map[string]any{
			"jsonrpc": "2.0",
			"id":      json.RawMessage(req.ID),
			"result": map[string]any{
				"protocolVersion": 1,
				"agentInfo":       map[string]string{"name": "mock-hermes", "version": "0.0.1"},
			},
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := c.Initialize(ctx, InitializeRequest{
		ProtocolVersion:    CurrentProtocolVersion,
		ClientInfo:         &Implementation{Name: "oscillitron-test", Version: "0"},
		ClientCapabilities: ClientCapabilities{},
	})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if resp.ProtocolVersion != 1 {
		t.Errorf("ProtocolVersion = %d, want 1", resp.ProtocolVersion)
	}
	if resp.AgentInfo == nil || resp.AgentInfo.Name != "mock-hermes" {
		t.Errorf("AgentInfo = %+v, want name=mock-hermes", resp.AgentInfo)
	}
	<-done
}

func TestPromptWithStreamingChunks(t *testing.T) {
	r, w, srv, closeAll := newPair()
	defer closeAll()

	var (
		mu     sync.Mutex
		chunks []string
	)
	onNotif := func(method string, params json.RawMessage) {
		if method != "session/update" {
			return
		}
		var n SessionNotification
		if err := json.Unmarshal(params, &n); err != nil {
			t.Errorf("decode notification: %v", err)
			return
		}
		var chunk AgentMessageChunk
		if err := json.Unmarshal(n.Update, &chunk); err != nil {
			// Not an agent_message_chunk variant — fine, ignore.
			return
		}
		if chunk.SessionUpdate != "agent_message_chunk" {
			return
		}
		mu.Lock()
		chunks = append(chunks, chunk.Content.Text)
		mu.Unlock()
	}

	c := NewClient(r, w, onNotif)
	defer c.Close()

	// Server: read prompt request, stream 3 chunks, then send response.
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := srv.readFrame(t)
		if req.Method != "session/prompt" {
			t.Errorf("got method %q, want session/prompt", req.Method)
			return
		}
		for _, piece := range []string{"hello ", "from ", "hermes"} {
			srv.writeRaw(t, map[string]any{
				"jsonrpc": "2.0",
				"method":  "session/update",
				"params": map[string]any{
					"sessionId": "sess-1",
					"update": map[string]any{
						"sessionUpdate": "agent_message_chunk",
						"content":       map[string]any{"type": "text", "text": piece},
					},
				},
			})
		}
		srv.writeRaw(t, map[string]any{
			"jsonrpc": "2.0",
			"id":      json.RawMessage(req.ID),
			"result":  map[string]any{"stopReason": "end_turn"},
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := c.Prompt(ctx, PromptRequest{
		SessionID: "sess-1",
		Prompt:    []ContentBlock{{Type: "text", Text: "go"}},
	})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if resp.StopReason != StopReasonEndTurn {
		t.Errorf("StopReason = %q, want end_turn", resp.StopReason)
	}
	<-done

	mu.Lock()
	got := strings.Join(chunks, "")
	mu.Unlock()
	if got != "hello from hermes" {
		t.Errorf("chunks joined = %q, want %q", got, "hello from hermes")
	}
}

func TestCallReturnsRPCError(t *testing.T) {
	r, w, srv, closeAll := newPair()
	defer closeAll()
	c := NewClient(r, w, nil)
	defer c.Close()

	go func() {
		req := srv.readFrame(t)
		srv.writeRaw(t, map[string]any{
			"jsonrpc": "2.0",
			"id":      json.RawMessage(req.ID),
			"error":   map[string]any{"code": -32602, "message": "bad params"},
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := c.NewSession(ctx, NewSessionRequest{Cwd: "/tmp"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	rpcErr, ok := err.(*RPCError)
	if !ok {
		t.Fatalf("err type = %T, want *RPCError", err)
	}
	if rpcErr.Code != -32602 {
		t.Errorf("code = %d, want -32602", rpcErr.Code)
	}
}

func TestCallRespectsContextCancel(t *testing.T) {
	r, w, _, closeAll := newPair()
	defer closeAll()
	c := NewClient(r, w, nil)
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel
	_, err := c.Initialize(ctx, InitializeRequest{ProtocolVersion: CurrentProtocolVersion})
	if err == nil {
		t.Fatal("expected error from canceled ctx")
	}
}
