//go:build hermes_stage5

// CLAUDE GENERATED
package hermes

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// sseDecoder reads Hermes' SSE event stream. It is intentionally
// minimal: Hermes emits one event per "data:" line followed by a
// blank separator, and the only other framing it uses is the SSE
// comment ":" for keepalives and the "stream closed" sentinel. We do
// not need event-name framing, multi-line data accumulation, or
// retry-id support.
type sseDecoder struct {
	r *bufio.Scanner
}

func newSSEDecoder(body io.Reader) *sseDecoder {
	s := bufio.NewScanner(body)
	// SSE lines can carry long JSON payloads (tool args, deltas with
	// embedded code blocks). Bump the buffer ceiling above bufio's
	// default 64KiB.
	s.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	return &sseDecoder{r: s}
}

// event is a parsed Hermes SSE event. Fields that don't apply to the
// event's kind are zero-valued.
type event struct {
	kind  string
	delta string     // message.delta
	text  string     // reasoning.available.text, run.completed.output, run.failed.error, approval.request.tool, run.cancelled (empty)
	usage tokenUsage // run.completed
}

// next returns the next event from the stream. ok=false with err=nil
// means the stream ended cleanly (server-side sentinel close). A ctx
// cancellation surfaces as ctx.Err().
func (d *sseDecoder) next(ctx context.Context) (event, bool, error) {
	for d.r.Scan() {
		if err := ctx.Err(); err != nil {
			return event{}, false, err
		}
		line := d.r.Text()
		if line == "" || strings.HasPrefix(line, ":") {
			// Blank separator or SSE comment (keepalive, stream-close
			// sentinel). Skip — Hermes emits one payload per non-empty
			// "data:" line, so the separator does not delimit events
			// for us.
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			// SSE allows other field names ("event:", "id:", "retry:").
			// Hermes' api_server only emits "data:" lines today; ignore
			// anything else to stay forward-compatible.
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		evt, err := parseEvent(payload)
		if err != nil {
			return event{}, false, err
		}
		return evt, true, nil
	}
	if err := d.r.Err(); err != nil {
		return event{}, false, err
	}
	return event{}, false, nil
}

// parseEvent decodes a single Hermes event payload.
func parseEvent(payload string) (event, error) {
	// Discriminate on the "event" field. The rest of the payload
	// shape depends on the kind.
	var head struct {
		Event string `json:"event"`
	}
	if err := json.Unmarshal([]byte(payload), &head); err != nil {
		return event{}, err
	}
	if head.Event == "" {
		return event{}, errors.New("hermes: SSE payload missing 'event' field")
	}
	evt := event{kind: head.Event}

	switch head.Event {
	case "message.delta":
		var p struct {
			Delta string `json:"delta"`
		}
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return event{}, err
		}
		evt.delta = p.Delta
	case "reasoning.available":
		var p struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return event{}, err
		}
		evt.text = p.Text
	case "approval.request":
		// The api_server merges arbitrary approval_data into the event;
		// we surface a best-effort tool identifier for trace context.
		var p struct {
			Tool string `json:"tool"`
		}
		_ = json.Unmarshal([]byte(payload), &p)
		evt.text = p.Tool
	case "run.completed":
		var p struct {
			Output string `json:"output"`
			Usage  struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
				TotalTokens  int `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return event{}, err
		}
		evt.text = p.Output
		evt.usage = tokenUsage{
			input:  p.Usage.InputTokens,
			output: p.Usage.OutputTokens,
			total:  p.Usage.TotalTokens,
		}
	case "run.failed":
		var p struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return event{}, err
		}
		evt.text = p.Error
	case "tool.started", "tool.completed", "run.cancelled":
		// No fields we currently consume; trace logging in the caller
		// is enough.
	}
	return evt, nil
}
