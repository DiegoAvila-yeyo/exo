package termserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/DiegoAvila-yeyo/exo/chatstore"
	"github.com/yeyoos/nucleo-base/shared/api"
)

func TestChatSessionsCreateAppearsInList(t *testing.T) {
	store := newFakeStore()
	chats := newTestChatStore(t)
	_, server, httpServer := newTestServer(t, store, WithChatStore(chats))
	client, csrf := bootstrapClient(t, httpServer, server)

	created := postChatSession(t, client, httpServer, server, csrf)
	if created.Title != chatstore.DefaultTitle {
		t.Fatalf("created title = %q, want %q", created.Title, chatstore.DefaultTitle)
	}

	list := getChatSessions(t, client, httpServer, server)
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("list = %+v, want single session %q", list, created.ID)
	}
}

func TestChatPostLazilyCreatesSessionAndDerivesTitleFromFirstMessage(t *testing.T) {
	store := newFakeStore()
	chats := newTestChatStore(t)
	runner := func(_ context.Context, _ string, _ []api.Message, _ string, _ string, _ string, _ string) ([]api.Message, *NavigateAction, *CanvasSuggestion, error) {
		return []api.Message{{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: "hi"}}}}, nil, nil, nil
	}
	_, server, httpServer := newTestServer(t, store, WithAgentRunner(runner), WithChatStore(chats))
	client, csrf := bootstrapClient(t, httpServer, server)

	sessionID := startChatWithoutSessionID(t, client, httpServer, server, csrf, "please rename the project's README file")

	session := waitForChatSession(t, chats, sessionID, func(s chatstore.ChatSession) bool {
		return s.Title != chatstore.DefaultTitle
	})
	if session.Title != "please rename the project's README file" {
		t.Fatalf("title = %q, want the first message verbatim (under the truncation limit)", session.Title)
	}
	if len(session.Entries) == 0 || session.Entries[0].Kind != "system" {
		t.Fatalf("entries = %+v, want first entry to be the user's own line", session.Entries)
	}
}

func TestChatSessionsIsolateHistoryBetweenSessions(t *testing.T) {
	store := newFakeStore()
	chats := newTestChatStore(t)

	var receivedHistories [][]api.Message
	runner := func(_ context.Context, _ string, history []api.Message, _ string, _ string, _ string, _ string) ([]api.Message, *NavigateAction, *CanvasSuggestion, error) {
		receivedHistories = append(receivedHistories, history)
		return append(append([]api.Message{}, history...), api.Message{
			Role: api.RoleAssistant, Content: []api.Block{{Type: api.BlockText, Text: "ack"}},
		}), nil, nil, nil
	}
	_, server, httpServer := newTestServer(t, store, WithAgentRunner(runner), WithChatStore(chats))
	client, csrf := bootstrapClient(t, httpServer, server)

	sessionA := postChatSession(t, client, httpServer, server, csrf)
	waitUntilIdle(t, server)
	postChatMessage(t, client, httpServer, server, csrf, sessionA.ID, "first session message")
	waitForChatSession(t, chats, sessionA.ID, func(s chatstore.ChatSession) bool { return len(s.Messages) > 0 })

	sessionB := postChatSession(t, client, httpServer, server, csrf)
	waitUntilIdle(t, server)
	postChatMessage(t, client, httpServer, server, csrf, sessionB.ID, "second session message")
	waitForChatSession(t, chats, sessionB.ID, func(s chatstore.ChatSession) bool { return len(s.Messages) > 0 })

	// Session B's turn must have started from an empty history — not
	// session A's — proving turns don't leak conversation state across
	// sessions.
	if len(receivedHistories) != 2 {
		t.Fatalf("runner invocations = %d, want 2", len(receivedHistories))
	}
	if len(receivedHistories[1]) != 0 {
		t.Fatalf("session B's starting history = %+v, want empty (isolated from session A)", receivedHistories[1])
	}

	loadedA, err := chats.Load(sessionA.ID)
	if err != nil {
		t.Fatalf("load session A failed: %v", err)
	}
	loadedB, err := chats.Load(sessionB.ID)
	if err != nil {
		t.Fatalf("load session B failed: %v", err)
	}
	if len(loadedA.Messages) != 1 || len(loadedB.Messages) != 1 {
		t.Fatalf("session A messages = %d, session B messages = %d, want 1 each", len(loadedA.Messages), len(loadedB.Messages))
	}
}

