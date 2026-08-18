package agenthost

import (
	"context"
	"testing"

	nbtool "github.com/yeyoos/nucleo-base/layer2-runtime-rails/tool"
	"github.com/yeyoos/nucleo-base/shared/api"
)

// dummyTool is a minimal tool.Tool for exercising registry contents without
// needing a real provider/adapter — the gate wiring only cares about names.
type dummyTool struct{ name string }

func (d dummyTool) Definition() api.ToolDef {
	return api.ToolDef{Name: d.name}
}
func (d dummyTool) Execute(_ context.Context, _ string) (string, bool) { return "", false }

func TestWrapRegistryWithGateStartsWithOnlyTheDecisionTool(t *testing.T) {
	base := nbtool.NewRegistry()
	base.Register(dummyTool{name: "bash"})
	base.Register(dummyTool{name: "read_file"})

	live, snapshot := wrapRegistryWithGate(&Host{}, base)

	assertOnlyNames(t, live, "atoms_decision")
	assertOnlyNames(t, snapshot, "atoms_decision")
}

func TestWrapRegistryWithGateInspectOpensAtomAndBaseTools(t *testing.T) {
	base := nbtool.NewRegistry()
	base.Register(dummyTool{name: "bash"})

	live, _ := wrapRegistryWithGate(&Host{}, base)

	if _, isErr := live.Execute(context.Background(), "atoms_decision", `{"action":"inspect"}`); isErr {
		t.Fatalf("inspect returned an error result")
	}
	assertOnlyNames(t, live, "bash", "atom")
}

func TestWrapRegistryWithGateSkipOpensBaseToolsWithoutAtom(t *testing.T) {
	base := nbtool.NewRegistry()
	base.Register(dummyTool{name: "bash"})

	live, _ := wrapRegistryWithGate(&Host{}, base)

	if _, isErr := live.Execute(context.Background(), "atoms_decision", `{"action":"skip"}`); isErr {
		t.Fatalf("skip returned an error result")
	}
	assertOnlyNames(t, live, "bash")
}

// TestWrapRegistryWithGateSnapshotSurvivesLiveMutation is the property
// Host.Run's per-turn reset depends on: once atoms_decision opens Phase 2
// (mutating `live` in place), `snapshot` must still hold only
// atoms_decision, untouched — otherwise resetting to it on the next turn
// wouldn't actually re-close the gate.
func TestWrapRegistryWithGateSnapshotSurvivesLiveMutation(t *testing.T) {
	base := nbtool.NewRegistry()
	base.Register(dummyTool{name: "bash"})

	live, snapshot := wrapRegistryWithGate(&Host{}, base)
	live.Execute(context.Background(), "atoms_decision", `{"action":"inspect"}`)

	assertOnlyNames(t, snapshot, "atoms_decision")

	// Reproduce Host.Run's per-turn reset and confirm it actually re-closes
	// the gate.
	live.ReplaceFrom(snapshot)
	assertOnlyNames(t, live, "atoms_decision")
}

func assertOnlyNames(t *testing.T, r *nbtool.Registry, want ...string) {
	t.Helper()
	gotDefs := r.Definitions()
	got := make(map[string]bool, len(gotDefs))
	for _, d := range gotDefs {
		got[d.Name] = true
	}
	if len(got) != len(want) {
		t.Fatalf("registry has %d tools %v, want exactly %v", len(got), namesOf(gotDefs), want)
	}
	for _, name := range want {
		if !got[name] {
			t.Fatalf("registry missing %q, has %v", name, namesOf(gotDefs))
		}
	}
}

func namesOf(defs []api.ToolDef) []string {
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.Name)
	}
	return out
}
