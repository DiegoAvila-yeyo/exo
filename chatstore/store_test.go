package chatstore

import (
	"path/filepath"
	"testing"

	"github.com/yeyoos/nucleo-base/shared/api"
)

func TestCreateWritesDefaultTitleAndEmptyMessages(t *testing.T) {
	store := newTestStore(t)

	session, err := store.Create()
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if session.Title != DefaultTitle {
		t.Fatalf("title = %q, want %q", session.Title, DefaultTitle)
	}
	if len(session.Messages) != 0 {
		t.Fatalf("messages = %+v, want empty", session.Messages)
	}
	if len(session.Entries) != 0 {
		t.Fatalf("entries = %+v, want empty", session.Entries)
	}

	loaded, err := store.Load(session.ID)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.ID != session.ID {
		t.Fatalf("loaded.ID = %q, want %q", loaded.ID, session.ID)
	}
}

func TestSaveRoundTripsMessagesAndBumpsUpdatedAt(t *testing.T) {
	store := newTestStore(t)

	session, err := store.Create()
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	originalUpdatedAt := session.UpdatedAt

	session.Title = "Deploy the staging env"
	session.Messages = []api.Message{
		{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: "hola"}}},
		{Role: api.RoleAssistant, Content: []api.Block{{Type: api.BlockText, Text: "hola! como puedo ayudarte?"}}},
	}
	session.Entries = []ChatEntry{
		{Text: "You: hola", Kind: "system"},
		{Text: "hola! como puedo ayudarte?"},
	}
	session.ProjectPath = "/Users/eltitoyeyo/exo"
	if err := store.Save(session); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := store.Load(session.ID)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.Title != "Deploy the staging env" {
		t.Fatalf("title = %q, want %q", loaded.Title, "Deploy the staging env")
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("messages = %+v, want 2 entries", loaded.Messages)
	}
	if loaded.Messages[1].Content[0].Text != "hola! como puedo ayudarte?" {
		t.Fatalf("messages[1] text = %q, want assistant reply", loaded.Messages[1].Content[0].Text)
	}
	if len(loaded.Entries) != 2 || loaded.Entries[0].Kind != "system" {
		t.Fatalf("entries = %+v, want 2 entries with first kind=system", loaded.Entries)
	}
	if loaded.ProjectPath != "/Users/eltitoyeyo/exo" {
		t.Fatalf("ProjectPath = %q, want %q", loaded.ProjectPath, "/Users/eltitoyeyo/exo")
	}
	if !loaded.UpdatedAt.After(originalUpdatedAt) {
		t.Fatalf("UpdatedAt = %v, want after %v", loaded.UpdatedAt, originalUpdatedAt)
	}
}

func TestLoadMissingSessionReturnsErrNotFound(t *testing.T) {
	store := newTestStore(t)

	_, err := store.Load("chat-does-not-exist")
	if err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestListOrdersByMostRecentlyUpdatedFirst(t *testing.T) {
	store := newTestStore(t)

	older, err := store.Create()
	if err != nil {
		t.Fatalf("create older failed: %v", err)
	}
	newer, err := store.Create()
	if err != nil {
		t.Fatalf("create newer failed: %v", err)
	}

	// Touch "older" again so its UpdatedAt moves forward, but it should still
	// sort after "newer" once "newer" is saved last.
	older.Title = "First task"
	if err := store.Save(older); err != nil {
		t.Fatalf("save older failed: %v", err)
	}
	newer.Title = "Second task"
	newer.ProjectPath = "/Users/eltitoyeyo/exo"
	if err := store.Save(newer); err != nil {
		t.Fatalf("save newer failed: %v", err)
	}

	summaries, err := store.List()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("summaries = %+v, want 2 entries", summaries)
	}
	if summaries[0].ID != newer.ID {
		t.Fatalf("summaries[0].ID = %q, want most recently updated %q", summaries[0].ID, newer.ID)
	}
	if summaries[0].Title != "Second task" {
		t.Fatalf("summaries[0].Title = %q, want %q", summaries[0].Title, "Second task")
	}
	if summaries[0].ProjectPath != "/Users/eltitoyeyo/exo" {
		t.Fatalf("summaries[0].ProjectPath = %q, want %q", summaries[0].ProjectPath, "/Users/eltitoyeyo/exo")
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := New(filepath.Join(t.TempDir(), "chats"))
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	return store
}
