package termserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/yeyoos/nucleo-base/shared/api"
)

func strPtr(s string) *string { return &s }

// TestResolvePlanningContextStateTable exercises every row of Round 2's
// atomic (planning_id, board_id) state table directly — see
// build_prompt_PLANNING_ROUND2.md's "Context lifecycle" section.
func TestResolvePlanningContextStateTable(t *testing.T) {
	ps := newTestPlanningStore(t)
	planning, err := ps.Create("Exo")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	board := planning.AddBoard("Vision")
	if err := ps.Save(planning); err != nil {
		t.Fatalf("Save: %v", err)
	}
	otherPlanning, err := ps.Create("Other")
	if err != nil {
		t.Fatalf("Create other: %v", err)
	}

	tests := []struct {
		name                                string
		existingPlanningID, existingBoardID string
		planningID, boardID                 *string
		wantPlanningID, wantBoardID         string
		wantErr                             bool
	}{
		{
			name:               "both omitted preserves existing",
			existingPlanningID: planning.ID, existingBoardID: board.ID,
			planningID: nil, boardID: nil,
			wantPlanningID: planning.ID, wantBoardID: board.ID,
		},
		{
			name:           "both non-empty sets context",
			planningID:     strPtr(planning.ID),
			boardID:        strPtr(board.ID),
			wantPlanningID: planning.ID,
			wantBoardID:    board.ID,
		},
		{
			name:               "both explicit empty clears",
			existingPlanningID: planning.ID, existingBoardID: board.ID,
			planningID: strPtr(""), boardID: strPtr(""),
			wantPlanningID: "", wantBoardID: "",
		},
		{
			name:               "planning omitted, board present is invalid",
			existingPlanningID: planning.ID, existingBoardID: board.ID,
			planningID: nil, boardID: strPtr(board.ID),
			wantErr: true,
		},
		{
			name:               "planning present, board omitted is invalid",
			existingPlanningID: planning.ID, existingBoardID: board.ID,
			planningID: strPtr(planning.ID), boardID: nil,
			wantErr: true,
		},
		{
			// Planning-only context is valid on its own — being "in a
			// Planning" with no Board open is a real state (e.g. a Planning
			// with no Boards yet), needed so planning_create_board can
			// bootstrap the first one.
			name:           "planning set, board explicit empty is valid planning-only context",
			planningID:     strPtr(planning.ID),
			boardID:        strPtr(""),
			wantPlanningID: planning.ID,
			wantBoardID:    "",
		},
		{
			name:               "planning empty, board set is invalid",
			existingPlanningID: planning.ID, existingBoardID: board.ID,
			planningID: strPtr(""), boardID: strPtr(board.ID),
			wantErr: true,
		},
		{
			name:       "board does not belong to planning",
			planningID: strPtr(otherPlanning.ID), boardID: strPtr(board.ID),
			wantErr: true,
		},
		{
			name:       "unknown planning id",
			planningID: strPtr("does-not-exist"), boardID: strPtr(board.ID),
			wantErr: true,
		},
		{
			name:       "unknown planning id, planning-only",
			planningID: strPtr("does-not-exist"), boardID: strPtr(""),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotP, gotB, err := resolvePlanningContext(ps, tc.existingPlanningID, tc.existingBoardID, tc.planningID, tc.boardID)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolvePlanningContext: got nil error, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolvePlanningContext: unexpected error: %v", err)
			}
			if gotP != tc.wantPlanningID || gotB != tc.wantBoardID {
				t.Fatalf("resolvePlanningContext = (%q, %q), want (%q, %q)", gotP, gotB, tc.wantPlanningID, tc.wantBoardID)
			}
		})
	}
}

func TestResolvePlanningContextNilStoreRejectsRealPair(t *testing.T) {
	_, _, err := resolvePlanningContext(nil, "", "", strPtr("planning-x"), strPtr("board-y"))
	if err == nil {
		t.Fatal("expected error when store is nil and a real pair is requested")
	}
}

func TestResolvePlanningContextNilStoreRejectsPlanningOnly(t *testing.T) {
	_, _, err := resolvePlanningContext(nil, "", "", strPtr("planning-x"), strPtr(""))
	if err == nil {
		t.Fatal("expected error when store is nil and a planning-only context is requested")
	}
}

func TestResolvePlanningContextNilStoreAllowsClearAndPreserve(t *testing.T) {
	if _, _, err := resolvePlanningContext(nil, "a", "b", nil, nil); err != nil {
		t.Fatalf("preserve with nil store: unexpected error: %v", err)
	}
	gotP, gotB, err := resolvePlanningContext(nil, "a", "b", strPtr(""), strPtr(""))
	if err != nil {
		t.Fatalf("clear with nil store: unexpected error: %v", err)
	}
	if gotP != "" || gotB != "" {
		t.Fatalf("clear with nil store = (%q, %q), want (\"\", \"\")", gotP, gotB)
	}
}

