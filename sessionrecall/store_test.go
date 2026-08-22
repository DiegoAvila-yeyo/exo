package sessionrecall

import (
	"testing"
)

func TestSaveAndLoadRoundTrip(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pr, err := store.Load("/proj/a")
	if err != nil {
		t.Fatalf("Load (auto-create): %v", err)
	}
	if pr.Version != 0 || len(pr.Entries) != 0 {
		t.Fatalf("auto-created ProjectRecall = %+v, want empty Version 0", pr)
	}

	pr.Entries = append(pr.Entries, SessionSummary{
		SessionID:   "chat-1",
		Title:       "Fixed the widget bug",
		Description: "Debugged and fixed a rendering issue",
		SummaryBody: "Long summary...",
		Status:      StatusClosed,
	})
	saved, err := store.Save(pr)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saved.Version != 1 {
		t.Fatalf("saved.Version = %d, want 1", saved.Version)
	}

	reloaded, err := store.Load("/proj/a")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.Version != 1 || len(reloaded.Entries) != 1 {
		t.Fatalf("reloaded = %+v, want 1 entry at version 1", reloaded)
	}
	if reloaded.Entries[0].SessionID != "chat-1" {
		t.Fatalf("reloaded.Entries[0].SessionID = %q, want chat-1", reloaded.Entries[0].SessionID)
	}
}

func TestSaveRejectsStaleVersion(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pr, _ := store.Load("/proj/a")
	pr.Entries = append(pr.Entries, SessionSummary{SessionID: "chat-1", Status: StatusClosed})
	if _, err := store.Save(pr); err != nil {
		t.Fatalf("first Save: %v", err)
	}

	// pr.Version is still 0 (the caller's stale copy) — Save must reject it
	// now that the on-disk version has moved to 1.
	if _, err := store.Save(pr); err != ErrStaleVersion {
		t.Fatalf("second Save with stale Version = %v, want ErrStaleVersion", err)
	}
}

func TestSaveUpsertsBySessionIDIdempotently(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Simulate closing the same session twice: Load fresh, replace/insert
	// the entry for SessionID, Save — exactly what termserver/chat.go's
	// close handler does on retry.
	closeOnce := func() {
		pr, err := store.Load("/proj/a")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		entry := SessionSummary{SessionID: "chat-1", Title: "t", Status: StatusClosed}
		replaced := false
		for i, e := range pr.Entries {
			if e.SessionID == entry.SessionID {
				pr.Entries[i] = entry
				replaced = true
				break
			}
		}
		if !replaced {
			pr.Entries = append(pr.Entries, entry)
		}
		if _, err := store.Save(pr); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	closeOnce()
	closeOnce()

	final, err := store.Load("/proj/a")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(final.Entries) != 1 {
		t.Fatalf("len(final.Entries) = %d, want 1 (closing twice must not duplicate)", len(final.Entries))
	}
	if final.Version != 2 {
		t.Fatalf("final.Version = %d, want 2 (two successful saves)", final.Version)
	}
}
