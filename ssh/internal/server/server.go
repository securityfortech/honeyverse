package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/honeyverse/ssh/internal/llm"
	"github.com/honeyverse/ssh/internal/logger"
	"github.com/honeyverse/ssh/internal/scenario"
	"github.com/honeyverse/ssh/internal/shell"
)

// Server wraps the gliderlabs SSH server with honeypot logic.
type Server struct {
	scenario *scenario.Scenario
	provider llm.Provider
	logger   *logger.Logger
	ssh      *ssh.Server
}

// New builds a Server listening on the given port.
// hostKeyPath is the file used to persist the RSA host key across restarts.
func New(sc *scenario.Scenario, provider llm.Provider, lg *logger.Logger, port int, hostKeyPath string) (*Server, error) {
	srv := &Server{
		scenario: sc,
		provider: provider,
		logger:   lg,
	}

	signer, err := loadOrGenerateHostKey(hostKeyPath)
	if err != nil {
		return nil, fmt.Errorf("host key: %w", err)
	}

	srv.ssh = &ssh.Server{
		Addr:            fmt.Sprintf(":%d", port),
		Handler:         srv.handleSession,
		PasswordHandler: srv.handlePassword,
		// Reject public-key auth to always capture a password attempt.
		PublicKeyHandler: func(_ ssh.Context, _ ssh.PublicKey) bool { return false },
	}
	srv.ssh.AddHostKey(signer)

	return srv, nil
}

// ListenAndServe starts the SSH server (blocks until error).
func (s *Server) ListenAndServe() error {
	return s.ssh.ListenAndServe()
}

// handlePassword is called for every authentication attempt.
func (s *Server) handlePassword(ctx ssh.Context, password string) bool {
	sid := sessionID()
	remoteIP := logger.RemoteIP(ctx.RemoteAddr())
	username := ctx.User()

	s.logger.Log(logger.Event{
		SessionID: sid,
		Type:      logger.EventAuthAttempt,
		RemoteIP:  remoteIP,
		Username:  username,
		Password:  password,
	})

	authCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	accepted := s.provider.ValidateAuth(authCtx, s.scenario.Content(), username, password)

	eventType := logger.EventAuthReject
	if accepted {
		eventType = logger.EventAuthAccept
		ctx.SetValue("session_id", sid)
	}

	s.logger.Log(logger.Event{
		SessionID: sid,
		Type:      eventType,
		RemoteIP:  remoteIP,
		Username:  username,
		Password:  password,
	})

	if accepted {
		log.Printf("[%s] AUTH ACCEPTED  user=%q pass=%q ip=%s", sid, username, password, remoteIP)
	} else {
		log.Printf("[%s] AUTH REJECTED  user=%q pass=%q ip=%s", sid, username, password, remoteIP)
	}

	return accepted
}

// handleSession runs the interactive shell after successful auth.
func (s *Server) handleSession(sess ssh.Session) {
	id, _ := sess.Context().Value("session_id").(string)
	if id == "" {
		id = sessionID()
	}

	remoteIP := logger.RemoteIP(sess.RemoteAddr())
	username := sess.User()

	s.logger.Log(logger.Event{
		SessionID: id,
		Type:      logger.EventConnect,
		RemoteIP:  remoteIP,
		Username:  username,
	})
	log.Printf("[%s] SESSION START  user=%q ip=%s", id, username, remoteIP)

	shell.New(id, username, s.scenario.Content(), s.provider, s.logger).Run(sess)

	s.logger.Log(logger.Event{
		SessionID: id,
		Type:      logger.EventDisconnect,
		RemoteIP:  remoteIP,
		Username:  username,
	})
	log.Printf("[%s] SESSION END    user=%q ip=%s", id, username, remoteIP)
}

// loadOrGenerateHostKey loads an existing RSA key from disk or creates one.
func loadOrGenerateHostKey(path string) (gossh.Signer, error) {
	if data, err := os.ReadFile(path); err == nil {
		if signer, err := gossh.ParsePrivateKey(data); err == nil {
			return signer, nil
		} else {
			log.Printf("warn: could not parse host key, regenerating: %v", err)
		}
	}

	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return nil, fmt.Errorf("generate RSA key: %w", err)
	}

	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		return nil, fmt.Errorf("create signer: %w", err)
	}

	if pemBytes := encodeRSAKey(key); pemBytes != nil {
		if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
			log.Printf("warn: could not persist host key to %s: %v", path, err)
		}
	}

	return signer, nil
}

func encodeRSAKey(key *rsa.PrivateKey) []byte {
	block, err := gossh.MarshalPrivateKey(key, "")
	if err != nil {
		return nil
	}
	return pem.EncodeToMemory(block)
}

func sessionID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-%08x", time.Now().UTC().Format("20060102-150405"), b)
}