// --- integration: the full request/response/session-persistence path ---

// TestChatPlanningContextSetsAndClearsExplicitly is Round 2's required
// round-trip: a turn inside a Planning Board sets persisted session
// context and reaches the runner; a later turn with an explicit ("", "")
// pair clears both, and the runner receives empty context on that turn.
func TestChatPlanningContextSetsAndClearsExplicitly(t *testing.T) {
	store := newFakeStore()
	chats := newTestChatStore(t)
	ps := newTestPlanningStore(t)
	planning, err := ps.Create("Exo")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	board := planning.AddBoard("Vision")
	if err := ps.Save(planning); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var receivedPlanningIDs, receivedBoardIDs []string
	runner := func(_ context.Context, _ string, _ []api.Message, _ string, planningID string, boardID string) ([]api.Message, *NavigateAction, error) {
		receivedPlanningIDs = append(receivedPlanningIDs, planningID)
		receivedBoardIDs = append(receivedBoardIDs, boardID)
		return nil, nil, nil
	}
	_, server, httpServer := newTestServer(t, store, WithAgentRunner(runner), WithChatStore(chats), WithPlanningStore(ps))
	client, csrf := bootstrapClient(t, httpServer, server)

	session := postChatSession(t, client, httpServer, server, csrf)

	resp1 := postChatMessageRaw(t, client, httpServer, server, csrf, map[string]any{
		"message": "one", "session_id": session.ID,
		"planning_id": planning.ID, "board_id": board.ID,
	})
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusAccepted {
		t.Fatalf("first POST status = %d, want %d", resp1.StatusCode, http.StatusAccepted)
	}
	waitUntilIdle(t, server)

	loaded1, err := chats.Load(session.ID)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if loaded1.PlanningID != planning.ID || loaded1.BoardID != board.ID {
		t.Fatalf("session context = (%q, %q), want (%q, %q)", loaded1.PlanningID, loaded1.BoardID, planning.ID, board.ID)
	}

	// Explicit clear.
	resp2 := postChatMessageRaw(t, client, httpServer, server, csrf, map[string]any{
		"message": "two", "session_id": session.ID,
		"planning_id": "", "board_id": "",
	})
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusAccepted {
		t.Fatalf("second POST status = %d, want %d", resp2.StatusCode, http.StatusAccepted)
	}
	waitUntilIdle(t, server)

	loaded2, err := chats.Load(session.ID)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if loaded2.PlanningID != "" || loaded2.BoardID != "" {
		t.Fatalf("session context after clear = (%q, %q), want (\"\", \"\")", loaded2.PlanningID, loaded2.BoardID)
	}

	if len(receivedPlanningIDs) != 2 {
		t.Fatalf("runner invocations = %d, want 2", len(receivedPlanningIDs))
	}
	if receivedPlanningIDs[0] != planning.ID || receivedBoardIDs[0] != board.ID {
		t.Fatalf("first turn context = (%q, %q), want (%q, %q)", receivedPlanningIDs[0], receivedBoardIDs[0], planning.ID, board.ID)
	}
	if receivedPlanningIDs[1] != "" || receivedBoardIDs[1] != "" {
		t.Fatalf("second turn context = (%q, %q), want (\"\", \"\")", receivedPlanningIDs[1], receivedBoardIDs[1])
	}
}

