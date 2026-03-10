package claude

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const model = anthropic.ModelClaudeSonnet4_5

// systemPrompt is injected at the start of every shell session.
const systemPrompt = `You are a bash shell running on a real Linux system. You must behave EXACTLY as a real terminal would.

SCENARIO DESCRIPTION:
%s

STRICT RULES:
- Output ONLY raw terminal output. No markdown, no code fences, no explanations.
- If a command produces no output (cd, export, unset, true, etc.) — return nothing at all.
- Stay 100%% in character. You ARE this system, not an AI assistant.
- Be fully consistent: files you list with ls must be readable with cat; directories must be navigable.
- Track the current working directory across commands. Update it on successful cd calls.
- Always end your response with the shell prompt on a new line in this exact format:
  username@hostname:current_dir$
  (note the trailing space after $; adjust username, hostname and current_dir based on the scenario and cd history)
- For destructive commands (rm -rf, mkfs, shutdown, reboot) — simulate their effect convincingly.
- For privilege errors — respond with realistic "Permission denied" or "sudo: command not found".
- Simulate realistic sudo behaviour (prompt for password, accept the user's known password).
- When you receive __SYSTEM_INIT__, output the MOTD/last-login banner followed by the initial shell prompt on the very last line.`

// authPrompt is used for a one-shot credential check.
const authPrompt = `Based on the following scenario, decide whether this SSH login attempt should be accepted.

SCENARIO:
%s

LOGIN ATTEMPT:
  Username: %s
  Password: %s

Respond with exactly ONE word: ACCEPT or REJECT.
Do not explain. Do not add punctuation.`

// Message represents a single turn in the conversation history.
type Message struct {
	Role    string // "user" or "assistant"
	Content string
}

// Client wraps the Anthropic API.
type Client struct {
	api anthropic.Client
}

// New creates a Client with the given API key.
func New(apiKey string) *Client {
	return &Client{
		api: anthropic.NewClient(option.WithAPIKey(apiKey)),
	}
}

// ValidateAuth asks Claude whether the given credentials should be accepted.
func (c *Client) ValidateAuth(ctx context.Context, scenario, username, password string) bool {
	prompt := fmt.Sprintf(authPrompt, scenario, username, password)

	msg, err := c.api.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: 16,
		Messages: []anthropic.MessageParam{
			{
				Role: anthropic.MessageParamRoleUser,
				Content: []anthropic.ContentBlockParamUnion{
					{OfText: &anthropic.TextBlockParam{Text: prompt}},
				},
			},
		},
	})
	if err != nil {
		return false
	}
	if len(msg.Content) == 0 {
		return false
	}
	answer := strings.TrimSpace(strings.ToUpper(msg.Content[0].Text))
	return strings.HasPrefix(answer, "ACCEPT")
}

// ExecuteCommand sends the command to Claude and returns the full response.
// Used for auth checks and MOTD where we need the complete text before acting.
func (c *Client) ExecuteCommand(ctx context.Context, scenario string, history []Message, command string) (string, error) {
	var buf strings.Builder
	err := c.ExecuteCommandStream(ctx, scenario, history, command, func(chunk string) {
		buf.WriteString(chunk)
	})
	return buf.String(), err
}

// ExecuteCommandStream streams the response token-by-token, calling onChunk for
// each text delta. This lets the caller write to the terminal immediately.
func (c *Client) ExecuteCommandStream(ctx context.Context, scenario string, history []Message, command string, onChunk func(string)) error {
	sys := fmt.Sprintf(systemPrompt, scenario)
	messages := buildMessages(history, command)

	stream := c.api.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     model,
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

func buildMessages(history []Message, command string) []anthropic.MessageParam {
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
