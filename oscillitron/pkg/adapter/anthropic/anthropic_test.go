// CLAUDE GENERATED
package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

// mockAnthropic returns an httptest server that replies with the
// supplied text wrapped in a Messages API envelope. Captures the
// last request body for assertions.
func mockAnthropic(t *testing.T, replyText string) (*httptest.Server, *string) {
	t.Helper()
	var lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		lastBody = string(b)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_x", "model": "test-model", "role": "assistant", "type": "message",
			"content": []map[string]any{{"type": "text", "text": replyText}},
			"usage":   map[string]int{"input_tokens": 50, "output_tokens": 20},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &lastBody
}

func mkEnv(content string) session.Envelope {
	return session.NewRoot("ap-1", content, "", "", session.Budget{DepthRemaining: 3})
}

func TestEvaluate_ParsesPlaybookFromAPI(t *testing.T) {
	srv, _ := mockAnthropic(t, `{"playbook":"process","rationale":"single task","confidence":0.85}`)
	a, err := New(Config{APIKey: "sk-x", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	env, err := a.Evaluate(context.Background(), mkEnv("draft an email"))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if env.Evaluate == nil {
		t.Fatal("Evaluate field not populated")
	}
	if env.Evaluate.Playbook != session.PlaybookProcess {
		t.Errorf("Playbook = %q, want process", env.Evaluate.Playbook)
	}
	if env.Evaluate.Confidence != 0.85 {
		t.Errorf("Confidence = %v, want 0.85", env.Evaluate.Confidence)
	}
	if env.Evaluate.TokensUsed != 70 {
		t.Errorf("TokensUsed = %d, want 70 (50+20)", env.Evaluate.TokensUsed)
	}
}

func TestEvaluate_RejectsUnknownPlaybook(t *testing.T) {
	srv, _ := mockAnthropic(t, `{"playbook":"foobar","confidence":0.5}`)
	a, _ := New(Config{APIKey: "sk-x", BaseURL: srv.URL})
	if _, err := a.Evaluate(context.Background(), mkEnv("x")); err == nil {
		t.Fatal("expected error on unknown playbook")
	}
}

func TestExecute_Process_ParsesReturnResult(t *testing.T) {
	srv, _ := mockAnthropic(t, `{"content":"here is the draft email","confidence":0.9}`)
	a, _ := New(Config{APIKey: "sk-x", BaseURL: srv.URL})
	env := mkEnv("draft an email")
	env.Evaluate = &session.Evaluate{Playbook: session.PlaybookProcess}
	out, err := a.Execute(context.Background(), env)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Execute == nil || out.Execute.Category != session.CategoryReturnResult {
		t.Fatalf("Execute Category = %+v, want return_result", out.Execute)
	}
	if out.Execute.ReturnResult.Result.Content != "here is the draft email" {
		t.Errorf("content = %q", out.Execute.ReturnResult.Result.Content)
	}
	if out.Execute.ReturnResult.Confidence != 0.9 {
		t.Errorf("confidence = %v", out.Execute.ReturnResult.Confidence)
	}
}

func TestExecute_Plan_ParsesSubAPs(t *testing.T) {
	srv, _ := mockAnthropic(t,
		`{"sub_aps":[{"input":"step 1","needs_verification":true},{"input":"step 2"}],"recompose":"sequential"}`)
	a, _ := New(Config{APIKey: "sk-x", BaseURL: srv.URL})
	env := mkEnv("plan this")
	env.Evaluate = &session.Evaluate{Playbook: session.PlaybookPlan}
	out, err := a.Execute(context.Background(), env)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Execute == nil || out.Execute.Category != session.CategoryEmitSubtree {
		t.Fatalf("Category = %+v, want emit_subtree", out.Execute)
	}
	if len(out.Execute.EmitSubtree.SubAPs) != 2 {
		t.Errorf("sub_aps = %d, want 2", len(out.Execute.EmitSubtree.SubAPs))
	}
	if !out.Execute.EmitSubtree.SubAPs[0].NeedsVerification {
		t.Errorf("first sub_ap should NeedsVerification=true")
	}
	if out.Execute.EmitSubtree.Recompose != session.RecomposeSequential {
		t.Errorf("recompose = %q, want sequential", out.Execute.EmitSubtree.Recompose)
	}
}

func TestExecute_Critique_ParsesVerifierSignal(t *testing.T) {
	srv, _ := mockAnthropic(t,
		`{"verdict":"issues","issues":[{"severity":"warning","what":"tone is too formal"}]}`)
	a, _ := New(Config{APIKey: "sk-x", BaseURL: srv.URL})
	env := mkEnv("critique this draft")
	env.Evaluate = &session.Evaluate{Playbook: session.PlaybookCritique}
	out, err := a.Execute(context.Background(), env)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Execute == nil || out.Execute.Category != session.CategoryVerifierSignal {
		t.Fatalf("Category = %+v, want verifier_signal", out.Execute)
	}
	if out.Execute.VerifierSignal.Verdict != session.VerdictIssues {
		t.Errorf("verdict = %q, want issues", out.Execute.VerifierSignal.Verdict)
	}
	if len(out.Execute.VerifierSignal.Issues) != 1 {
		t.Errorf("issues = %d, want 1", len(out.Execute.VerifierSignal.Issues))
	}
}

func TestExecute_RequiresPriorEvaluate(t *testing.T) {
	a, _ := New(Config{APIKey: "sk-x"})
	env := mkEnv("x")
	if _, err := a.Execute(context.Background(), env); err == nil {
		t.Fatal("expected error when Execute called before Evaluate")
	}
}

func TestExecute_TolerantsMarkdownFences(t *testing.T) {
	srv, _ := mockAnthropic(t,
		"Sure, here is the draft:\n```json\n{\"content\":\"hello\",\"confidence\":0.7}\n```")
	a, _ := New(Config{APIKey: "sk-x", BaseURL: srv.URL})
	env := mkEnv("x")
	env.Evaluate = &session.Evaluate{Playbook: session.PlaybookProcess}
	out, err := a.Execute(context.Background(), env)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Execute.ReturnResult.Result.Content != "hello" {
		t.Errorf("markdown-fenced JSON should still parse; got %q", out.Execute.ReturnResult.Result.Content)
	}
}

func TestExecute_RoutesAllFiveV0Playbooks(t *testing.T) {
	// Smoke that every v0 playbook has a system prompt registered and
	// produces the right Execute category.
	cases := []struct {
		pb      session.Playbook
		reply   string
		wantCat session.Category
	}{
		{session.PlaybookPlan, `{"sub_aps":[],"recompose":"none"}`, session.CategoryEmitSubtree},
		{session.PlaybookProcess, `{"content":"x","confidence":0.5}`, session.CategoryReturnResult},
		{session.PlaybookCritique, `{"verdict":"pass"}`, session.CategoryVerifierSignal},
		{session.PlaybookVerifyGrounded, `{"verdict":"pass"}`, session.CategoryVerifierSignal},
		{session.PlaybookCompose, `{"content":"merged","confidence":0.8}`, session.CategoryReturnResult},
	}
	for _, c := range cases {
		c := c
		t.Run(string(c.pb), func(t *testing.T) {
			srv, _ := mockAnthropic(t, c.reply)
			a, _ := New(Config{APIKey: "sk-x", BaseURL: srv.URL})
			env := mkEnv("x")
			env.Evaluate = &session.Evaluate{Playbook: c.pb}
			out, err := a.Execute(context.Background(), env)
			if err != nil {
				t.Fatalf("Execute %s: %v", c.pb, err)
			}
			if out.Execute.Category != c.wantCat {
				t.Errorf("%s: Category = %q, want %q", c.pb, out.Execute.Category, c.wantCat)
			}
		})
	}
}
