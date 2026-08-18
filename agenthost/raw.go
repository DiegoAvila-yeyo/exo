package agenthost

import (
	"context"
	"strings"

	"github.com/yeyoos/nucleo-base/shared/api"
)

// RawSend runs exactly one provider completion — no Agent loop, no
// Coordinator phases, no ensureTask/decoratePrompt/autoInvestigate, no
// tools at all. Built for exp1h to test whether "wants to read the file
// first" (seen in exp1g's Experimento L, 0/14) survives once every layer of
// exo's own agentic machinery is removed, leaving only system prompt + user
// message → one response.
func RawSend(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	provider, err := buildProviderFromEnv(systemPrompt)
	if err != nil {
		return "", err
	}
	messages := []api.Message{
		{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: userPrompt}}},
	}
	resp, err := provider.Send(ctx, systemPrompt, messages, nil)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, block := range resp.Content {
		if block.Type == api.BlockText {
			b.WriteString(block.Text)
		}
	}
	return b.String(), nil
}
