package termserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DiegoAvila-yeyo/exo/planningstore"
)

func newTestPlanningStore(t *testing.T) *planningstore.Store {
	t.Helper()
	ps, err := planningstore.New(t.TempDir())
	if err != nil {
		t.Fatalf("planningstore.New: %v", err)
	}
	return ps
}

func TestPlanningsListEmptyWithoutStore(t *testing.T) {
	store := newFakeStore()
	_, server, httpServer := newTestServer(t, store)
	client, _ := bootstrapClient(t, httpServer, server)

	req, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/plannings", nil)
	req.Header.Set("Origin", allowedOrigin(server))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /api/plannings failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var list []planningstore.PlanningSummary
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("list = %+v, want empty (no store configured)", list)
	}
}

func TestPlanningsCreateWithoutStoreReturns503(t *testing.T) {
	store := newFakeStore()
	_, server, httpServer := newTestServer(t, store)
	client, csrf := bootstrapClient(t, httpServer, server)

	req := newJSONRequest(t, http.MethodPost, httpServer.URL+"/api/plannings?csrf_token="+url.QueryEscape(csrf), `{"name":"Exo"}`)
	req.Header.Set("Origin", allowedOrigin(server))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /api/plannings failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestPlanningCreateGetListRoundTrip(t *testing.T) {
	store := newFakeStore()
	ps := newTestPlanningStore(t)
	_, server, httpServer := newTestServer(t, store, WithPlanningStore(ps))
	client, csrf := bootstrapClient(t, httpServer, server)

	created := createPlanningViaHTTP(t, client, httpServer, server, csrf, "Exo Workspace")
	if created.ID == "" || created.Name != "Exo Workspace" {
		t.Fatalf("created planning = %+v", created)
	}

	// GET /api/plannings/{id}
	getReq, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/plannings/"+created.ID, nil)
	getReq.Header.Set("Origin", allowedOrigin(server))
	getResp, err := client.Do(getReq)
	if err != nil {
		t.Fatalf("GET planning failed: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET planning status = %d, want %d", getResp.StatusCode, http.StatusOK)
	}
	var loaded planningstore.Planning
	if err := json.NewDecoder(getResp.Body).Decode(&loaded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if loaded.ID != created.ID {
		t.Fatalf("loaded.ID = %q, want %q", loaded.ID, created.ID)
	}

	// GET /api/plannings lists it
	listReq, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/plannings", nil)
	listReq.Header.Set("Origin", allowedOrigin(server))
	listResp, err := client.Do(listReq)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	defer listResp.Body.Close()
	var list []planningstore.PlanningSummary
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("list = %+v, want just %q", list, created.ID)
	}
}

func TestPlanningGetUnknownID404s(t *testing.T) {
	store := newFakeStore()
	ps := newTestPlanningStore(t)
	_, server, httpServer := newTestServer(t, store, WithPlanningStore(ps))
	client, _ := bootstrapClient(t, httpServer, server)

	req, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/plannings/does-not-exist", nil)
	req.Header.Set("Origin", allowedOrigin(server))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestBoardCreateAndListAndKnowledge(t *testing.T) {
	store := newFakeStore()
	ps := newTestPlanningStore(t)
	_, server, httpServer := newTestServer(t, store, WithPlanningStore(ps))
	client, csrf := bootstrapClient(t, httpServer, server)

	planning := createPlanningViaHTTP(t, client, httpServer, server, csrf, "Exo Workspace")

	// Create a board.
	boardReq := newJSONRequest(t, http.MethodPost,
		httpServer.URL+"/api/plannings/"+planning.ID+"/boards?csrf_token="+url.QueryEscape(csrf),
		`{"name":"Vision"}`)
	boardReq.Header.Set("Origin", allowedOrigin(server))
	boardReq.Header.Set("Content-Type", "application/json")
	boardResp, err := client.Do(boardReq)
	if err != nil {
		t.Fatalf("create board failed: %v", err)
	}
	defer boardResp.Body.Close()
	if boardResp.StatusCode != http.StatusCreated {
		t.Fatalf("create board status = %d, want %d", boardResp.StatusCode, http.StatusCreated)
	}
	var board planningstore.Board
	if err := json.NewDecoder(boardResp.Body).Decode(&board); err != nil {
		t.Fatalf("decode board: %v", err)
	}
	if board.Name != "Vision" {
		t.Fatalf("board.Name = %q, want %q", board.Name, "Vision")
	}

	// List boards.
	listReq, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/plannings/"+planning.ID+"/boards", nil)
	listReq.Header.Set("Origin", allowedOrigin(server))
	listResp, err := client.Do(listReq)
	if err != nil {
		t.Fatalf("list boards failed: %v", err)
	}
	defer listResp.Body.Close()
	var boards []planningstore.Board
	if err := json.NewDecoder(listResp.Body).Decode(&boards); err != nil {
		t.Fatalf("decode boards: %v", err)
	}
	if len(boards) != 1 || boards[0].ID != board.ID {
		t.Fatalf("boards = %+v, want just %+v", boards, board)
	}

	// Get one board.
	getBoardReq, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/plannings/"+planning.ID+"/boards/"+board.ID, nil)
	getBoardReq.Header.Set("Origin", allowedOrigin(server))
	getBoardResp, err := client.Do(getBoardReq)
	if err != nil {
		t.Fatalf("get board failed: %v", err)
	}
	defer getBoardResp.Body.Close()
	if getBoardResp.StatusCode != http.StatusOK {
		t.Fatalf("get board status = %d, want %d", getBoardResp.StatusCode, http.StatusOK)
	}

	// Knowledge for the board is empty (no Knowledge API exists yet in
	// Round 1 — this only proves the read side works and returns []).
	knowledgeReq, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/plannings/"+planning.ID+"/boards/"+board.ID+"/knowledge", nil)
	knowledgeReq.Header.Set("Origin", allowedOrigin(server))
	knowledgeResp, err := client.Do(knowledgeReq)
	if err != nil {
		t.Fatalf("knowledge failed: %v", err)
	}
	defer knowledgeResp.Body.Close()
	if knowledgeResp.StatusCode != http.StatusOK {
		t.Fatalf("knowledge status = %d, want %d", knowledgeResp.StatusCode, http.StatusOK)
	}
	var knowledge []planningstore.Knowledge
	if err := json.NewDecoder(knowledgeResp.Body).Decode(&knowledge); err != nil {
		t.Fatalf("decode knowledge: %v", err)
	}
	if len(knowledge) != 0 {
		t.Fatalf("knowledge = %+v, want empty", knowledge)
	}
}

func TestPlanningCreateRejectsEmptyName(t *testing.T) {
	store := newFakeStore()
	ps := newTestPlanningStore(t)
	_, server, httpServer := newTestServer(t, store, WithPlanningStore(ps))
	client, csrf := bootstrapClient(t, httpServer, server)

	req := newJSONRequest(t, http.MethodPost, httpServer.URL+"/api/plannings?csrf_token="+url.QueryEscape(csrf), `{"name":"  "}`)
	req.Header.Set("Origin", allowedOrigin(server))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// TestKnowledgeAcceptTransitionsAISuggestedToAccepted covers Round 2's
// accept endpoint doing exactly the one transition it's meant for.
func TestKnowledgeAcceptTransitionsAISuggestedToAccepted(t *testing.T) {
	store := newFakeStore()
	ps := newTestPlanningStore(t)
	_, server, httpServer := newTestServer(t, store, WithPlanningStore(ps))
	client, csrf := bootstrapClient(t, httpServer, server)

	planning := createPlanningViaHTTP(t, client, httpServer, server, csrf, "Exo")
	planning.AddBoard("Vision")
	k := planning.AddKnowledge(planningstore.Knowledge{
		Type: planningstore.KnowledgeNote, Title: "AI note", Author: planningstore.AuthorAISuggested,
	})
	if err := ps.Save(planning); err != nil {
		t.Fatalf("Save: %v", err)
	}

	resp := postKnowledgeTransition(t, client, httpServer, server, csrf, planning.ID, k.ID, "accept")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var updated planningstore.Knowledge
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.Author != planningstore.AuthorAccepted {
		t.Fatalf("Author = %q, want %q", updated.Author, planningstore.AuthorAccepted)
	}

	reloaded, err := ps.Load(planning.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.Knowledge[0].Author != planningstore.AuthorAccepted {
		t.Fatalf("persisted Author = %q, want %q", reloaded.Knowledge[0].Author, planningstore.AuthorAccepted)
	}
}

// TestKnowledgeRejectTransitionsAISuggestedToRejected mirrors the accept
// test for the reject path.
func TestKnowledgeRejectTransitionsAISuggestedToRejected(t *testing.T) {
	store := newFakeStore()
	ps := newTestPlanningStore(t)
	_, server, httpServer := newTestServer(t, store, WithPlanningStore(ps))
	client, csrf := bootstrapClient(t, httpServer, server)

	planning := createPlanningViaHTTP(t, client, httpServer, server, csrf, "Exo")
	k := planning.AddKnowledge(planningstore.Knowledge{
		Type: planningstore.KnowledgeResearch, Title: "AI research", Author: planningstore.AuthorAISuggested,
	})
	if err := ps.Save(planning); err != nil {
		t.Fatalf("Save: %v", err)
	}

	resp := postKnowledgeTransition(t, client, httpServer, server, csrf, planning.ID, k.ID, "reject")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	reloaded, err := ps.Load(planning.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.Knowledge[0].Author != planningstore.AuthorRejected {
		t.Fatalf("persisted Author = %q, want %q", reloaded.Knowledge[0].Author, planningstore.AuthorRejected)
	}
}

// TestKnowledgeTransitionRefusesNonAISuggestedSource is the state-machine
// guard: accept/reject only ever fire from ai_suggested. Anything else
// (already human, already accepted, already rejected) must 409 and leave
// the entry untouched — these endpoints are not a general "set author" API.
func TestKnowledgeTransitionRefusesNonAISuggestedSource(t *testing.T) {
	store := newFakeStore()
	ps := newTestPlanningStore(t)
	_, server, httpServer := newTestServer(t, store, WithPlanningStore(ps))
	client, csrf := bootstrapClient(t, httpServer, server)

	planning := createPlanningViaHTTP(t, client, httpServer, server, csrf, "Exo")

	sources := []planningstore.AuthorState{
		planningstore.AuthorHuman,
		planningstore.AuthorAccepted,
		planningstore.AuthorRejected,
		planningstore.AuthorArchived,
	}
	for _, author := range sources {
		k := planning.AddKnowledge(planningstore.Knowledge{Type: planningstore.KnowledgeNote, Title: string(author), Author: author})
		if err := ps.Save(planning); err != nil {
			t.Fatalf("Save: %v", err)
		}

		for _, action := range []string{"accept", "reject"} {
			resp := postKnowledgeTransition(t, client, httpServer, server, csrf, planning.ID, k.ID, action)
			resp.Body.Close()
			if resp.StatusCode != http.StatusConflict {
				t.Fatalf("author=%q action=%q: status = %d, want %d", author, action, resp.StatusCode, http.StatusConflict)
			}
		}

		reloaded, err := ps.Load(planning.ID)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		var got planningstore.AuthorState
		for _, entry := range reloaded.Knowledge {
			if entry.ID == k.ID {
				got = entry.Author
			}
		}
		if got != author {
			t.Fatalf("author=%q: entry mutated to %q despite rejected transition", author, got)
		}
	}
}

func postKnowledgeTransition(t *testing.T, client *http.Client, httpServer *httptest.Server, server *Server, csrf, planningID, knowledgeID, action string) *http.Response {
	t.Helper()
	target := httpServer.URL + "/api/plannings/" + planningID + "/knowledge/" + knowledgeID + "/" + action + "?csrf_token=" + url.QueryEscape(csrf)
	req := newJSONRequest(t, http.MethodPost, target, `{}`)
	req.Header.Set("Origin", allowedOrigin(server))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s failed: %v", target, err)
	}
	return resp
}

func createPlanningViaHTTP(t *testing.T, client *http.Client, httpServer *httptest.Server, server *Server, csrf, name string) planningstore.Planning {
	t.Helper()
	req := newJSONRequest(t, http.MethodPost, httpServer.URL+"/api/plannings?csrf_token="+url.QueryEscape(csrf), `{"name":"`+strings.ReplaceAll(name, `"`, `\"`)+`"}`)
	req.Header.Set("Origin", allowedOrigin(server))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("create planning failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create planning status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	var p planningstore.Planning
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("decode created planning: %v", err)
	}
	return p
}
