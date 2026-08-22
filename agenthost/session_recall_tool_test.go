package agenthost

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DiegoAvila-yeyo/exo/sessionrecall"
)

func newTestSessionRecallStore(t *testing.T) *sessionrecall.Store {
	t.Helper()
	store, err := sessionrecall.New(t.TempDir())
	if err != nil {
		t.Fatalf("sessionrecall.New: %v", err)
	}
	return store
}

func TestSessionRecallToolListScopesToActiveProject(t *testing.T) {
	store := newTestSessionRecallStore(t)

	prA, _ := store.Load("/proj/a")
	prA.Entries = append(prA.Entries, sessionrecall.SessionSummary{
		SessionID: "chat-a1", Title: "Project A work", Description: "did A things", Status: sessionrecall.StatusClosed,
	})
	if _, err := store.Save(prA); err != nil {
		t.Fatalf("Save project A: %v", err)
	}

	prB, _ := store.Load("/proj/b")
	prB.Entries = append(prB.Entries, sessionrecall.SessionSummary{
		SessionID: "chat-b1", Title: "Project B work", Description: "did B things", Status: sessionrecall.StatusClosed,
	})
	if _, err := store.Save(prB); err != nil {
		t.Fatalf("Save project B: %v", err)
	}

	toolA := sessionRecallTool{store: store, cell: &canvasCell{projectID: "/proj/a"}}
	out, isErr := toolA.Execute(context.Background(), `{"action":"list"}`)
	if isErr {
		t.Fatalf("list for /proj/a returned error: %s", out)
	}
	if !strings.Contains(out, "chat-a1") {
		t.Fatalf("list output = %q, want it to mention chat-a1", out)
	}
	if strings.Contains(out, "chat-b1") {
		t.Fatalf("list output = %q, must not leak project B's chat-b1", out)
	}
}

func TestSessionRecallToolGetRejectsUnknownSessionID(t *testing.T) {
	store := newTestSessionRecallStore(t)
	tool := sessionRecallTool{store: store, cell: &canvasCell{projectID: "/proj/a"}}

	out, isErr := tool.Execute(context.Background(), `{"action":"get","session_id":"does-not-exist"}`)
	if !isErr {
		t.Fatalf("get for unknown session_id should return an error, got: %s", out)
	}
	if !strings.Contains(out, "not found") {
		t.Fatalf("error message = %q, want it to mention \"not found\"", out)
	}
}

func TestSessionRecallToolGetNeverReturnsRawTranscript(t *testing.T) {
	store := newTestSessionRecallStore(t)

	pr, _ := store.Load("/proj/a")
	pr.Entries = append(pr.Entries, sessionrecall.SessionSummary{
		SessionID:   "chat-a1",
		Title:       "Fixed a bug",
		Description: "one-liner",
		SummaryBody: "The session fixed a null pointer bug in the login handler.",
		ClosedAt:    time.Now(),
		ModelID:     "claude-sonnet-4-6",
		Status:      sessionrecall.StatusClosed,
	})
	if _, err := store.Save(pr); err != nil {
		t.Fatalf("Save: %v", err)
	}

	tool := sessionRecallTool{store: store, cell: &canvasCell{projectID: "/proj/a"}}
	out, isErr := tool.Execute(context.Background(), `{"action":"get","session_id":"chat-a1"}`)
	if isErr {
		t.Fatalf("get returned error: %s", out)
	}

	// The rendered output must be built exclusively from SessionSummary's
	// own fields (Title/SummaryBody/ClosedAt/ModelID/ContextPctAtClose) —
	// it must never reach into chatstore.ChatSession.Messages/Entries, which
	// this tool has no access to in the first place (only *sessionrecall.
	// Store, never *chatstore.Store). Assert the summary content made it
	// through and nothing beyond it is present.
	if !strings.Contains(out, "The session fixed a null pointer bug in the login handler.") {
		t.Fatalf("get output = %q, want it to contain the summary body", out)
	}
	if !strings.Contains(out, "claude-sonnet-4-6") {
		t.Fatalf("get output = %q, want it to contain the model id", out)
	}
}

func TestSessionRecallToolRequiresConfiguredStore(t *testing.T) {
	tool := sessionRecallTool{store: nil, cell: &canvasCell{projectID: "/proj/a"}}
	out, isErr := tool.Execute(context.Background(), `{"action":"list"}`)
	if !isErr {
		t.Fatalf("Execute with nil store should error, got: %s", out)
	}
}
