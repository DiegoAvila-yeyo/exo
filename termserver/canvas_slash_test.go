package termserver

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/DiegoAvila-yeyo/exo/canvasstore"
	"github.com/yeyoos/nucleo-base/shared/api"
)

func TestParseMaterializeSlashCommand(t *testing.T) {
	cases := []struct {
		message  string
		wantName string
		wantOK   bool
	}{
		{"/materialize Auth Flow", "Auth Flow", true},
		{"  /materialize   Auth Flow  ", "Auth Flow", true},
		{"/materialize", "", false},
		{"/materialize   ", "", false},
		{"materialize Auth Flow", "", false},
		{"hey /materialize this please", "", false},
	}
	for _, c := range cases {
		name, ok := parseMaterializeSlashCommand(c.message)
		if ok != c.wantOK || name != c.wantName {
			t.Errorf("parseMaterializeSlashCommand(%q) = (%q, %v), want (%q, %v)", c.message, name, ok, c.wantName, c.wantOK)
		}
	}
}

// TestMaterializeSlashCommandMaterializesUnambiguousDraft is an end-to-end
// check of the fallback channel: POST /api/chat with "/materialize <name>"
// bypasses the agent loop entirely and materializes the named draft
// directly against canvasstore.
func TestMaterializeSlashCommandMaterializesUnambiguousDraft(t *testing.T) {
	store := newFakeStore()
	cs := newTestCanvasStore(t)
	runner := func(_ context.Context, _ string, _ []api.Message, _ string, _ string, _ string) ([]api.Message, *NavigateAction, *CanvasSuggestion, error) {
		t.Fatalf("runner should never be called for a /materialize slash command")
		return nil, nil, nil, nil
	}
	_, server, httpServer := newTestServer(t, store, WithAgentRunner(runner), WithCanvasStore(cs))
	client, csrf := bootstrapClient(t, httpServer, server)

	projectPath := "/proj"
	pc, err := cs.Load(projectPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	obj, err := pc.AddDraft("diagram", "Auth Flow", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("AddDraft: %v", err)
	}
	if _, err := cs.Save(pc); err != nil {
		t.Fatalf("Save: %v", err)
	}

	resp := postChatMessageRaw(t, client, httpServer, server, csrf, map[string]any{
		"message": "/materialize Auth Flow", "project_path": projectPath,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/chat (/materialize) status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	reloaded, err := cs.Load(projectPath)
	if err != nil {
		t.Fatalf("Load (after materialize): %v", err)
	}
	var found *canvasstore.CanvasObject
	for i := range reloaded.Objects {
		if reloaded.Objects[i].ObjectID == obj.ObjectID {
			found = &reloaded.Objects[i]
		}
	}
	if found == nil || found.Phase != canvasstore.PhaseMaterialized {
		t.Fatalf("object after /materialize = %+v, want Phase materialized", found)
	}
}
