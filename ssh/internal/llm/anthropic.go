package llm

import (
	"context"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const anthropicModel = anthropic.ModelClaudeSonnet4_5

// AnthropicProvider implements Provider using the Anthropic API.
type AnthropicProvider struct {
	api anthropic.Client
}

// NewAnthropic creates an AnthropicProvider with the given API key.
func NewAnthropic(apiKey string) *AnthropicProvider {
	return &AnthropicProvider{
		api: anthropic.NewClient(option.WithAPIKey(apiKey)),
	}
}

func (p *AnthropicProvider) ValidateAuth(ctx context.Context, scenario, username, password string) bool {
	prompt := AuthPrompt(scenario, username, password)
	msg, err := p.api.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropicModel,
		MaxTokens: 16,
		Messages: []anthropic.MessageParam{
			{Role: anthropic.MessageParamRoleUser, Content: []anthropic.ContentBlockParamUnion{
				{OfText: &anthropic.TextBlockParam{Text: prompt}},
			}},
		},
	})
	if err != nil || len(msg.Content) == 0 {
		return false
	}
	answer := strings.TrimSpace(strings.ToUpper(msg.Content[0].Text))
	return strings.HasPrefix(answer, "ACCEPT")
}

func (p *AnthropicProvider) ExecuteCommand(ctx context.Context, scenario string, history []Message, command string) (string, error) {
	var buf strings.Builder
	err := p.ExecuteCommandStream(ctx, scenario, history, command, func(chunk string) {
		buf.WriteString(chunk)
	})
	return buf.String(), err
}

func (p *AnthropicProvider) ExecuteCommandStream(ctx context.Context, scenario string, history []Message, command string, onChunk func(string)) error {
	sys := SystemPrompt(scenario)
	messages := buildAnthropicMessages(history, command)

	stream := p.api.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropicModel,
		MaxTokens: 2048,
		System:    []anthropic.TextBlockParam{{Text: sys}},
		Messages:  messages,
	})
	defer stream.Close()

	for stream.Next() {
		event := stream.Current()
		if delta, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
			if text, ok := delta.Delta.AsAny().(anthropic.TextDelta); ok && text.Text != "" {
				onChunk(text.Text)
			}
		}
	}
	return stream.Err()
}

func buildAnthropicMessages(history []Message, command string) []anthropic.MessageParam {
	messages := make([]anthropic.MessageParam, 0, len(history)+1)
	for _, h := range history {
		role := anthropic.MessageParamRoleUser
		if h.Role == "assistant" {
			role = anthropic.MessageParamRoleAssistant
		}
		messages = append(messages, anthropic.MessageParam{
			Role:    role,
			Content: []anthropic.ContentBlockParamUnion{{OfText: &anthropic.TextBlockParam{Text: h.Content}}},
		})
	}
	messages = append(messages, anthropic.MessageParam{
		Role:    anthropic.MessageParamRoleUser,
		Content: []anthropic.ContentBlockParamUnion{{OfText: &anthropic.TextBlockParam{Text: command}}},
	})
	return messages
}

var _ Provider = (*AnthropicProvider)(nil)
