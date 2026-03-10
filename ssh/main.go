package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/honeyverse/ssh/internal/llm"
	"github.com/honeyverse/ssh/internal/logger"
	"github.com/honeyverse/ssh/internal/scenario"
	"github.com/honeyverse/ssh/internal/server"
)

func main() {
	var (
		scenarioFile = flag.String("scenario", "SCENARIO.md", "path to scenario markdown file")
		port         = flag.Int("port", 2222, "SSH listen port")
		logDir       = flag.String("log-dir", "sessions", "directory for session logs")
		hostKeyPath  = flag.String("host-key", "host_key", "path to persist the RSA host key")

		// LLM provider selection
		provider    = flag.String("provider", "anthropic", "LLM provider: anthropic | ollama")
		apiKey      = flag.String("api-key", "", "Anthropic API key (default: $ANTHROPIC_API_KEY)")
		ollamaURL   = flag.String("ollama-url", "http://localhost:11434", "Ollama base URL")
		ollamaModel = flag.String("ollama-model", "qwen2.5:0.5b", "Ollama model name")
	)
	flag.Parse()

	sc, err := scenario.Load(*scenarioFile)
	if err != nil {
		log.Fatalf("scenario: %v", err)
	}
	log.Printf("scenario: %q", sc.Name())

	var llmProvider llm.Provider
	switch *provider {
	case "anthropic":
		if *apiKey == "" {
			*apiKey = os.Getenv("ANTHROPIC_API_KEY")
		}
		if *apiKey == "" {
			log.Fatal("Anthropic API key required: set ANTHROPIC_API_KEY or use --api-key")
		}
		llmProvider = llm.NewAnthropic(*apiKey)
		log.Printf("provider: anthropic (claude-sonnet-4-5)")

	case "ollama":
		llmProvider = llm.NewOllama(*ollamaURL, *ollamaModel)
		log.Printf("provider: ollama  url=%s  model=%s", *ollamaURL, *ollamaModel)

	default:
		log.Fatalf("unknown provider %q — use 'anthropic' or 'ollama'", *provider)
	}

	lg, err := logger.New(*logDir)
	if err != nil {
		log.Fatalf("logger: %v", err)
	}

	srv, err := server.New(sc, llmProvider, lg, *port, *hostKeyPath)
	if err != nil {
		log.Fatalf("server: %v", err)
	}

	log.Printf("SSH honeypot on port %d", *port)

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
