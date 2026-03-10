package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// OllamaProvider implements Provider using a local Ollama instance.
// It uses the native /api/chat endpoint with NDJSON streaming.
type OllamaProvider struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewOllama creates an OllamaProvider.
// baseURL defaults to http://localhost:11434 if empty.
func NewOllama(baseURL, model string) *OllamaProvider {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &OllamaProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		client:  &http.Client{Timeout: 0}, // no global timeout; rely on ctx
	}
}

// --- Ollama API types ---

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Think    bool            `json:"think"` // disable thinking mode (qwen3 etc.)
}

type ollamaChunk struct {
	Message ollamaMessage `json:"message"`
	Done    bool          `json:"done"`
	Error   string        `json:"error,omitempty"`
}

// --- Provider implementation ---

func (p *OllamaProvider) ValidateAuth(ctx context.Context, scenario, username, password string) bool {
	prompt := AuthPrompt(scenario, username, password)

	var result strings.Builder
	err := p.stream(ctx, []ollamaMessage{
		{Role: "user", Content: prompt},
	}, func(chunk string) {
		result.WriteString(chunk)
	})
	if err != nil {
		return false
	}
	answer := strings.TrimSpace(strings.ToUpper(result.String()))
	// Be flexible: accept if response contains ACCEPT anywhere.
	return strings.Contains(answer, "ACCEPT")
}

func (p *OllamaProvider) ExecuteCommand(ctx context.Context, scenario string, history []Message, command string) (string, error) {
	var buf strings.Builder
	err := p.ExecuteCommandStream(ctx, scenario, history, command, func(chunk string) {
		buf.WriteString(chunk)
	})
	return buf.String(), err
}

func (p *OllamaProvider) ExecuteCommandStream(ctx context.Context, scenario string, history []Message, command string, onChunk func(string)) error {
	sys := SystemPrompt(scenario)
	messages := buildOllamaMessages(sys, history, command)

	// Strip <think>...</think> blocks that reasoning models emit.
	inThink := false
	var thinkBuf strings.Builder

	return p.stream(ctx, messages, func(chunk string) {
		filtered := filterThinking(chunk, &inThink, &thinkBuf)
		if filtered != "" {
			onChunk(filtered)
		}
	})
}

// stream posts to /api/chat and calls onChunk for each content delta.
func (p *OllamaProvider) stream(ctx context.Context, messages []ollamaMessage, onChunk func(string)) error {
	body, err := json.Marshal(ollamaRequest{
		Model:    p.model,
		Messages: messages,
		Stream:   true,
		Think:    false,
	})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama returned HTTP %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var chunk ollamaChunk
		if err := json.Unmarshal(line, &chunk); err != nil {
			continue
		}
		if chunk.Error != "" {
			return fmt.Errorf("ollama error: %s", chunk.Error)
		}
		if chunk.Message.Content != "" {
			onChunk(chunk.Message.Content)
		}
		if chunk.Done {
			break
		}
	}
	return scanner.Err()
}

func buildOllamaMessages(system string, history []Message, command string) []ollamaMessage {
	messages := make([]ollamaMessage, 0, len(history)+2)
	messages = append(messages, ollamaMessage{Role: "system", Content: system})
	for _, h := range history {
		messages = append(messages, ollamaMessage{Role: h.Role, Content: h.Content})
	}
	messages = append(messages, ollamaMessage{Role: "user", Content: command})
	return messages
}

// filterThinking strips <think>...</think> blocks emitted by reasoning models.
// It is stateful: inThink and buf persist across chunks.
func filterThinking(chunk string, inThink *bool, buf *strings.Builder) string {
	var out strings.Builder
	for _, ch := range chunk {
		buf.WriteRune(ch)
		s := buf.String()

		if *inThink {
			if strings.HasSuffix(s, "</think>") {
				*inThink = false
				buf.Reset()
			}
		} else {
			if strings.HasSuffix(s, "<think>") {
				*inThink = true
				// Flush whatever came before <think>.
				before := strings.TrimSuffix(s, "<think>")
				out.WriteString(before)
				buf.Reset()
			}
		}
	}
	if !*inThink {
		remaining := buf.String()
		// Only flush if we're sure we're not at the start of a tag.
		if !strings.HasPrefix("<think>", remaining) {
			out.WriteString(remaining)
			buf.Reset()
		}
	}
	return out.String()
}

// Ensure OllamaProvider satisfies Provider at compile time.
var _ Provider = (*OllamaProvider)(nil)

// keep time imported for potential future use
var _ = time.Second
