# Honeyverse

A security honeypot that uses Claude as its brain. Instead of hardcoded fake responses, you describe the system you want to simulate in plain English — and Claude does the rest in real time.

## The Idea

Traditional honeypots are static. They pretend to be a specific system by replaying scripted responses. The moment an attacker goes slightly off-script, the illusion breaks.

Honeyverse works differently: every interaction is generated live by an LLM. You write a `SCENARIO.md` describing a system — its hostname, users, installed software, files, planted secrets, vulnerabilities — and Claude becomes that system. Commands produce consistent, plausible output. Files exist. Directories are navigable. Credentials work. The fake server feels real because it reasons about itself in context.

The attacker gets trapped in an infinite, believable environment while everything they do gets logged.

## How It Works

```
SCENARIO.md  ──▶  Claude (system prompt)
                       │
attacker types command  ──▶  Claude generates terminal output  ──▶  streamed back line by line
                       │
                  session log (.jsonl)
```

1. You write `SCENARIO.md` describing any system you want to simulate
2. The honeypot starts an SSH server
3. When an attacker connects, Claude validates their credentials against the scenario
4. Once in, every command they type is sent to Claude with the full conversation history
5. Claude responds as the real system would — maintaining filesystem state, tracking the working directory, generating realistic file contents
6. Everything is logged: credentials, commands, outputs

The conversation history *is* the state. Claude naturally stays consistent because it remembers what it already said.

## SSH Honeypot

```
ssh/
├── SCENARIO.md          # Describe your target system here
├── main.go              # Entry point
└── internal/
    ├── scenario/        # Loads SCENARIO.md
    ├── claude/          # Anthropic API client (auth + streaming shell)
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
| `--api-key` | `$ANTHROPIC_API_KEY` | Anthropic API key |

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

The more detail you provide, the more convincing the simulation. Planted secrets, realistic bash history, misconfigured services — Claude will honour all of it.

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

## Requirements

- Go 1.22+
- Anthropic API key
- For port 22 redirect: `iptables -t nat -A PREROUTING -p tcp --dport 22 -j REDIRECT --to-port 2222`
