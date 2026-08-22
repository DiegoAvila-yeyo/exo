package termserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/DiegoAvila-yeyo/exo/chatstore"
	"github.com/DiegoAvila-yeyo/exo/sessionrecall"
	"github.com/yeyoos/nucleo-base/shared/api"
)

func newTestSessionRecallStore(t *testing.T) *sessionrecall.Store {
	t.Helper()
	store, err := sessionrecall.New(t.TempDir())
	if err != nil {
		t.Fatalf("sessionrecall.New: %v", err)
	}
	return store
}

func closeChatSession(t *testing.T, client *http.Client, httpServer *httptest.Server, server *Server, csrf, sessionID string) *http.Response {
	t.Helper()
	req := newJSONRequest(t, http.MethodPost, httpServer.URL+"/api/chat/sessions/"+url.PathEscape(sessionID)+"/close?csrf_token="+url.QueryEscape(csrf), `{}`)
	req.Header.Set("Origin", allowedOrigin(server))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST close session failed: %v", err)
	}
	return resp
}

func TestCloseSessionSequenceLeavesSessionOpenWhenSummaryFails(t *testing.T) {
	store := newFakeStore()
	chats := newTestChatStore(t)
	recall := newTestSessionRecallStore(t)

	summarizerCalls := 0
	failingSummarizer := func(_ context.Context, _ string, _ []api.Message) (string, string, string, error) {
		summarizerCalls++
		return "", "", "", errors.New("summarization failed")
	}

	_, server, httpServer := newTestServer(t, store,
		WithChatStore(chats),
		WithSessionRecallStore(recall),
		WithSessionSummarizer(failingSummarizer),
	)
	client, csrf := bootstrapClient(t, httpServer, server)

	session := postChatSession(t, client, httpServer, server, csrf)

	resp := closeChatSession(t, client, httpServer, server, csrf, session.ID)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("close status = %d, want %d (summarization failure)", resp.StatusCode, http.StatusInternalServerError)
	}
	if summarizerCalls != 1 {
		t.Fatalf("summarizer calls = %d, want 1", summarizerCalls)
	}

	loaded, err := chats.Load(session.ID)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if loaded.Status == chatstore.StatusClosed {
		t.Fatal("session was marked closed despite a failed summarization call")
	}

	pr, err := recall.Load("")
	if err != nil {
		t.Fatalf("load recall: %v", err)
	}
	if len(pr.Entries) != 0 {
		t.Fatalf("recall entries = %d, want 0 (no summary was ever produced)", len(pr.Entries))
	}
}

func TestCloseSessionIsIdempotent(t *testing.T) {
	store := newFakeStore()
	chats := newTestChatStore(t)
	recall := newTestSessionRecallStore(t)

	summarizerCalls := 0
	summarizer := func(_ context.Context, _ string, _ []api.Message) (string, string, string, error) {
		summarizerCalls++
		return "Session title", "one-liner", "full summary body", nil
	}

	_, server, httpServer := newTestServer(t, store,
		WithChatStore(chats),
		WithSessionRecallStore(recall),
		WithSessionSummarizer(summarizer),
	)
	client, csrf := bootstrapClient(t, httpServer, server)

	session := postChatSession(t, client, httpServer, server, csrf)

	resp1 := closeChatSession(t, client, httpServer, server, csrf, session.ID)
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first close status = %d, want %d", resp1.StatusCode, http.StatusOK)
	}

	resp2 := closeChatSession(t, client, httpServer, server, csrf, session.ID)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second close status = %d, want %d", resp2.StatusCode, http.StatusOK)
	}

	// The second close must be a no-op success: no second summarization
	// call, no duplicate recall entry.
	if summarizerCalls != 1 {
		t.Fatalf("summarizer calls = %d, want 1 (second close must not re-summarize)", summarizerCalls)
	}

	loaded, err := chats.Load(session.ID)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if loaded.Status != chatstore.StatusClosed {
		t.Fatalf("session status = %q, want %q after both closes", loaded.Status, chatstore.StatusClosed)
	}

	pr, err := recall.Load("")
	if err != nil {
		t.Fatalf("load recall: %v", err)
	}
	if len(pr.Entries) != 1 {
		t.Fatalf("recall entries = %d, want 1 (closing twice must not duplicate)", len(pr.Entries))
	}
	if pr.Entries[0].SessionID != session.ID {
		t.Fatalf("recall entry session id = %q, want %q", pr.Entries[0].SessionID, session.ID)
	}
}

func TestClosedSessionRejectsNewMessages(t *testing.T) {
	store := newFakeStore()
	chats := newTestChatStore(t)
	recall := newTestSessionRecallStore(t)

	summarizer := func(_ context.Context, _ string, _ []api.Message) (string, string, string, error) {
		return "Session title", "one-liner", "full summary body", nil
	}

	runnerCalls := 0
	runner := func(_ context.Context, _ string, _ []api.Message, _ string, _ string, _ string, _ string) ([]api.Message, *NavigateAction, *CanvasSuggestion, *TurnUsage, error) {
		runnerCalls++
		return nil, nil, nil, nil, nil
	}

	_, server, httpServer := newTestServer(t, store,
		WithChatStore(chats),
		WithAgentRunner(runner),
		WithSessionRecallStore(recall),
		WithSessionSummarizer(summarizer),
	)
	client, csrf := bootstrapClient(t, httpServer, server)

	session := postChatSession(t, client, httpServer, server, csrf)

	resp := closeChatSession(t, client, httpServer, server, csrf, session.ID)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("close status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, err := json.Marshal(map[string]string{"message": "please continue", "session_id": session.ID})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := newJSONRequest(t, http.MethodPost, httpServer.URL+"/api/chat?csrf_token="+url.QueryEscape(csrf), string(body))
	req.Header.Set("Origin", allowedOrigin(server))
	req.Header.Set("Content-Type", "application/json")
	chatResp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /api/chat failed: %v", err)
	}
	defer chatResp.Body.Close()

	if chatResp.StatusCode == http.StatusAccepted {
		t.Fatalf("POST /api/chat against a closed session status = %d, want a rejection", chatResp.StatusCode)
	}
	if runnerCalls != 0 {
		t.Fatalf("runner calls = %d, want 0 (a closed session must never reach the runner)", runnerCalls)
	}
}
