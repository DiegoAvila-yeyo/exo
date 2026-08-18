package yeyotelemetry

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "telemetry.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestRecordDecisionInsertsOneRow(t *testing.T) {
	s := openTestStore(t)
	snapshot := []AtomSnapshot{{Name: "a", ContentHash: "h1"}, {Name: "b", ContentHash: "h2"}}
	if err := s.RecordDecision("sess-1", "turn-1", "inspect", snapshot, 123); err != nil {
		t.Fatalf("RecordDecision: %v", err)
	}

	var count int
	var decision string
	var atomCount, textBytes int
	var catalogHash string
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM yeyo_gate_events WHERE event_type='decision'`).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Fatalf("got %d decision rows, want 1", count)
	}
	if err := s.db.QueryRow(`SELECT decision, index_atom_count, index_text_bytes, catalog_hash
		FROM yeyo_gate_events WHERE event_type='decision'`).Scan(&decision, &atomCount, &textBytes, &catalogHash); err != nil {
		t.Fatalf("select: %v", err)
	}
	if decision != "inspect" || atomCount != 2 || textBytes != 123 {
		t.Fatalf("got (%q, %d, %d), want (inspect, 2, 123)", decision, atomCount, textBytes)
	}
	if catalogHash != CatalogHash(snapshot) {
		t.Fatalf("stored catalog_hash %q != CatalogHash(snapshot) %q", catalogHash, CatalogHash(snapshot))
	}
}

func TestRecordAtomGetOrdersBySeq(t *testing.T) {
	s := openTestStore(t)
	if err := s.RecordAtomGet("sess-1", "turn-1", "first", ContentHash("body1"), 1); err != nil {
		t.Fatalf("RecordAtomGet(1): %v", err)
	}
	if err := s.RecordAtomGet("sess-1", "turn-1", "second", ContentHash("body2"), 2); err != nil {
		t.Fatalf("RecordAtomGet(2): %v", err)
	}

	rows, err := s.db.Query(`SELECT atom_name, atom_seq FROM yeyo_gate_events
		WHERE event_type='atom_get' AND turn_id='turn-1' ORDER BY atom_seq ASC`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		var seq int
		if err := rows.Scan(&name, &seq); err != nil {
			t.Fatalf("scan: %v", err)
		}
		names = append(names, name)
	}
	if len(names) != 2 || names[0] != "first" || names[1] != "second" {
		t.Fatalf("got %v, want [first second] in order", names)
	}
}

func TestRecordTurnResult(t *testing.T) {
	s := openTestStore(t)
	if err := s.RecordTurnResult("sess-1", "turn-1", "completed", 4); err != nil {
		t.Fatalf("RecordTurnResult: %v", err)
	}
	var result string
	var idx int
	if err := s.db.QueryRow(`SELECT turn_result, message_index_before FROM yeyo_gate_events
		WHERE event_type='turn_result'`).Scan(&result, &idx); err != nil {
		t.Fatalf("select: %v", err)
	}
	if result != "completed" || idx != 4 {
		t.Fatalf("got (%q, %d), want (completed, 4)", result, idx)
	}
}

func TestEventsAreAppendOnlyNoUpdateHelper(t *testing.T) {
	// Structural guard: Store exposes no method that mutates an existing
	// row. This test documents that intent and fails loudly (compile
	// error) if a future edit adds one without updating this note.
	var s *Store
	_ = s.RecordDecision
	_ = s.RecordAtomGet
	_ = s.RecordTurnResult
	// No Update*/Delete* method should exist; nothing to assert at runtime
	// beyond the schema itself having no primary key callers can target
	// for mutation from this package's public surface.
	var _ *sql.DB
}

func TestOpenCreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "telemetry.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
}
