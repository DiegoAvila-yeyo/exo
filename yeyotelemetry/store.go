// Package yeyotelemetry persists real-usage telemetry for the yeyo
// atoms_decision gate (agenthost/host.go, behind EXO_YEYO_GATE) — the
// consumer of the onDecision/onGet measurement hooks the gate migration
// left in place. See ~/yeyo/docs/codex_consult_instrumentacion.md and
// ~/yeyo/docs/experiments-roadmap.md ("Fase operacional") for the design
// this implements: append-only events, not mutable rows, because
// reconstructing catalog drift after the fact requires the full history of
// what was seen, not just the latest state.
//
// Storage: its own SQLite file (appconfig.YeyoTelemetryDBPath), sibling to
// memory.db, opened the same way localstore.OpenSQLite does
// (nucleo-base/layer4-knowledge-memory/localstore) — modernc.org/sqlite,
// WAL mode, one *sql.DB. NOT a table added to chatstore or sessionstore:
// both of those are one-file-per-record JSON stores (write-tmp-then-rename)
// with no table/SQL concept at all, so "add a table" doesn't apply to them
// literally. See the build report for the full reasoning — this was a
// build-time finding, not an assumption to silently work around.
package yeyotelemetry

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS yeyo_gate_events (
	id                    INTEGER PRIMARY KEY AUTOINCREMENT,
	ts                    TEXT NOT NULL,
	session_id            TEXT NOT NULL,
	turn_id               TEXT NOT NULL,
	event_type            TEXT NOT NULL CHECK (event_type IN ('decision','atom_get','turn_result')),
	decision              TEXT,
	index_atom_count      INTEGER,
	index_text_bytes      INTEGER,
	catalog_hash          TEXT,
	catalog_snapshot_json TEXT,
	atom_name             TEXT,
	atom_seq              INTEGER,
	content_hash          TEXT,
	turn_result           TEXT,
	message_index_before  INTEGER
);
CREATE INDEX IF NOT EXISTS idx_yeyo_gate_events_session ON yeyo_gate_events(session_id);
CREATE INDEX IF NOT EXISTS idx_yeyo_gate_events_turn    ON yeyo_gate_events(turn_id);
CREATE INDEX IF NOT EXISTS idx_yeyo_gate_events_type     ON yeyo_gate_events(event_type);
`

// Store is append-only: every exported method here does an INSERT, never
// an UPDATE. Reopening/migrating is idempotent (CREATE TABLE IF NOT
// EXISTS), same as localstore.SQLiteStore.
type Store struct {
	db *sql.DB
	mu sync.Mutex
}

// Open creates the DB file and its directory if needed, and applies the
// schema. Mirrors localstore.OpenSQLite's shape (same driver, same WAL/
// busy_timeout pragmas) — deliberately, so this behaves the same way under
// concurrent access as the memory store already does in production.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("yeyotelemetry: path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("yeyotelemetry: mkdir: %w", err)
	}

	dsn := path + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("yeyotelemetry: open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("yeyotelemetry: ping sqlite: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("yeyotelemetry: migrate schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// AtomSnapshot is one periferia atom as it existed at the moment a
// "decision" event was recorded: name plus a content hash of its body, so
// catalog drift (an atom's body changing between two sessions, or an atom
// being added/removed) is reconstructable later purely from stored events
// — without this, "the index available in that moment" would be
// unrecoverable the instant the live catalog changes underneath it.
type AtomSnapshot struct {
	Name        string `json:"name"`
	ContentHash string `json:"content_hash"`
}

// ContentHash hashes one atom's body. Design decision made during this
// build, not specified in the Codex consult: sha256, full 64-char hex, of
// the exact body string the model would see via "atom get" — no
// normalization (whitespace, case, etc.) applied. Flagged for review: a
// looser hash (e.g. trimmed/normalized) might be more useful for detecting
// only meaningful drift, but that's a judgment call better made with real
// drift examples in hand, not guessed now.
func ContentHash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// CatalogHash hashes an entire periferia snapshot as one unit: it changes
// if any atom is added, removed, renamed, or edited, but not if the same
// set of (name, content_hash) pairs is presented in a different order.
// Same design-decision caveat as ContentHash — exact hash shape wasn't
// specified upstream, this is this build's choice, flagged for review.
func CatalogHash(snapshot []AtomSnapshot) string {
	sorted := append([]AtomSnapshot(nil), snapshot...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	var b strings.Builder
	for _, a := range sorted {
		b.WriteString(a.Name)
		b.WriteByte(0)
		b.WriteString(a.ContentHash)
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// RecordDecision logs one atoms_decision(inspect|skip) call. snapshot and
// indexTextBytes are meaningless for "skip" (the model never saw the
// index) but are still passed in and stored for "skip" too, so a later
// query doesn't need to special-case which decision it's looking at to
// know what catalog *would* have been shown.
func (s *Store) RecordDecision(sessionID, turnID, decision string, snapshot []AtomSnapshot, indexTextBytes int) error {
	if s == nil {
		return nil
	}
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("yeyotelemetry: marshal snapshot: %w", err)
	}
	return s.insert(map[string]any{
		"ts":                    nowRFC3339(),
		"session_id":            sessionID,
		"turn_id":               turnID,
		"event_type":            "decision",
		"decision":              decision,
		"index_atom_count":      len(snapshot),
		"index_text_bytes":      indexTextBytes,
		"catalog_hash":          CatalogHash(snapshot),
		"catalog_snapshot_json": string(snapshotJSON),
	})
}

// RecordAtomGet logs one successful "atom get <name>" call. seq is 1-based,
// ordered within the turn — the caller (agenthost) is responsible for
// numbering these in call order, this method just persists whatever it's
// given.
func (s *Store) RecordAtomGet(sessionID, turnID, atomName, contentHash string, seq int) error {
	if s == nil {
		return nil
	}
	return s.insert(map[string]any{
		"ts":           nowRFC3339(),
		"session_id":   sessionID,
		"turn_id":      turnID,
		"event_type":   "atom_get",
		"atom_name":    atomName,
		"atom_seq":     seq,
		"content_hash": contentHash,
	})
}

// RecordTurnResult logs the technical outcome of one turn: "completed",
// "error", or "cancelled" (context cancellation) — never a judgment about
// whether the gate's decision was *correct*, that's not knowable from
// inside the turn. messageIndexBefore is the length of the session's
// message history immediately before this turn ran (chatstore.
// ChatSession.Messages) — enough for offline analysis to locate "the
// message right after this turn" in chatstore without this package needing
// to know anything about chatstore itself (deliberately not classifying
// "was this a correction" here — see the package doc comment and the
// build report).
func (s *Store) RecordTurnResult(sessionID, turnID, result string, messageIndexBefore int) error {
	if s == nil {
		return nil
	}
	return s.insert(map[string]any{
		"ts":                   nowRFC3339(),
		"session_id":           sessionID,
		"turn_id":              turnID,
		"event_type":           "turn_result",
		"turn_result":          result,
		"message_index_before": messageIndexBefore,
	})
}

func (s *Store) insert(cols map[string]any) error {
	names := make([]string, 0, len(cols))
	placeholders := make([]string, 0, len(cols))
	args := make([]any, 0, len(cols))
	for k, v := range cols {
		names = append(names, k)
		placeholders = append(placeholders, "?")
		args = append(args, v)
	}
	stmt := fmt.Sprintf("INSERT INTO yeyo_gate_events (%s) VALUES (%s)",
		strings.Join(names, ", "), strings.Join(placeholders, ", "))

	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(stmt, args...)
	if err != nil {
		return fmt.Errorf("yeyotelemetry: insert %s event: %w", cols["event_type"], err)
	}
	return nil
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
