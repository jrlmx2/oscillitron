// CLAUDE GENERATED
// Package acp implements the minimum subset of the Agent Client
// Protocol (https://agentclientprotocol.com) needed to drive a single
// long-lived Hermes specialist: initialize, session/new, session/prompt,
// and the session/update notification stream.
//
// Wire format assumption: newline-delimited JSON-RPC 2.0 over stdio
// (one JSON object per line, terminated by '\n'). The ACP spec does
// not explicitly pin framing in its overview docs; this assumption is
// what the integration test under internal/test/hermes validates
// against a real Hermes binary. If Hermes uses LSP-style
// Content-Length framing instead, swap the codec here.
package acp

import "encoding/json"

// ProtocolVersion is a uint16 per the ACP schema.
type ProtocolVersion uint16

// CurrentProtocolVersion is the version this client negotiates.
// Bump in lockstep with the Hermes ACP server we're talking to.
const CurrentProtocolVersion ProtocolVersion = 1

// Implementation identifies a client or agent in the initialize handshake.
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Title   string `json:"title,omitempty"`
}

// FileSystemCapabilities advertises filesystem hooks the client offers.
// Oscillitron offers none in the thin cut.
type FileSystemCapabilities struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

// ClientCapabilities is what the client (Oscillitron) advertises.
type ClientCapabilities struct {
	FS       FileSystemCapabilities `json:"fs"`
	Terminal bool                   `json:"terminal"`
}

// InitializeRequest is the params of the "initialize" method.
type InitializeRequest struct {
	ProtocolVersion    ProtocolVersion    `json:"protocolVersion"`
	ClientInfo         *Implementation    `json:"clientInfo,omitempty"`
	ClientCapabilities ClientCapabilities `json:"clientCapabilities"`
}

// InitializeResponse is the result of the "initialize" method. Most
// fields are parsed leniently — the thin cut only needs to confirm
// the agent agreed to the protocol version.
type InitializeResponse struct {
	ProtocolVersion ProtocolVersion `json:"protocolVersion"`
	AgentInfo       *Implementation `json:"agentInfo,omitempty"`
	// AgentCapabilities and AuthMethods retained as raw JSON for now;
	// the thin cut doesn't branch on them. Promote to typed fields when
	// we need to inspect them.
	AgentCapabilities json.RawMessage `json:"agentCapabilities,omitempty"`
	AuthMethods       json.RawMessage `json:"authMethods,omitempty"`
}

// NewSessionRequest is the params of the "session/new" method.
type NewSessionRequest struct {
	Cwd        string            `json:"cwd"`
	McpServers []json.RawMessage `json:"mcpServers"`
}

// NewSessionResponse is the result of the "session/new" method.
type NewSessionResponse struct {
	SessionID string `json:"sessionId"`
}

// ContentBlock is the polymorphic content element used in prompts and
// chunk notifications. Only "text" is populated in the thin cut.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// PromptRequest is the params of the "session/prompt" method.
type PromptRequest struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

// StopReason values per the ACP schema. Used by the adapter to map to
// session.ExitReason.
type StopReason string

const (
	StopReasonEndTurn   StopReason = "end_turn"
	StopReasonCancelled StopReason = "cancelled"
	StopReasonRefusal   StopReason = "refusal"
	StopReasonToolUse   StopReason = "tool_use"
)

// PromptResponse is the result of the "session/prompt" method.
type PromptResponse struct {
	StopReason StopReason `json:"stopReason"`
}

// SessionNotification is the params of the "session/update"
// notification (agent -> client). The Update field is a discriminated
// union keyed on `sessionUpdate`; we keep it as RawMessage and let
// callers decode the variants they care about.
type SessionNotification struct {
	SessionID string          `json:"sessionId"`
	Update    json.RawMessage `json:"update"`
}

// AgentMessageChunk is the variant of SessionNotification.Update that
// carries an assistant-side text fragment during a prompt. Discriminator
// value: "agent_message_chunk".
type AgentMessageChunk struct {
	SessionUpdate string       `json:"sessionUpdate"`
	Content       ContentBlock `json:"content"`
}

// UsageUpdate is the variant of SessionNotification.Update that
// reports context-window utilization. Discriminator value:
// "usage_update". Per the ACP schema this carries {size, used} — the
// agent's context window and current fill, intended for editor UIs
// (the context-bar in Zed et al.). It does NOT carry per-turn input
// and output token counts — ACP has no such notification. For cost
// accounting the Hermes adapter estimates tokens client-side from
// the request and response text length.
type UsageUpdate struct {
	SessionUpdate string `json:"sessionUpdate"`
	Size          int    `json:"size"` // total context window
	Used          int    `json:"used"` // tokens used so far
}
