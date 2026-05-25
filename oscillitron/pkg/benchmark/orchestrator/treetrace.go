package orchestrator

import (
	"fmt"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/runner"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// TreeTrace is a minimal reconstruction of a tree execution for
// debugging. Each node captures the prompt that went in, the
// playbook that ran, and the output that came out. The tree
// structure is preserved via Children.
type TreeTrace struct {
	CaseID   string         `json:"case_id"`
	Expected string         `json:"expected"`
	Goal     string         `json:"goal,omitempty"`
	Root     TreeTraceNode  `json:"root"`
	Final    TreeTraceFinal `json:"final"`
}

// TreeTraceNode is one AP in the tree.
type TreeTraceNode struct {
	ID       string `json:"id"`
	Playbook string `json:"playbook"`
	Category string `json:"category"`

	// Input is the prompt/task this node received.
	Input string `json:"input"`
	// OutputSchema propagated to this node.
	OutputSchema string `json:"output_schema,omitempty"`

	// Output depends on category:
	//   return_result  → the result content
	//   verifier_signal → "verdict: pass/fail/issues"
	//   emit_subtree   → "N children, recompose=X"
	Output     string  `json:"output"`
	Confidence float64 `json:"confidence"`

	Children []TreeTraceNode `json:"children,omitempty"`
}

// TreeTraceFinal is the recomposed output.
type TreeTraceFinal struct {
	Raw        string  `json:"raw"`
	Extracted  string  `json:"extracted"`
	Confidence float64 `json:"confidence"`
	Calls      int     `json:"calls"`
}

// BuildTreeTrace reconstructs the tree from a runner.Result.
func BuildTreeTrace(caseID, expected, goal string, res runner.Result, extracted string) TreeTrace {
	root := buildNode(res.Root, res.Subtree)
	return TreeTrace{
		CaseID:   caseID,
		Expected: expected,
		Goal:     goal,
		Root:     root,
		Final: TreeTraceFinal{
			Raw:        res.ResolvedPayload.Result.Content,
			Extracted:  extracted,
			Confidence: res.ResolvedPayload.Confidence,
			Calls:      res.State.ExecuteCount + res.State.EvaluateCount,
		},
	}
}

func buildNode(env session.Envelope, subtree map[session.ID][]session.Envelope) TreeTraceNode {
	node := TreeTraceNode{
		ID:           string(env.ID),
		Input:        env.Input.Content,
		OutputSchema: env.OutputSchema,
	}

	if env.Evaluate != nil {
		node.Playbook = string(env.Evaluate.Playbook)
		node.Confidence = env.Evaluate.Confidence
	}

	if env.Execute != nil {
		node.Category = string(env.Execute.Category)
		switch {
		case env.Execute.ReturnResult != nil:
			node.Output = env.Execute.ReturnResult.Result.Content
			node.Confidence = env.Execute.ReturnResult.Confidence
		case env.Execute.VerifierSignal != nil:
			vs := env.Execute.VerifierSignal
			node.Output = fmt.Sprintf("verdict: %s", vs.Verdict)
			if len(vs.Issues) > 0 {
				parts := make([]string, 0, len(vs.Issues))
				for _, iss := range vs.Issues {
					parts = append(parts, fmt.Sprintf("[%s] %s: %s", iss.Severity, iss.Where, iss.What))
				}
				node.Output += " | " + strings.Join(parts, "; ")
			}
		case env.Execute.EmitSubtree != nil:
			es := env.Execute.EmitSubtree
			node.Output = fmt.Sprintf("%d children, recompose=%s", len(es.SubAPs), es.Recompose)
		}
	}

	children, ok := subtree[env.ID]
	if ok && len(children) > 0 {
		node.Children = make([]TreeTraceNode, 0, len(children))
		for _, child := range children {
			node.Children = append(node.Children, buildNode(child, subtree))
		}
	}

	return node
}

// RenderText produces a human-readable indented text trace.
func (t TreeTrace) RenderText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "=== %s === expected=%s\n", t.CaseID, t.Expected)
	if t.Goal != "" {
		fmt.Fprintf(&b, "GOAL: %s\n", t.Goal)
	}
	b.WriteString("\n")
	renderNode(&b, t.Root, 0)
	b.WriteString("\n")
	fmt.Fprintf(&b, "FINAL: %s\n", truncate(t.Final.Raw, 300))
	fmt.Fprintf(&b, "  extracted=%q expected=%q calls=%d conf=%.2f\n",
		t.Final.Extracted, t.Expected, t.Final.Calls, t.Final.Confidence)
	return b.String()
}

func renderNode(b *strings.Builder, node TreeTraceNode, depth int) {
	indent := strings.Repeat("  ", depth)
	shortID := node.ID
	if i := strings.LastIndex(shortID, "-c"); i >= 0 {
		shortID = shortID[i+1:]
	}
	if depth == 0 {
		shortID = "ROOT"
	}

	fmt.Fprintf(b, "%s[%s] %s → %s\n", indent, shortID, node.Playbook, node.Category)
	fmt.Fprintf(b, "%s  in:  %s\n", indent, truncate(node.Input, 200))
	fmt.Fprintf(b, "%s  out: %s\n", indent, truncate(node.Output, 200))
	if node.Confidence > 0 {
		fmt.Fprintf(b, "%s  conf: %.2f\n", indent, node.Confidence)
	}

	for _, child := range node.Children {
		renderNode(b, child, depth+1)
	}
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