func TestChatPostThreadsProjectPathToRunnerAndPersistsIt(t *testing.T) {
	store := newFakeStore()
	chats := newTestChatStore(t)

	var receivedPaths []string
	runner := func(_ context.Context, _ string, _ []api.Message, projectPath string, _ string, _ string, _ string) ([]api.Message, *NavigateAction, *CanvasSuggestion, error) {
		receivedPaths = append(receivedPaths, projectPath)
		return nil, nil, nil, nil
	}
	_, server, httpServer := newTestServer(t, store, WithAgentRunner(runner), WithChatStore(chats))
	client, csrf := bootstrapClient(t, httpServer, server)

	session := postChatSession(t, client, httpServer, server, csrf)

	// First message picks the project — runner must see it immediately.
	postChatMessageWithProject(t, client, httpServer, server, csrf, session.ID, "look around", "/Users/eltitoyeyo/exo")
	waitForChatSession(t, chats, session.ID, func(s chatstore.ChatSession) bool { return s.ProjectPath != "" })
	waitUntilIdle(t, server) // runner's append into receivedPaths happens-before agentMu unlocks

	// Second message omits project_path — the session must keep the one it
	// already picked, not fall back to empty.
	postChatMessage(t, client, httpServer, server, csrf, session.ID, "keep going")
	waitUntilIdle(t, server)

	if len(receivedPaths) != 2 {
		t.Fatalf("runner invocations = %d, want 2", len(receivedPaths))
	}
	if receivedPaths[0] != "/Users/eltitoyeyo/exo" {
		t.Fatalf("first turn projectPath = %q, want %q", receivedPaths[0], "/Users/eltitoyeyo/exo")
	}
	if receivedPaths[1] != "/Users/eltitoyeyo/exo" {
		t.Fatalf("second turn projectPath = %q, want it to persist from the first turn", receivedPaths[1])
	}

	loaded, err := chats.Load(session.ID)
	if err != nil {
		t.Fatalf("load session failed: %v", err)
	}
	if loaded.ProjectPath != "/Users/eltitoyeyo/exo" {
		t.Fatalf("persisted ProjectPath = %q, want %q", loaded.ProjectPath, "/Users/eltitoyeyo/exo")
	}
}