// TestChatPlanningContextAllowsPlanningOnly covers the "in a Planning, no
// Board open" state added after Round 2 shipped: a Planning with no Boards
// yet must still let the chat reach the agent with planning_id set and
// board_id empty, so planning_create_board can bootstrap the first Board.
func TestChatPlanningContextAllowsPlanningOnly(t *testing.T) {
	store := newFakeStore()
	chats := newTestChatStore(t)
	ps := newTestPlanningStore(t)
	planning, err := ps.Create("Exo")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var receivedPlanningIDs, receivedBoardIDs []string
	runner := func(_ context.Context, _ string, _ []api.Message, _ string, planningID string, boardID string) ([]api.Message, *NavigateAction, error) {
		receivedPlanningIDs = append(receivedPlanningIDs, planningID)
		receivedBoardIDs = append(receivedBoardIDs, boardID)
		return nil, nil, nil
	}
	_, server, httpServer := newTestServer(t, store, WithAgentRunner(runner), WithChatStore(chats), WithPlanningStore(ps))
	client, csrf := bootstrapClient(t, httpServer, server)

	session := postChatSession(t, client, httpServer, server, csrf)
	resp := postChatMessageRaw(t, client, httpServer, server, csrf, map[string]any{
		"message": "create a board", "session_id": session.ID,
		"planning_id": planning.ID, "board_id": "",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
	waitUntilIdle(t, server)

	loaded, err := chats.Load(session.ID)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if loaded.PlanningID != planning.ID || loaded.BoardID != "" {
		t.Fatalf("session context = (%q, %q), want (%q, \"\")", loaded.PlanningID, loaded.BoardID, planning.ID)
	}
	if len(receivedPlanningIDs) != 1 || receivedPlanningIDs[0] != planning.ID || receivedBoardIDs[0] != "" {
		t.Fatalf("runner context = (%q, %q), want (%q, \"\")", receivedPlanningIDs[0], receivedBoardIDs[0], planning.ID)
	}
}

// TestChatPlanningContextRejectsPartialPairAndLeavesContextUnchanged covers
// the "session context unchanged" half of the spec: a malformed pair must
// 400 without ever touching what the session already had.
func TestChatPlanningContextRejectsPartialPairAndLeavesContextUnchanged(t *testing.T) {
	store := newFakeStore()
	chats := newTestChatStore(t)
	ps := newTestPlanningStore(t)
	planning, err := ps.Create("Exo")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	board := planning.AddBoard("Vision")
	if err := ps.Save(planning); err != nil {
		t.Fatalf("Save: %v", err)
	}

	runner := func(_ context.Context, _ string, _ []api.Message, _ string, _ string, _ string) ([]api.Message, *NavigateAction, error) {
		return nil, nil, nil
	}
	_, server, httpServer := newTestServer(t, store, WithAgentRunner(runner), WithChatStore(chats), WithPlanningStore(ps))
	client, csrf := bootstrapClient(t, httpServer, server)

	session := postChatSession(t, client, httpServer, server, csrf)

	// Establish real context first.
	resp0 := postChatMessageRaw(t, client, httpServer, server, csrf, map[string]any{
		"message": "one", "session_id": session.ID,
		"planning_id": planning.ID, "board_id": board.ID,
	})
	resp0.Body.Close()
	waitUntilIdle(t, server)

	cases := []map[string]any{
		{"message": "bad-1", "session_id": session.ID, "board_id": board.ID},       // planning_id omitted
		{"message": "bad-2", "session_id": session.ID, "planning_id": planning.ID}, // board_id omitted
		// {planning_id: real, board_id: ""} is deliberately NOT here — that's
		// the valid "in a Planning, no Board open" state (see
		// TestChatPlanningContextAllowsPlanningOnly), not an error case.
		{"message": "bad-3", "session_id": session.ID, "planning_id": "", "board_id": board.ID}, // board without planning
	}
	for _, payload := range cases {
		resp := postChatMessageRaw(t, client, httpServer, server, csrf, payload)
		body, _ := decodeErrorBody(resp)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("payload %+v: status = %d (%s), want %d", payload, resp.StatusCode, body, http.StatusBadRequest)
		}

		loaded, err := chats.Load(session.ID)
		if err != nil {
			t.Fatalf("load session: %v", err)
		}
		if loaded.PlanningID != planning.ID || loaded.BoardID != board.ID {
			t.Fatalf("payload %+v: session context = (%q, %q), want unchanged (%q, %q)",
				payload, loaded.PlanningID, loaded.BoardID, planning.ID, board.ID)
		}
	}
}

// TestChatPlanningContextRejectsBoardFromAnotherPlanning covers the
// cross-planning mismatch case: a real planning_id paired with a board_id
// that belongs to a different Planning must 400, not silently write into
// the wrong Planning's board.
func TestChatPlanningContextRejectsBoardFromAnotherPlanning(t *testing.T) {
	store := newFakeStore()
	chats := newTestChatStore(t)
	ps := newTestPlanningStore(t)
	planningA, _ := ps.Create("A")
	boardA := planningA.AddBoard("Board A")
	if err := ps.Save(planningA); err != nil {
		t.Fatalf("Save A: %v", err)
	}
	planningB, _ := ps.Create("B")
	boardB := planningB.AddBoard("Board B")
	if err := ps.Save(planningB); err != nil {
		t.Fatalf("Save B: %v", err)
	}
	_ = boardA

	runner := func(_ context.Context, _ string, _ []api.Message, _ string, _ string, _ string) ([]api.Message, *NavigateAction, error) {
		return nil, nil, nil
	}
	_, server, httpServer := newTestServer(t, store, WithAgentRunner(runner), WithChatStore(chats), WithPlanningStore(ps))
	client, csrf := bootstrapClient(t, httpServer, server)

	session := postChatSession(t, client, httpServer, server, csrf)

	resp := postChatMessageRaw(t, client, httpServer, server, csrf, map[string]any{
		"message": "one", "session_id": session.ID,
		"planning_id": planningA.ID, "board_id": boardB.ID,
	})
	body, _ := decodeErrorBody(resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d (%s), want %d", resp.StatusCode, body, http.StatusBadRequest)
	}

	loaded, err := chats.Load(session.ID)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if loaded.PlanningID != "" || loaded.BoardID != "" {
		t.Fatalf("session context = (%q, %q), want untouched (\"\", \"\")", loaded.PlanningID, loaded.BoardID)
	}
}

func postChatMessageRaw(t *testing.T, client *http.Client, httpServer *httptest.Server, server *Server, csrf string, payload map[string]any) *http.Response {
	t.Helper()
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
	return resp
}

func decodeErrorBody(resp *http.Response) (string, error) {
	if resp == nil || resp.Body == nil {
		return "", nil
	}
	var buf [512]byte
	n, _ := resp.Body.Read(buf[:])
	return string(buf[:n]), nil
}

// --- Round 3: navigate delivery ---

// TestChatStreamDeliversNavigateEventOnErrorToo is the committed-vs-
// delivered guarantee at the HTTP layer: a turn whose runner returns a
// NavigateAction *and* a non-nil error must still deliver the navigate SSE
// event — a later, unrelated failure must not swallow an already-committed
// navigation.
func TestChatStreamDeliversNavigateEventOnErrorToo(t *testing.T) {
	store := newFakeStore()
	chats := newTestChatStore(t)
	runner := func(_ context.Context, _ string, _ []api.Message, _ string, _ string, _ string) ([]api.Message, *NavigateAction, error) {
		return nil, &NavigateAction{PlanningID: "planning-x", PlanningName: "Exo", BoardID: "board-y", BoardName: "Auth"}, errFakeTurnFailure
	}
	_, server, httpServer := newTestServer(t, store, WithAgentRunner(runner), WithChatStore(chats))
	client, csrf := bootstrapClient(t, httpServer, server)

	streamResp := openChatStream(t, client, httpServer, server, true)
	defer streamResp.Body.Close()
	reader := bufio.NewReader(streamResp.Body)
	assertSSEEvent(t, readSSEEvent(t, reader), map[string]any{"type": "idle"})

	resp := postChatMessageRaw(t, client, httpServer, server, csrf, map[string]any{
		"message": "abre Exo / Auth", "client_id": "tab-1", "turn_id": "turn-1",
	})
	resp.Body.Close()

	var sawNavigate bool
	for !sawNavigate {
		raw := readSSEEvent(t, reader)
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("unmarshal SSE payload %q failed: %v", string(raw), err)
		}
		if decoded["type"] != "navigate" {
			continue
		}
		sawNavigate = true
		if decoded["client_id"] != "tab-1" || decoded["turn_id"] != "turn-1" {
			t.Fatalf("navigate event = %#v, want client_id=tab-1 turn_id=turn-1", decoded)
		}
		if decoded["planning_id"] != "planning-x" || decoded["board_id"] != "board-y" {
			t.Fatalf("navigate event = %#v, want planning_id=planning-x board_id=board-y", decoded)
		}
	}
}

