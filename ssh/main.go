package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/honeyverse/ssh/internal/claude"
	"github.com/honeyverse/ssh/internal/logger"
	"github.com/honeyverse/ssh/internal/scenario"
	"github.com/honeyverse/ssh/internal/server"
)

func main() {
	var (
		scenarioFile = flag.String("scenario", "SCENARIO.md", "path to scenario markdown file")
		port         = flag.Int("port", 2222, "SSH listen port")
		logDir       = flag.String("log-dir", "sessions", "directory for session JSON logs")
		hostKeyPath  = flag.String("host-key", "host_key", "path to persist the RSA host key")
		apiKey       = flag.String("api-key", "", "Anthropic API key (default: $ANTHROPIC_API_KEY)")
	)
	flag.Parse()

	if *apiKey == "" {
		*apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if *apiKey == "" {
		log.Fatal("Anthropic API key required: set ANTHROPIC_API_KEY or use --api-key")
	}

	sc, err := scenario.Load(*scenarioFile)
	if err != nil {
		log.Fatalf("scenario: %v", err)
	}
	log.Printf("scenario loaded: %q", sc.Name())

	lg, err := logger.New(*logDir)
	if err != nil {
		log.Fatalf("logger: %v", err)
	}

	cl := claude.New(*apiKey)

	srv, err := server.New(sc, cl, lg, *port, *hostKeyPath)
	if err != nil {
		log.Fatalf("server init: %v", err)
	}

	log.Printf("SSH honeypot listening on port %d  (scenario: %s)", *port, sc.Name())

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Fatalf("server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down")
}
