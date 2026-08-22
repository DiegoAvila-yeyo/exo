// Command gatebug2repro reproduces (and, after the fix, verifies) Bug 2 from
// build_prompt_YEYO_FIX_AMBOS.md: lexical bias in the atoms_decision gate's
// initial inspect/skip call — paraphrased tasks that don't share literal
// wording with an atom's description get "skip" even when the atom applies.
// Same pattern as cmd/gatesmoke/cmd/gatebug1repro — throwaway, not part of
// exo's normal build/install.
//
// Each run is a single, isolated turn (own session id) — this bug is about
// the *first* decision, not cross-turn context (that's Bug 1, already
// fixed), so turns must not accumulate history that could confound the
// result. Selecting -case picks which scenario to run; -list prints them.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/DiegoAvila-yeyo/exo/agenthost"
	"github.com/DiegoAvila-yeyo/exo/appconfig"
	"github.com/DiegoAvila-yeyo/exo/sessions"
)

type scenario struct {
	name string
	msg  string
	note string
}

var scenarios = []scenario{
	// The literal-phrasing control from uso-real-report.md's Conversación
	// 13 — calques protocolo-hulk's own wording ("300 líneas"). Expected to
	// already fire "inspect" before any fix; a regression here would mean
	// the fix broke the easy case, not just the hard one.
	{"literal-300-lineas", "Un archivo .py va a terminar teniendo bastante más de 300 líneas, ¿lo dejo así?", "atom: protocolo-hulk (literal — known-good baseline)"},

	// The known failing case from uso-real-report.md's Conversación 12,
	// paraphrased, casual, no numbers.
	{"gigante", "el archivo utils.py se puso gigante, ¿lo dejo así o hago algo?", "atom: protocolo-hulk (paraphrased — known failure)"},

	// New casual paraphrasings, none seen in the few-shot examples added to
	// the fix (centro-navegacion-fewshot.md's own examples: "terminó siendo
	// un moustro" / "helper que sume dos números") — for testing
	// generalization, not memorization of one phrase.
	{"desmadre", "el módulo de pagos se me fue de las manos, ya no entiendo ni yo qué hace cada parte", "atom: protocolo-hulk (new casual phrasing #1)"},
	{"kilometrico", "tenemos un archivo kilométrico en el backend, capaz valga la pena partirlo", "atom: protocolo-hulk (new casual phrasing #2)"},
	{"trivial-helper", "che, agregame una función que valide un email en el archivo de validaciones", "atom: config-load-typed/rails-conventions-style (trivial-sounding code change)"},

	// Controls — genuinely irrelevant, general knowledge. Must stay "skip"
	// after the fix (P(skip|irrelevante) shouldn't degrade).
	{"control-tcp-udp", "¿por qué en Go se prefiere composición sobre herencia?", "control: general knowledge, no project convention involved"},
	{"control-gracias", "gracias, eso era todo", "control: closing remark, no task at all"},
}

func main() {
	list := flag.Bool("list", false, "print the scenario names and exit")
	name := flag.String("case", "", "scenario name to run (see -list)")
	flag.Parse()

	if *list {
		for _, s := range scenarios {
			fmt.Printf("%-20s %s\n", s.name, s.note)
		}
		return
	}
	if strings.TrimSpace(*name) == "" {
		fmt.Fprintln(os.Stderr, "usage: gatebug2repro -case <name> (see -list)")
		os.Exit(1)
	}
	var picked *scenario
	for i := range scenarios {
		if scenarios[i].name == *name {
			picked = &scenarios[i]
			break
		}
	}
	if picked == nil {
		fmt.Fprintf(os.Stderr, "unknown case %q (see -list)\n", *name)
		os.Exit(1)
	}

	if err := os.Setenv("EXO_YEYO_GATE", "1"); err != nil {
		fmt.Fprintln(os.Stderr, "setenv EXO_YEYO_GATE:", err)
		os.Exit(1)
	}
	if strings.TrimSpace(os.Getenv("EXO_AGENT_ROOT_PATH")) == "" {
		fmt.Fprintln(os.Stderr, "EXO_AGENT_ROOT_PATH must be set to a throwaway copy of the repo (see gatebug1repro's comment on why)")
		os.Exit(1)
	}

	envPath, err := appconfig.EnvFilePath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolve env file:", err)
		os.Exit(1)
	}
	if err := appconfig.LoadEnvFile(envPath); err != nil {
		fmt.Fprintln(os.Stderr, "load env file:", err)
		os.Exit(1)
	}

	manager := sessions.New()
	host, err := agenthost.New(context.Background(), manager, nil, nil, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agenthost.New:", err)
		os.Exit(1)
	}
	defer host.Close()

	sessionCtx := agenthost.ContextWithSessionID(context.Background(), "gatebug2repro-"+picked.name)
	fmt.Printf("\n===== case %q: %q =====\n", picked.name, picked.msg)
	var out strings.Builder
	if err := host.Run(sessionCtx, picked.msg, &out); err != nil {
		fmt.Fprintf(os.Stderr, "Run: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(out.String())
}
