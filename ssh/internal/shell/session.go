package shell

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"

	"github.com/honeyverse/ssh/internal/llm"
	"github.com/honeyverse/ssh/internal/logger"
)

// Session holds all state for a single attacker interaction.
type Session struct {
	id         string
	username   string
	scenario   string
	history    []llm.Message
	lastPrompt string // last shell prompt line emitted by the LLM
	provider   llm.Provider
	logger     *logger.Logger
}

// New creates a Session.
func New(id, username, scenarioContent string, provider llm.Provider, lg *logger.Logger) *Session {
	return &Session{
		id:       id,
		username: username,
		scenario: scenarioContent,
		provider: provider,
		logger:   lg,
	}
}

// Run drives the interactive shell loop for the given SSH session.
func (s *Session) Run(sess ssh.Session) {
	// Fetch MOTD + initial prompt. Not added to history to keep command
	// context clean.
	motd := s.fetchMOTD()
	writeOutput(sess, motd)

	for {
		line, err := readLine(sess)
		if err != nil {
			return
		}

		s.logger.Log(logger.Event{
			SessionID: s.id,
			Type:      logger.EventCommand,
			Username:  s.username,
			Command:   line,
		})

		if line == "" {
			writeOutput(sess, s.lastPrompt)
			continue
		}

		if isExit(line) {
			_, _ = sess.Write([]byte("logout\r\n"))
			return
		}

		s.streamResponse(sess, line)

		s.logger.Log(logger.Event{
			SessionID: s.id,
			Type:      logger.EventOutput,
			Username:  s.username,
			Command:   line,
		})
	}
}

// fetchMOTD asks the LLM for the MOTD banner and initial prompt in one call.
// The result is NOT added to conversation history.
func (s *Session) fetchMOTD() string {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	initMsg := "__SYSTEM_INIT__: Output the login MOTD and last-login banner for this system, then on the very last line show the initial shell prompt (format: user@hostname:~$ ). Raw text only."
	output, err := s.provider.ExecuteCommand(ctx, s.scenario, nil, initMsg)
	if err != nil || output == "" {
		output = fmt.Sprintf("%s@host:~$ ", s.username)
	}
	s.lastPrompt = extractPrompt(output)

	// Ensure there is a blank line between the MOTD body and the prompt so
	// the first typed command never appears joined to the last MOTD line.
	lines := strings.Split(strings.TrimRight(output, "\r\n"), "\n")
	if len(lines) >= 2 {
		body := strings.Join(lines[:len(lines)-1], "\n")
		prompt := strings.TrimRight(lines[len(lines)-1], "\r")
		output = body + "\n\n" + prompt
	}
	return output
}

// streamResponse streams the LLM response line-by-line to the SSH session.
// Lines are flushed as soon as they're complete — no character-drip effect.
func (s *Session) streamResponse(sess ssh.Session, command string) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	var full strings.Builder    // accumulates the full response for history
	var lineBuf strings.Builder // buffers tokens until a complete line is ready
	firstLine := true           // track first line to filter command echo

	err := s.provider.ExecuteCommandStream(ctx, s.scenario, s.history, command, func(chunk string) {
		full.WriteString(chunk)
		lineBuf.WriteString(chunk)

		// Flush every complete line as soon as it arrives.
		for {
			buf := lineBuf.String()
			idx := strings.Index(buf, "\n")
			if idx < 0 {
				break
			}
			raw := strings.TrimRight(buf[:idx], "\r")
			lineBuf.Reset()
			lineBuf.WriteString(buf[idx+1:])

			// Skip markdown code fences (e.g. mistral wraps file output in ```).
			if isCodeFence(raw) {
				firstLine = false
				continue
			}
			// Skip command echo on first line (e.g. mistral echoes "prompt$ command").
			if firstLine {
				firstLine = false
				if isCommandEcho(raw, command) {
					continue
				}
			}
			// Normalise to \r\n for PTY clients.
			_, _ = sess.Write([]byte(raw + "\r\n"))
		}
	})

	// Flush the trailing prompt (no \n after it — cursor stays on prompt line).
	if remainder := lineBuf.String(); remainder != "" {
		_, _ = sess.Write([]byte(remainder))
	}

	if err != nil {
		fallback := fmt.Sprintf("bash: %s: command not found\r\n%s", firstWord(command), s.lastPrompt)
		_, _ = sess.Write([]byte(fallback))
		return
	}

	output := full.String()
	if p := extractPrompt(output); p != "" {
		s.lastPrompt = p
	}
	s.history = append(s.history,
		llm.Message{Role: "user", Content: command},
		llm.Message{Role: "assistant", Content: output},
	)
}

// extractPrompt returns the last non-empty line of output.
func extractPrompt(output string) string {
	lines := strings.Split(strings.TrimRight(output, "\r\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimRight(lines[i], "\r"); line != "" {
			return line
		}
	}
	return ""
}

// writeOutput writes output to the SSH session with correct \r\n endings.
// The last line has no trailing newline so the cursor stays on the prompt.
func writeOutput(w io.Writer, output string) {
	if output == "" {
		return
	}
	normalised := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(output)
	lines := strings.Split(normalised, "\n")

	var buf bytes.Buffer
	for i, line := range lines {
		buf.WriteString(line)
		if i < len(lines)-1 {
			buf.WriteString("\r\n")
		}
	}
	_, _ = w.Write(buf.Bytes())
}

// readLine reads one line character-by-character with local echo, backspace
// support, and control-code handling.
func readLine(sess ssh.Session) (string, error) {
	var line []byte
	buf := make([]byte, 1)

	for {
		n, err := sess.Read(buf)
		if n > 0 {
			c := buf[0]
			switch {
			case c == '\r' || c == '\n':
				_, _ = sess.Write([]byte("\r\n"))
				return string(line), nil

			case c == 0x03: // Ctrl+C
				_, _ = sess.Write([]byte("^C\r\n"))
				return "", nil

			case c == 0x04: // Ctrl+D — EOF on empty line
				if len(line) == 0 {
					return "", io.EOF
				}

			case c == 0x7f || c == 0x08: // Backspace / DEL
				if len(line) > 0 {
					line = line[:len(line)-1]
					_, _ = sess.Write([]byte("\b \b"))
				}

			case c == 0x1b: // ESC sequence (arrow keys etc.) — discard
				discard := make([]byte, 2)
				_, _ = sess.Read(discard)

			case c >= 0x20: // Printable character
				line = append(line, c)
				_, _ = sess.Write([]byte{c})
			}
		}
		if err != nil {
			return string(line), io.EOF
		}
	}
}

// isCommandEcho returns true when a model echoes the command back as the first
// output line (e.g. "admin@host:~$ ls" or just "$ ls" / "ls").
func isCommandEcho(line, command string) bool {
	cmd := strings.TrimSpace(command)
	trimmed := strings.TrimSpace(line)
	// Match "...$ command" patterns (prompt echo).
	if idx := strings.LastIndex(trimmed, "$ "); idx >= 0 {
		return strings.TrimSpace(trimmed[idx+2:]) == cmd
	}
	// Match bare command echo (line equals the command exactly).
	return trimmed == cmd
}

// isCodeFence returns true for markdown code-fence lines (``` or ~~~).
func isCodeFence(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

func isExit(line string) bool {
	switch strings.TrimSpace(line) {
	case "exit", "logout", "quit":
		return true
	}
	return false
}

func firstWord(s string) string {
	if parts := strings.Fields(s); len(parts) > 0 {
		return parts[0]
	}
	return s
}
