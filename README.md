# Honeyverse 🔐🍯🤖

Honeyverse is a versatile AI-powered honeypot platform designed to simulate realistic digital environments and trap threat actors inside controlled deception scenarios. Instead of exposing real infrastructure, Honeyverse allows defenders to define custom attack scenarios while large language models dynamically generate a believable fake world around the attacker.

The system can emulate virtually any service, protocol, or infrastructure — SSH servers, web applications, APIs, cloud environments, enterprise networks, IoT devices, and industrial systems. Every interaction inside the environment is generated in real time by LLMs, including command outputs, system responses, file systems, logs, configurations, and service behaviors, creating a dynamic simulation that evolves as the attacker explores it.

From the attacker's perspective the environment appears authentic and interactive, while in reality they are operating inside a controlled simulation designed to observe behavior, capture techniques, gather high-value threat intelligence, and keep adversaries engaged in an infinite exploration loop without ever reaching real assets.

Honeyverse transforms traditional honeypots into adaptive deception environments where every attacker interaction becomes an opportunity to study tactics, delay intrusion campaigns, and strengthen defensive intelligence.

## How It Works

## How It Works

```
SCENARIO.md  ──▶  LLM (system prompt)
                       │
attacker types command  ──▶  LLM generates terminal output  ──▶  streamed back line by line
                       │
                  session log (.jsonl)
```

1. You write `SCENARIO.md` describing any system you want to simulate
2. The honeypot starts a service (e.g. SSH)
3. When an attacker connects, the LLM validates their credentials against the scenario
4. Once in, every command they type is sent to the LLM with the full conversation history
5. The LLM responds as the real system would — maintaining filesystem state, tracking the working directory, generating realistic file contents
6. Everything is logged: credentials, commands, outputs

The conversation history *is* the state. The LLM stays consistent because it remembers everything it already said.

## SSH Honeypot

```
ssh/
├── SCENARIO.md          # Describe your target system here
├── main.go              # Entry point
└── internal/
    ├── scenario/        # Loads SCENARIO.md
    ├── llm/             # Provider interface + Anthropic & Ollama backends
    ├── server/          # gliderlabs/ssh server, host key management
    ├── shell/           # PTY session: echo, backspace, line-buffered streaming
    └── logger/          # Per-session JSONL logs
```

### Quick Start

```bash
cd ssh
export ANTHROPIC_API_KEY=sk-ant-...
go run . --scenario SCENARIO.md --port 2222
```

```bash
# In another terminal — attack your own honeypot
ssh admin@localhost -p 2222
# password: admin123 (as defined in SCENARIO.md)
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--scenario` | `SCENARIO.md` | Path to scenario file |
| `--port` | `2222` | SSH listen port |
| `--log-dir` | `sessions/` | Directory for session logs |
| `--host-key` | `host_key` | Path to persist the RSA host key |
| `--provider` | `anthropic` | LLM backend: `anthropic` or `ollama` |
| `--api-key` | `$ANTHROPIC_API_KEY` | Anthropic API key |
| `--ollama-url` | `http://localhost:11434` | Ollama base URL |
| `--ollama-model` | `qwen2.5:0.5b` | Ollama model name |

### Writing a Scenario

`SCENARIO.md` is free-form markdown. Claude reads it as the system prompt. Describe whatever you want:

```markdown
# prod-db-01 — PostgreSQL Database Server

## System Identity
- Hostname: prod-db-01
- OS: Debian 11 (Bullseye)
- Uptime: ~120 days

## Users & Credentials
- postgres / postgres (weak default, should have been changed)
- backup / backup2023

## Installed Software
- PostgreSQL 14 running on port 5432
- ...

## Sensitive Files
- /etc/postgresql/14/main/pg_hba.conf — auth config
- /home/postgres/.pgpass — plaintext credentials
- ...

## Vulnerabilities
- Default postgres password never changed
- ...
```

The more detail you provide, the more convincing the simulation. Planted secrets, realistic bash history, misconfigured services — the LLM will honour all of it.

### Session Logs

Every session is logged to `sessions/<id>.jsonl`, one JSON object per line:

```json
{"session_id":"20240115-143022-a1b2c3d4","timestamp":"...","type":"auth_attempt","remote_ip":"203.0.113.45","username":"root","password":"toor"}
{"session_id":"20240115-143022-a1b2c3d4","timestamp":"...","type":"auth_accept","username":"root","password":"toor"}
{"session_id":"20240115-143022-a1b2c3d4","timestamp":"...","type":"command","username":"root","command":"cat /etc/shadow"}
{"session_id":"20240115-143022-a1b2c3d4","timestamp":"...","type":"output","username":"root","command":"cat /etc/shadow","output":"root:$6$..."}
```

Event types: `connect`, `auth_attempt`, `auth_accept`, `auth_reject`, `command`, `output`, `disconnect`

## Roadmap

- [ ] Wrong-password delay — slow down brute-force attempts
- [ ] Trap commands — deeper engagement when attacker runs sensitive commands
- [ ] Session replay — replay captured sessions from logs
- [ ] Webhook alerts — Slack/Discord notification on new session
- [ ] Multiple scenarios — run several honeypots on different ports simultaneously
- [ ] HTTP honeypot — fake web admin panel (WordPress, phpMyAdmin, etc.)
- [ ] DNS honeypot
- [ ] YAML scenario format with stricter structure

## LLM Providers

| Provider | Flag | Notes |
|----------|------|-------|
| Anthropic Claude | `--provider anthropic` | Best quality. Requires API key. |
| Ollama (local) | `--provider ollama` | Fully offline. Recommend 7B+ models. |

```bash
# Anthropic (default)
go run . --provider anthropic

# Ollama — local, no API key needed
go run . --provider ollama --ollama-model llama3.1:8b
go run . --provider ollama --ollama-model qwen2.5:7b
```

> **Note:** Models below 7B parameters typically cannot follow strict terminal-emulation instructions reliably.

## Requirements

- Go 1.22+
- Anthropic API key (for Anthropic provider) or Ollama running locally
- For port 22 redirect: `iptables -t nat -A PREROUTING -p tcp --dport 22 -j REDIRECT --to-port 2222`
