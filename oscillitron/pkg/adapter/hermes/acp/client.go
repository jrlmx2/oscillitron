// CLAUDE GENERATED
package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// jsonRPCVersion is the only version this client speaks.
const jsonRPCVersion = "2.0"

// rawMessage is the wire-level JSON-RPC frame (request, response, or
// notification). Field presence disambiguates the kind:
//   - Method != "" && ID present  -> request from peer (we don't handle any in the thin cut)
//   - Method != "" && ID absent   -> notification from peer
//   - Method == "" && ID present  -> response to one of our requests
type rawMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is the JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if len(e.Data) > 0 && string(e.Data) != "null" {
		return fmt.Sprintf("acp rpc error %d: %s (data=%s)", e.Code, e.Message, string(e.Data))
	}
	return fmt.Sprintf("acp rpc error %d: %s", e.Code, e.Message)
}

// NotificationHandler is invoked for every agent->client notification
// received on the connection. Method is the JSON-RPC method name
// (e.g. "session/update"); params is the raw JSON for the caller to
// decode. Handlers run on the client's read goroutine — they must not
// block on the client.
type NotificationHandler func(method string, params json.RawMessage)

// Client is a minimal JSON-RPC 2.0 client over newline-delimited JSON
// on an io.ReadWriter. One read goroutine demuxes responses to
// in-flight requests and routes notifications to the handler.
//
// Concurrency: Call and Notify are safe for concurrent use. Close is
// safe to call once; subsequent calls return ErrClosed.
type Client struct {
	w       io.Writer
	wMu     sync.Mutex // serializes writes
	nextID  atomic.Int64
	pending sync.Map // map[int64]chan *rawMessage
	onNotif NotificationHandler

	closeOnce sync.Once
	closed    chan struct{}
	readErr   atomic.Value // error from the read loop after close
}

// ErrClosed is returned by Call/Notify after the connection is closed.
var ErrClosed = errors.New("acp: client closed")

// NewClient starts a client speaking JSON-RPC over (r, w). The caller
// retains ownership of r and w; closing them (or canceling them upstream)
// causes the read loop to exit. The notification handler may be nil.
func NewClient(r io.Reader, w io.Writer, onNotif NotificationHandler) *Client {
	c := &Client{
		w:       w,
		onNotif: onNotif,
		closed:  make(chan struct{}),
	}
	go c.readLoop(r)
	return c
}

// Close stops the client. It does not close r or w (the spawning code
// in pkg/adapter/hermes owns the process and its pipes).
func (c *Client) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	// Fail any in-flight calls.
	c.pending.Range(func(k, v any) bool {
		select {
		case v.(chan *rawMessage) <- nil:
		default:
		}
		c.pending.Delete(k)
		return true
	})
	return nil
}

// Call issues a JSON-RPC request and waits for the matching response.
// params may be nil for parameterless methods. The result is decoded
// into out (which may also be nil to discard).
func (c *Client) Call(ctx context.Context, method string, params, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id := c.nextID.Add(1)
	respCh := make(chan *rawMessage, 1)
	c.pending.Store(id, respCh)
	defer c.pending.Delete(id)

	idBytes, _ := json.Marshal(id)
	if err := c.writeFrame(rawMessage{
		JSONRPC: jsonRPCVersion,
		ID:      idBytes,
		Method:  method,
		Params:  marshalOrNil(params),
	}); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		if err, _ := c.readErr.Load().(error); err != nil {
			return err
		}
		return ErrClosed
	case msg := <-respCh:
		if msg == nil {
			return ErrClosed
		}
		if msg.Error != nil {
			return msg.Error
		}
		if out == nil || len(msg.Result) == 0 {
			return nil
		}
		return json.Unmarshal(msg.Result, out)
	}
}

// Notify sends a JSON-RPC notification (no ID, no response). Used for
// any agent-callable client method we add later; not used in the thin
// cut but cheap to keep.
func (c *Client) Notify(method string, params any) error {
	return c.writeFrame(rawMessage{
		JSONRPC: jsonRPCVersion,
		Method:  method,
		Params:  marshalOrNil(params),
	})
}

func (c *Client) writeFrame(m rawMessage) error {
	select {
	case <-c.closed:
		return ErrClosed
	default:
	}
	buf, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.wMu.Lock()
	defer c.wMu.Unlock()
	if _, err := c.w.Write(append(buf, '\n')); err != nil {
		return err
	}
	return nil
}

func (c *Client) readLoop(r io.Reader) {
	defer c.Close()
	// Increase max line size; LLM outputs can produce long single-line
	// JSON payloads when streaming is chunky.
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg rawMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			// Malformed frame — record and bail. A real client might
			// keep going, but for the thin cut a parse error is fatal.
			c.readErr.Store(fmt.Errorf("acp: read decode: %w", err))
			return
		}
		switch {
		case msg.Method != "" && len(msg.ID) == 0:
			// Notification.
			if c.onNotif != nil {
				c.onNotif(msg.Method, msg.Params)
			}
		case msg.Method != "" && len(msg.ID) > 0:
			// Request from agent. The thin cut doesn't service any;
			// reply with method-not-found so the agent isn't stuck.
			_ = c.writeFrame(rawMessage{
				JSONRPC: jsonRPCVersion,
				ID:      msg.ID,
				Error: &RPCError{
					Code:    -32601,
					Message: "method not found",
				},
			})
		case len(msg.ID) > 0:
			// Response — route by numeric ID.
			var id int64
			if err := json.Unmarshal(msg.ID, &id); err != nil {
				continue
			}
			if v, ok := c.pending.LoadAndDelete(id); ok {
				v.(chan *rawMessage) <- &msg
			}
		}
	}
	if err := scanner.Err(); err != nil {
		c.readErr.Store(err)
	}
}

func marshalOrNil(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		// Encoding failure on our own params is a programmer bug; surface
		// it as a null payload and let the agent reject it.
		return json.RawMessage("null")
	}
	return b
}

// ---- typed wrappers for the four methods we actually use ----

// Initialize performs the "initialize" handshake.
func (c *Client) Initialize(ctx context.Context, req InitializeRequest) (*InitializeResponse, error) {
	var resp InitializeResponse
	if err := c.Call(ctx, "initialize", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// NewSession opens a new session on the agent.
func (c *Client) NewSession(ctx context.Context, req NewSessionRequest) (*NewSessionResponse, error) {
	var resp NewSessionResponse
	if err := c.Call(ctx, "session/new", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Prompt sends a user message into the session and blocks until the
// agent returns a stop reason. Assistant text chunks arrive on the
// notification handler concurrently — the caller is expected to have
// installed one that captures them.
func (c *Client) Prompt(ctx context.Context, req PromptRequest) (*PromptResponse, error) {
	var resp PromptResponse
	if err := c.Call(ctx, "session/prompt", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
