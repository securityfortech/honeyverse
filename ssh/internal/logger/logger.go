package logger

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type EventType string

const (
	EventConnect     EventType = "connect"
	EventAuthAttempt EventType = "auth_attempt"
	EventAuthAccept  EventType = "auth_accept"
	EventAuthReject  EventType = "auth_reject"
	EventCommand     EventType = "command"
	EventOutput      EventType = "output"
	EventDisconnect  EventType = "disconnect"
)

type Event struct {
	SessionID string    `json:"session_id"`
	Timestamp time.Time `json:"timestamp"`
	Type      EventType `json:"type"`
	RemoteIP  string    `json:"remote_ip,omitempty"`
	Username  string    `json:"username,omitempty"`
	Password  string    `json:"password,omitempty"`
	Command   string    `json:"command,omitempty"`
	Output    string    `json:"output,omitempty"`
	Message   string    `json:"message,omitempty"`
}

type Logger struct {
	dir string
	mu  sync.Mutex
}

func New(dir string) (*Logger, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create log dir %q: %w", dir, err)
	}
	return &Logger{dir: dir}, nil
}

func (l *Logger) Log(e Event) {
	e.Timestamp = time.Now().UTC()

	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	data = append(data, '\n')

	path := filepath.Join(l.dir, e.SessionID+".jsonl")

	l.mu.Lock()
	defer l.mu.Unlock()

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(data)
}

// RemoteIP extracts the host from a net.Addr, stripping the port.
func RemoteIP(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}
