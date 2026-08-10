package agenthost

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	anthropicapi "github.com/anthropics/anthropic-sdk-go"
	nbprovider "github.com/yeyoos/nucleo-base/layer2-runtime-rails/provider"
)

const (
	defaultAnthropicModel = "claude-sonnet-4-6"
	defaultLiteLLMModel   = "primary"
	defaultOpenAIModel    = "gpt-5-codex"
	defaultMaxTokens      = int64(4096)
)

func buildProviderFromEnv(systemPrompt string) (nbprovider.Provider, error) {
	maxTokens, err := providerMaxTokensFromEnv()
	if err != nil {
		return nil, err
	}

	switch {
	case strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) != "":
		model := envOrDefault("ANTHROPIC_MODEL", defaultAnthropicModel)
		return nbprovider.NewAnthropicProvider(anthropicapi.Model(model), maxTokens, systemPrompt), nil
	case strings.TrimSpace(os.Getenv("LITELLM_API_KEY")) != "":
		model := envOrDefault("LITELLM_MODEL", defaultLiteLLMModel)
		return nbprovider.NewLiteLLMProvider(model, systemPrompt, maxTokens), nil
	case strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) != "":
		model := envOrDefault("OPENAI_MODEL", defaultOpenAIModel)
		return nbprovider.NewOpenAIProvider(model, systemPrompt, maxTokens), nil
	default:
		return nil, fmt.Errorf("no provider configured: set ANTHROPIC_API_KEY, LITELLM_API_KEY, or OPENAI_API_KEY")
	}
}

func providerMaxTokensFromEnv() (int64, error) {
	raw := strings.TrimSpace(os.Getenv("EXO_AGENT_MAX_TOKENS"))
	if raw == "" {
		return defaultMaxTokens, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("invalid EXO_AGENT_MAX_TOKENS %q: must be a positive integer", raw)
	}
	return value, nil
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
