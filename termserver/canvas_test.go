package termserver

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/DiegoAvila-yeyo/exo/canvasstore"
)

func newTestCanvasStore(t *testing.T) *canvasstore.Store {
	t.Helper()
	cs, err := canvasstore.New(t.TempDir())
	if err != nil {
		t.Fatalf("canvasstore.New: %v", err)
	}
	return cs
}

func TestCanvasGetEmptyWithoutStore(t *testing.T) {
	store := newFakeStore()
	_, server, httpServer := newTestServer(t, store)
	client, _ := bootstrapClient(t, httpServer, server)

	req, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/canvases?project_path="+url.QueryEscape("/proj"), nil)
	req.Header.Set("Origin", allowedOrigin(server))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /api/canvases failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var pc canvasstore.ProjectCanvas
	if err := json.NewDecoder(resp.Body).Decode(&pc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(pc.Objects) != 0 {
		t.Fatalf("Objects = %+v, want empty (no store configured)", pc.Objects)
	}
}

func TestCanvasPatchWithoutStoreReturns503(t *testing.T) {
	store := newFakeStore()
	_, server, httpServer := newTestServer(t, store)
	client, csrf := bootstrapClient(t, httpServer, server)

	req := newJSONRequest(t, http.MethodPatch,
		httpServer.URL+"/api/canvases/objects/object-x?project_path="+url.QueryEscape("/proj")+"&csrf_token="+url.QueryEscape(csrf),
		`{"payload":{},"expected_version":0}`)
	req.Header.Set("Origin", allowedOrigin(server))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PATCH failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

// TestCanvasManualEditGoesThroughSupersedesNotMutation is the direct
// acceptance-criteria check for the manual editing panel's write path:
// edits land via PATCH, versioned through AppendAtom's supersedes chain,
// never overwriting the prior atom in place.
func TestCanvasManualEditGoesThroughSupersedesNotMutation(t *testing.T) {
	store := newFakeStore()
	cs := newTestCanvasStore(t)
	_, server, httpServer := newTestServer(t, store, WithCanvasStore(cs))
	client, csrf := bootstrapClient(t, httpServer, server)

	projectPath := "/proj"
	pc, err := cs.Load(projectPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	obj, err := pc.AddDraft("diagram", "My Diagram", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("AddDraft: %v", err)
	}
	if err := pc.Materialize(obj.ObjectID); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	saved, err := cs.Save(pc)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	patchURL := httpServer.URL + "/api/canvases/objects/" + obj.ObjectID +
		"?project_path=" + url.QueryEscape(projectPath) + "&csrf_token=" + url.QueryEscape(csrf)

	firstBody := `{"payload":{"nodes":["first edit"]},"expected_version":` + itoa(saved.Version) + `}`
	req := newJSONRequest(t, http.MethodPatch, patchURL, firstBody)
	req.Header.Set("Origin", allowedOrigin(server))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PATCH (first edit) failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH (first edit) status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var afterFirst canvasstore.ProjectCanvas
	if err := json.NewDecoder(resp.Body).Decode(&afterFirst); err != nil {
		t.Fatalf("decode (first edit): %v", err)
	}

	// A stale PATCH (expected_version behind current) must bounce with 409.
	staleBody := `{"payload":{"nodes":["stale edit"]},"expected_version":` + itoa(saved.Version) + `}`
	staleReq := newJSONRequest(t, http.MethodPatch, patchURL, staleBody)
	staleReq.Header.Set("Origin", allowedOrigin(server))
	staleReq.Header.Set("Content-Type", "application/json")
	staleResp, err := client.Do(staleReq)
	if err != nil {
		t.Fatalf("PATCH (stale edit) failed: %v", err)
	}
	defer staleResp.Body.Close()
	if staleResp.StatusCode != http.StatusConflict {
		t.Fatalf("PATCH (stale edit) status = %d, want %d", staleResp.StatusCode, http.StatusConflict)
	}

	// Retry with the fresh version succeeds and both atoms are retained.
	secondBody := `{"payload":{"nodes":["second edit"]},"expected_version":` + itoa(afterFirst.Version) + `}`
	secondReq := newJSONRequest(t, http.MethodPatch, patchURL, secondBody)
	secondReq.Header.Set("Origin", allowedOrigin(server))
	secondReq.Header.Set("Content-Type", "application/json")
	secondResp, err := client.Do(secondReq)
	if err != nil {
		t.Fatalf("PATCH (retry) failed: %v", err)
	}
	defer secondResp.Body.Close()
	if secondResp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH (retry) status = %d, want %d", secondResp.StatusCode, http.StatusOK)
	}
	var afterSecond canvasstore.ProjectCanvas
	if err := json.NewDecoder(secondResp.Body).Decode(&afterSecond); err != nil {
		t.Fatalf("decode (retry): %v", err)
	}

	if len(afterSecond.Atoms) != 2 {
		t.Fatalf("Atoms = %d, want 2 (first atom retained, not mutated)", len(afterSecond.Atoms))
	}
	current, ok := afterSecond.CurrentAtom(obj.ObjectID)
	if !ok {
		t.Fatalf("CurrentAtom not found after retry")
	}
	var head, prior canvasstore.CanvasAtom
	for _, a := range afterSecond.Atoms {
		if a.AtomID == current.AtomID {
			head = a
		} else {
			prior = a
		}
	}
	if head.Supersedes != prior.AtomID {
		t.Fatalf("current atom Supersedes = %q, want %q (the first edit's atom)", head.Supersedes, prior.AtomID)
	}
}

func itoa(n int) string {
	data, _ := json.Marshal(n)
	return string(data)
}

// TestCanvasManualEditRejectsDanglingDiagramEdge is the direct regression
// check for QA finding #5: a manual JSON edit with an edge referencing a
// node id that doesn't exist in the same payload must be rejected before
// it's persisted, not silently saved and dropped at render time.
func TestCanvasManualEditRejectsDanglingDiagramEdge(t *testing.T) {
	store := newFakeStore()
	cs := newTestCanvasStore(t)
	_, server, httpServer := newTestServer(t, store, WithCanvasStore(cs))
	client, csrf := bootstrapClient(t, httpServer, server)

	projectPath := "/proj"
	pc, err := cs.Load(projectPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	obj, err := pc.AddDraft("diagram", "My Diagram", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("AddDraft: %v", err)
	}
	if err := pc.Materialize(obj.ObjectID); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	saved, err := cs.Save(pc)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	patchURL := httpServer.URL + "/api/canvases/objects/" + obj.ObjectID +
		"?project_path=" + url.QueryEscape(projectPath) + "&csrf_token=" + url.QueryEscape(csrf)
	body := `{"payload":{"nodes":[{"id":"a"}],"edges":[{"id":"e1","from":"a","to":"missing"}]},"expected_version":` + itoa(saved.Version) + `}`
	req := newJSONRequest(t, http.MethodPatch, patchURL, body)
	req.Header.Set("Origin", allowedOrigin(server))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PATCH failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PATCH (dangling edge) status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	reloaded, err := cs.Load(projectPath)
	if err != nil {
		t.Fatalf("Load (after rejected edit): %v", err)
	}
	if len(reloaded.Atoms) != 0 {
		t.Fatalf("Atoms after rejected edit = %d, want 0 (nothing should have been persisted)", len(reloaded.Atoms))
	}
}
