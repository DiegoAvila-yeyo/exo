package agenthost

import (
	"context"
	"os"
	"strings"
	"time"

	nbprovider "github.com/yeyoos/nucleo-base/layer2-runtime-rails/provider"
)

// defaultContextWindowTokens is the conservative generic fallback used when
// the active model's real context length can't be resolved any other way
// (piece 1, step 3 of build_prompt_SESSION_RECALL.md): "being approximately
// right beats not showing anything" for the token meter (piece 5).
const defaultContextWindowTokens = 200_000

// staticContextWindows is a small, hand-maintained table keyed by
// model-name substring, for providers with no catalog endpoint to query
// (ANTHROPIC_API_KEY/OPENAI_API_KEY direct — see buildProviderFromEnv).
// Same substring-matching spirit as provider/catalog.go's qualityPatterns:
// don't invent a different matching style. Covers CONFIGURING_PROVIDER.md's
// documented defaults (claude-sonnet-4-6, gpt-5-codex) plus obvious
// siblings. Matching is case-insensitive, first match wins, most specific
// patterns first.
var staticContextWindows = []struct {
	pattern string
	tokens  int
}{
	{"claude-opus-4", 200_000},
	{"claude-sonnet-4", 200_000},
	{"claude-haiku-4", 200_000},
	{"claude-3-5-sonnet", 200_000},
	{"claude-3-5-haiku", 200_000},
	{"claude-", 200_000},
	{"gpt-5-codex", 400_000},
	{"gpt-5", 400_000},
	{"gpt-4o", 128_000},
	{"gpt-4-turbo", 128_000},
	{"o3", 200_000},
	{"o1", 200_000},
	{"gemini-3", 1_000_000},
	{"gemini-2", 1_000_000},
	{"gemini-1.5-pro", 2_000_000},
	{"gemini-1.5-flash", 1_000_000},
	{"gemini-", 1_000_000},
}

// resolveContextWindowTokens implements the three-step fallback from
// build_prompt_SESSION_RECALL.md's "New wrinkle" section:
//  1. LiteLLM active -> best-effort FetchModels once, look up modelID's
//     ContextLen.
//  2. Otherwise (or on failure) -> staticContextWindows substring match.
//  3. Otherwise -> defaultContextWindowTokens.
//
// Never fails, never blocks startup on a slow/unreachable gateway for long
// (bounded by FetchModels' own 15s HTTP client timeout) — same best-effort,
// log-and-degrade spirit as openMemoryStoreBestEffort.
func resolveContextWindowTokens(ctx context.Context, modelID string) int {
	if strings.TrimSpace(os.Getenv("LITELLM_API_KEY")) != "" || strings.TrimSpace(os.Getenv("LITELLM_BASE_URL")) != "" {
		fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if models, err := nbprovider.FetchModels(fetchCtx); err == nil {
			for _, m := range models {
				if m.ID == modelID && m.ContextLen > 0 {
					return m.ContextLen
				}
			}
		}
	}

	lower := strings.ToLower(modelID)
	for _, entry := range staticContextWindows {
		if strings.Contains(lower, entry.pattern) {
			return entry.tokens
		}
	}

	return defaultContextWindowTokens
}