func TestChatSessionDetailReturns404ForUnknownID(t *testing.T) {
	store := newFakeStore()
	chats := newTestChatStore(t)
	_, server, httpServer := newTestServer(t, store, WithChatStore(chats))
	client, _ := bootstrapClient(t, httpServer, server)

	req, err := http.NewRequest(http.MethodGet, httpServer.URL+"/api/chat/sessions/does-not-exist", nil)
	if err != nil {
		t.Fatalf("build request failed: %v", err)
	}
	req.Header.Set("Origin", allowedOrigin(server))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /api/chat/sessions/does-not-exist failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestDeriveChatTitleTruncatesAtWordBoundary(t *testing.T) {
	short := "fix the login bug"
	if got := deriveChatTitle(short); got != short {
		t.Fatalf("deriveChatTitle(%q) = %q, want unchanged", short, got)
	}

	long := "please go through every file in the pacta-pay-rails repository and check for unused imports"
	got := deriveChatTitle(long)
	if len([]rune(got)) > maxChatTitleLen+1 { // +1 for the trailing ellipsis rune
		t.Fatalf("deriveChatTitle(%q) = %q (%d runes), want <= %d", long, got, len([]rune(got)), maxChatTitleLen+1)
	}
	if got[len(got)-len("…"):] != "…" {
		t.Fatalf("deriveChatTitle(%q) = %q, want trailing ellipsis", long, got)
	}
	if got[len(got)-len("y…"):len(got)-len("…")] == " " {
		t.Fatalf("deriveChatTitle(%q) = %q, truncated mid-word", long, got)
	}
}

// --- helpers ---

func newTestChatStore(t *testing.T) *chatstore.Store {
	t.Helper()
	store, err := chatstore.New(filepath.Join(t.TempDir(), "chats"))
	if err != nil {
		t.Fatalf("new chat store failed: %v", err)
	}
	return store
}

func postChatSession(t *testing.T, client *http.Client, httpServer *httptest.Server, server *Server, csrf string) chatstore.ChatSessionSummary {
	t.Helper()
	req := newJSONRequest(t, http.MethodPost, httpServer.URL+"/api/chat/sessions?csrf_token="+url.QueryEscape(csrf), `{}`)
	req.Header.Set("Origin", allowedOrigin(server))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /api/chat/sessions failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/chat/sessions status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	var summary chatstore.ChatSessionSummary
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		t.Fatalf("decode create response failed: %v", err)
	}
	return summary
}

func getChatSessions(t *testing.T, client *http.Client, httpServer *httptest.Server, server *Server) []chatstore.ChatSessionSummary {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, httpServer.URL+"/api/chat/sessions", nil)
	if err != nil {
		t.Fatalf("build request failed: %v", err)
	}
	req.Header.Set("Origin", allowedOrigin(server))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /api/chat/sessions failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/chat/sessions status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var list []chatstore.ChatSessionSummary
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list response failed: %v", err)
	}
	return list
}

func startChatWithoutSessionID(t *testing.T, client *http.Client, httpServer *httptest.Server, server *Server, csrf, message string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"message": message})
	if err != nil {
		t.Fatalf("marshal body failed: %v", err)
	}
	req := newJSONRequest(t, http.MethodPost, httpServer.URL+"/api/chat?csrf_token="+url.QueryEscape(csrf), string(body))
	req.Header.Set("Origin", allowedOrigin(server))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /api/chat failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /api/chat status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
	var decoded struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode chat response failed: %v", err)
	}
	if decoded.SessionID == "" {
		t.Fatal("response missing session_id for lazily-created session")
	}
	return decoded.SessionID
}

func postChatMessage(t *testing.T, client *http.Client, httpServer *httptest.Server, server *Server, csrf, sessionID, message string) {
	t.Helper()
	postChatMessageWithProject(t, client, httpServer, server, csrf, sessionID, message, "")
}

func postChatMessageWithProject(t *testing.T, client *http.Client, httpServer *httptest.Server, server *Server, csrf, sessionID, message, projectPath string) {
	t.Helper()
	payload := map[string]string{"message": message, "session_id": sessionID}
	if projectPath != "" {
		payload["project_path"] = projectPath
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal body failed: %v", err)
	}
	req := newJSONRequest(t, http.MethodPost, httpServer.URL+"/api/chat?csrf_token="+url.QueryEscape(csrf), string(body))
	req.Header.Set("Origin", allowedOrigin(server))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /api/chat failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /api/chat status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
}

// waitUntilIdle blocks until the server's agent mutex is free, so sequential
// test turns don't race each other under the single in-flight-turn rule.
func waitUntilIdle(t *testing.T, server *Server) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if server.agentMu.TryLock() {
			server.agentMu.Unlock()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for agent to go idle")
}

func waitForChatSession(t *testing.T, chats *chatstore.Store, id string, ready func(chatstore.ChatSession) bool) chatstore.ChatSession {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		session, err := chats.Load(id)
		if err == nil && ready(session) {
			return session
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for chat session %q to reach expected state", id)
	return chatstore.ChatSession{}
}
