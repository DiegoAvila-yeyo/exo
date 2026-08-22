package agenthost

import "bytes"

// finalOnlyChatWriter fixes CANVAS_STATUS.md bugs #3/#4: the vendored
// agent.Agent (nucleo-base) prints its full internal trace to stdout during
// a turn — phase markers, tool-call lines (friendly for tools nucleo-base
// recognizes, but raw JSON for any tool it doesn't, which today means every
// exo-specific tool: canvas_*, atom_*, planning_*, scale_*), intermediate
// assistant text — and Host.Run's redirectStdout (stdout.go) forwards all of
// it, byte for byte, straight to the browser-visible chat stream. None of
// that trace was ever meant for the human: nucleo-base's own coordinator
// marks exactly where the human-facing answer starts by printing one of two
// literal marker lines (coordinator_render.go's publishFinal/publishBlocked
// in ~/nucleo-base) once the turn reaches a terminal state.
//
// This writer buffers everything until it sees one of those marker lines,
// drops the marker itself, and only then starts forwarding — so the browser
// chat shows just the real answer (or, for a gated turn, the block
// explanation), never the reasoning trace or raw tool-call JSON in front of
// it. Deliberately fixed here in agenthost, not in nucleo-base itself:
// nucleo-base is shared with other consumers in the ecosystem (avengers,
// etc.), and this filtering is specific to what exo's browser chat wants to
// show — adding a case to nucleo-base's friendlyToolName per exo tool would
// also work for the JSON-leak half of the bug, but wouldn't touch the
// marker-line leak, and would need a new case every time exo grows another
// tool.
//
// One instance per turn: redirectStdout opens a fresh pipe on every
// Host.Run call, so "found the boundary yet" naturally resets each turn
// with no extra bookkeeping required here.
type finalOnlyChatWriter struct {
	dst   writer
	buf   bytes.Buffer
	found bool
}

// writer is the minimal io.Writer contract — named locally so this file
// doesn't need to import "io" just for the one method.
type writer interface {
	Write(p []byte) (int, error)
}

var chatBoundaryMarkers = [][]byte{
	[]byte("=== FINAL ==="),
	[]byte("=== BLOCKED BY GATE ==="),
}

func newFinalOnlyChatWriter(dst writer) *finalOnlyChatWriter {
	return &finalOnlyChatWriter{dst: dst}
}

func (w *finalOnlyChatWriter) Write(p []byte) (int, error) {
	total := len(p)
	if w.found {
		if _, err := w.dst.Write(p); err != nil {
			return 0, err
		}
		return total, nil
	}

	w.buf.Write(p)
	for {
		idx := bytes.IndexByte(w.buf.Bytes(), '\n')
		if idx == -1 {
			break // no complete line buffered yet — wait for more data
		}
		line := w.buf.Next(idx + 1) // consumes through the '\n'
		if !isChatBoundaryMarker(bytes.TrimRight(line, "\r\n")) {
			continue
		}
		w.found = true
		rest := w.buf.Bytes() // whatever arrived after the marker in this same chunk
		w.buf.Reset()
		if len(rest) == 0 {
			return total, nil
		}
		if _, err := w.dst.Write(rest); err != nil {
			return 0, err
		}
		return total, nil
	}
	return total, nil
}

func isChatBoundaryMarker(line []byte) bool {
	for _, marker := range chatBoundaryMarkers {
		if bytes.Equal(line, marker) {
			return true
		}
	}
	return false
}
