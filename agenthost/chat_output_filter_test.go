package agenthost

import (
	"bytes"
	"testing"
)

func TestFinalOnlyChatWriterDropsTraceBeforeFinalMarker(t *testing.T) {
	var dst bytes.Buffer
	w := newFinalOnlyChatWriter(&dst)

	trace := "→ phase: acting\n" +
		"⚙️ canvas_materialize_draft: {\"draft_object_id\": \"object-1\"}\n" +
		"⚙️ canvas_edit_object: {\"object_id\": \"object-1\", \"payload\": {\"nodes\": []}}\n" +
		"=== FINAL ===\n" +
		"El nodo ahora dice \"Desplegar a Producción\".\n"
	if _, err := w.Write([]byte(trace)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := dst.String()
	want := "El nodo ahora dice \"Desplegar a Producción\".\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFinalOnlyChatWriterPassesThroughAfterBlockedMarker(t *testing.T) {
	var dst bytes.Buffer
	w := newFinalOnlyChatWriter(&dst)

	trace := "→ phase: reviewing\n" +
		"=== BLOCKED BY GATE ===\n" +
		"review score: 4/10\n" +
		"needs another pass on error handling\n"
	if _, err := w.Write([]byte(trace)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := dst.String()
	want := "review score: 4/10\nneeds another pass on error handling\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFinalOnlyChatWriterHandlesMarkerSplitAcrossWrites(t *testing.T) {
	var dst bytes.Buffer
	w := newFinalOnlyChatWriter(&dst)

	chunks := []string{
		"⚙️ canvas_edit_object: {\"object_id\"",
		": \"object-1\"}\n=== FIN",
		"AL ===\nhello",
		" world\n",
	}
	for _, c := range chunks {
		if _, err := w.Write([]byte(c)); err != nil {
			t.Fatalf("Write(%q): %v", c, err)
		}
	}

	got := dst.String()
	want := "hello world\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFinalOnlyChatWriterEmitsNothingWhenNoMarkerEverArrives(t *testing.T) {
	var dst bytes.Buffer
	w := newFinalOnlyChatWriter(&dst)

	if _, err := w.Write([]byte("→ phase: acting\n⚙️ bash: {\"command\": \"ls\"}\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got := dst.String(); got != "" {
		t.Fatalf("got %q, want empty (no boundary marker seen)", got)
	}
}
