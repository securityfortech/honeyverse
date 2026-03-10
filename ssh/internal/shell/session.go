package shell

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"

	"github.com/honeyverse/ssh/internal/claude"
	"github.com/honeyverse/ssh/internal/logger"
)

// Session holds all state for a single attacker interaction.
type Session struct {
	id         string
	username   string
	scenario   string
	history    []claude.Message
	lastPrompt string // last shell prompt line emitted by Claude
	claude     *claude.Client
	logger     *logger.Logger
}

// New creates a Session.
func New(id, username, scenarioContent string, cl *claude.Client, lg *logger.Logger) *Session {
	return &Session{
		id:       id,
		username: username,
		scenario: scenarioContent,
		claude:   cl,
		logger:   lg,
	}
}

// Run drives the interactive shell loop for the given SSH session.
func (s *Session) Run(sess ssh.Session) {
	// Ask Claude for the MOTD + initial shell prompt in one call.
	// This result is intentionally NOT added to conversation history so it
	// doesn't pollute subsequent command context.
	motd := s.fetchMOTD()
	writeOutput(sess, motd)

	for {
		line, err := readLine(sess)
		if err != nil {
			// EOF or Ctrl+D — client disconnected.
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

		s.streamClaude(sess, line)

		s.logger.Log(logger.Event{
			SessionID: s.id,
			Type:      logger.EventOutput,
			Username:  s.username,
			Command:   line,
		})
	}
}

// fetchMOTD calls Claude for the initial MOTD + first shell prompt.
// The result is NOT added to conversation history.
func (s *Session) fetchMOTD() string {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	initMsg := "__SYSTEM_INIT__: Output the login MOTD and last-login banner for this system, then on the very last line show the initial shell prompt (format: user@hostname:~$). Raw text only."
	output, err := s.claude.ExecuteCommand(ctx, s.scenario, nil, initMsg)
	if err != nil || output == "" {
		// Fallback: static prompt so the attacker sees something.
		output = fmt.Sprintf("%s@host:~$ ", s.username)
	}
	s.lastPrompt = extractPrompt(output)
	return output
}

// streamClaude streams the response line-by-line to the SSH session.
// Each complete line is written as soon as Claude generates it — no chatbot
// character-drip effect, just realistic line-at-a-time output.
func (s *Session) streamClaude(sess ssh.Session, command string) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var full strings.Builder   // accumulates the complete response for history
	var lineBuf strings.Builder // buffers until we have a full line to flush

	flushLine := func(line string) {
		// Convert \n → \r\n and write immediately.
		_, _ = sess.Write([]byte(line))
	}

	err := s.claude.ExecuteCommandStream(ctx, s.scenario, s.history, command, func(chunk string) {
		full.WriteString(chunk)
		lineBuf.WriteString(chunk)

		// Flush every complete line we've accumulated.
		for {
			buf := lineBuf.String()
			idx := strings.Index(buf, "\n")
			if idx < 0 {
				break
			}
			line := buf[:idx+1] // includes the \n
			// Replace \n with \r\n for PTY.
			line = strings.ReplaceAll(line, "\r\n", "\n")
			line = strings.ReplaceAll(line, "\n", "\r\n")
			flushLine(line)
			lineBuf.Reset()
			lineBuf.WriteString(buf[idx+1:])
		}
	})

	// Flush any remaining content (the prompt — no trailing \n).
	if remainder := lineBuf.String(); remainder != "" {
		_, _ = sess.Write([]byte(remainder))
	}

	output := full.String()

	if err != nil {
		cmd := firstWord(command)
		fallback := fmt.Sprintf("bash: %s: command not found\r\n%s", cmd, s.lastPrompt)
		_, _ = sess.Write([]byte(fallback))
		return
	}

	if p := extractPrompt(output); p != "" {
		s.lastPrompt = p
	}

	s.history = append(s.history,
		claude.Message{Role: "user", Content: command},
		claude.Message{Role: "assistant", Content: output},
	)
}

// extractPrompt returns the last non-empty line of output, which Claude
// is instructed to make the shell prompt.
func extractPrompt(output string) string {
	lines := strings.Split(strings.TrimRight(output, "\r\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimRight(lines[i], "\r")
		if line != "" {
			return line
		}
	}
	return ""
}

// writeOutput writes terminal output to the SSH session with \r\n line endings.
// The last line (the shell prompt) is written WITHOUT a trailing newline so the
// cursor stays on the prompt line, ready for input.
func writeOutput(w io.Writer, output string) {
	if output == "" {
		return
	}
	// Normalise to \n-only, then split.
	normalised := strings.ReplaceAll(output, "\r\n", "\n")
	normalised = strings.ReplaceAll(normalised, "\r", "\n")
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

// readLine reads one line from the SSH session character by character,
// echoing printable characters, handling backspace, and detecting control codes.
// Returns io.EOF when the client disconnects or sends Ctrl+D on an empty line.
func readLine(sess ssh.Session) (string, error) {
	var line []byte
	buf := make([]byte, 1)

	for {
		n, err := sess.Read(buf)
		if n > 0 {
			c := buf[0]
			switch {
			case c == '\r' || c == '\n':
				// Enter — echo newline and return the accumulated line.
				_, _ = sess.Write([]byte("\r\n"))
				return string(line), nil

			case c == 0x03: // Ctrl+C — interrupt, return empty with a visual cue.
				_, _ = sess.Write([]byte("^C\r\n"))
				return "", nil

			case c == 0x04: // Ctrl+D — EOF if line is empty, otherwise ignore.
				if len(line) == 0 {
					return "", io.EOF
				}

			case c == 0x7f || c == 0x08: // Backspace / DEL
				if len(line) > 0 {
					line = line[:len(line)-1]
					_, _ = sess.Write([]byte("\b \b")) // erase character on screen
				}

			case c == 0x1b: // ESC — start of an escape sequence (arrow keys etc.)
				// Read and discard the rest of the sequence (typically 2 more bytes).
				discard := make([]byte, 2)
				_, _ = sess.Read(discard)

			case c >= 0x20: // Printable ASCII
				line = append(line, c)
				_, _ = sess.Write([]byte{c}) // echo back
			}
		}
		if err != nil {
			return string(line), io.EOF
		}
	}
}

func isExit(line string) bool {
	switch strings.TrimSpace(line) {
	case "exit", "logout", "quit":
		return true
	}
	return false
}

func firstWord(s string) string {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