var errFakeTurnFailure = errors.New("simulated LLM failure after tool committed")

// TestChatNavigateDoesNotMutateSessionPlanningContext is the Round 2
// regression check: a turn that returns a NavigateAction must not, by
// itself, change what the session's own planning_id/board_id are — those
// are still governed entirely by resolvePlanningContext and this turn's own
// request fields, never by where the agent decided to navigate.
func TestChatNavigateDoesNotMutateSessionPlanningContext(t *testing.T) {
	store := newFakeStore()
	chats := newTestChatStore(t)
	runner := func(_ context.Context, _ string, _ []api.Message, _ string, _ string, _ string) ([]api.Message, *NavigateAction, error) {
		// Navigates somewhere else entirely — must not leak into session context.
		return nil, &NavigateAction{PlanningID: "elsewhere", PlanningName: "Elsewhere"}, nil
	}
	_, server, httpServer := newTestServer(t, store, WithAgentRunner(runner), WithChatStore(chats))
	client, csrf := bootstrapClient(t, httpServer, server)

	session := postChatSession(t, client, httpServer, server, csrf)
	resp := postChatMessageRaw(t, client, httpServer, server, csrf, map[string]any{
		"message": "abre Exo", "session_id": session.ID,
		"planning_id": "", "board_id": "", "client_id": "tab-1", "turn_id": "turn-1",
	})
	resp.Body.Close()
	waitUntilIdle(t, server)

	loaded, err := chats.Load(session.ID)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if loaded.PlanningID != "" || loaded.BoardID != "" {
		t.Fatalf("session context = (%q, %q), want unchanged (\"\", \"\") — navigation must not set it", loaded.PlanningID, loaded.BoardID)
	}
}
