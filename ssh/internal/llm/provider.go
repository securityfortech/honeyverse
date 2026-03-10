package llm

import "context"

// Provider is the interface every LLM backend must implement.
type Provider interface {
	// ValidateAuth decides whether the given SSH credentials should be accepted.
	ValidateAuth(ctx context.Context, scenario, username, password string) bool

	// ExecuteCommand returns the full terminal output for a command.
	ExecuteCommand(ctx context.Context, scenario string, history []Message, command string) (string, error)

	// ExecuteCommandStream streams terminal output token-by-token, calling
	// onChunk for each text delta so the caller can write to the terminal live.
	ExecuteCommandStream(ctx context.Context, scenario string, history []Message, command string, onChunk func(string)) error
}

// Message is a single turn in the conversation history.
type Message struct {
	Role    string // "user" or "assistant"
	Content string
}
