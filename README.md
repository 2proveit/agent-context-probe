# Agent Context Probe

![Agent Context Probe Demo](demo.gif)

Agent Context Probe is a local proxy and Dashboard for inspecting Anthropic
Messages, OpenAI Chat Completions, and OpenAI Responses traffic. The production
build is one Go executable: the React Dashboard is compiled as a SPA and
embedded in the binary.

The default command is:

```bash
agent-context-probe start
```

The proxy, management API, health check, and Dashboard share
`http://127.0.0.1:3001`.

## Features

- Transparent Anthropic Messages, OpenAI Chat Completions, and OpenAI Responses proxying
- Streaming and non-streaming response capture
- SQLite request history with model, time, and header filters
- Session and parent/subagent trace views
- Normalized messages, reasoning, tool calls, tool results, token usage, and latency
- Optional Claude Code subagent routing to different upstream models
- One executable with an embedded Dashboard; Node.js is not required at runtime
- Local-only listening by default
- Sensitive request and response headers redacted before SQLite persistence
- Explicit token authentication for non-loopback listening

## Build from source

Building requires Go 1.20 or later and Node.js 20 or later. The resulting
executable does not require Go, Node.js, npm, or a separate SQLite library.

### macOS and Linux

```bash
git clone https://github.com/2proveit/agent-context-probe.git
cd agent-context-probe
make install
make build
./bin/agent-context-probe start
```

`./run.sh` performs the build and start sequence as a convenience.

### Windows

Run in PowerShell:

```powershell
git clone https://github.com/2proveit/agent-context-probe.git
Set-Location agent-context-probe
powershell -NoProfile -ExecutionPolicy Bypass -File .\run.ps1
```

The script builds `bin\agent-context-probe.exe` and starts the single process.

## Install a Release

Release archives contain the executable, configuration example, README,
LICENSE, and install/uninstall scripts. On macOS and Linux:

```bash
./scripts/install.sh --version 0.1.0-beta.1
export PATH="$HOME/.local/bin:$PATH" # if the installer reports it is missing
agent-context-probe version
```

On Windows:

```powershell
.\scripts\install.ps1 -Version 0.1.0-beta.1
agent-context-probe.exe version
```

Running the installer again atomically replaces the executable and preserves
configuration, request history, and backups. The uninstall scripts remove only
the executable; user data is retained. The Windows installer adds its install
directory to the user PATH and the Windows uninstaller removes that exact entry.

## CLI

```text
agent-context-probe start [--config FILE] [--data-dir DIR] [--host HOST] [--port PORT]
agent-context-probe doctor [--config FILE] [--data-dir DIR] [--host HOST] [--port PORT]
agent-context-probe version
```

Running the executable without a subcommand remains equivalent to `start`.

`doctor` validates the resolved configuration, embedded Dashboard, data
directory, database schema, and listen address without contacting an upstream
provider or modifying files. Run it while the service is stopped so it can test
the configured port.

### Standard user directories

| System  | Configuration                                                   | Request database and backups                            |
| ------- | --------------------------------------------------------------- | ------------------------------------------------------- |
| macOS   | `~/Library/Application Support/Agent Context Probe/config.yaml` | `~/Library/Application Support/Agent Context Probe/`    |
| Linux   | `${XDG_CONFIG_HOME:-~/.config}/agent-context-probe/config.yaml` | `${XDG_DATA_HOME:-~/.local/share}/agent-context-probe/` |
| Windows | `%APPDATA%\Agent Context Probe\config.yaml`                     | `%LOCALAPPDATA%\Agent Context Probe\`                   |

No configuration file is created automatically. The data directory is created
on first start. The application no longer discovers `.env`, parent-directory
configuration files, or a current-directory `requests.db`; source checkouts can
keep the legacy layout by using `./run.sh`, `run.ps1`, `--config`, `--data-dir`,
or `DB_PATH` explicitly.

## Use the proxy

Start Agent Context Probe, then open the Dashboard:

```text
http://127.0.0.1:3001/
```

For Claude Code or another Anthropic-compatible client:

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:3001
claude
```

PowerShell equivalent:

```powershell
$env:ANTHROPIC_BASE_URL = "http://127.0.0.1:3001"
claude
```

For an OpenAI-compatible client, use this local API prefix:

```text
http://127.0.0.1:3001/v1
```

Available endpoints:

- `POST /v1/messages`
- `POST /v1/chat/completions`
- `POST /v1/responses`
- `GET /v1/models`
- `GET /health`
- `GET /api/requests`
- `DELETE /api/requests`
- `GET /api/sessions`
- `GET /api/ui-config`

The removed `/api/grade-prompt` frontend route had no Go implementation and is
not part of the supported API.

## Security model

The default host is `127.0.0.1`. Cross-origin access is not enabled: the
Dashboard calls the Go API on the same origin.

The proxy forwards the original client credentials to the selected upstream,
subject to existing provider rules. A configured `OPENAI_API_KEY` replaces the
incoming OpenAI `Authorization` value for the upstream request. The stored copy
is separate: values for headers such as `Authorization`, `Proxy-Authorization`,
`Cookie`, `Set-Cookie`, and `X-Api-Key` are replaced with `[REDACTED]` before
being written to SQLite or returned by the management API. On startup, existing
rows are also migrated to remove those stored header values.

Selected configuration files, SQLite databases, and backups use mode `0600` on
Unix-like systems. Newly created data and backup directories use mode `0700`.

### Non-loopback listening

Listening on `0.0.0.0`, `::`, or a LAN address requires an access token of at
least 16 characters. Configure it through `ACP_ACCESS_TOKEN` or
`server.access_token`:

```bash
ACP_HOST=0.0.0.0 \
ACP_ACCESS_TOKEN='replace-with-a-long-random-value' \
agent-context-probe start
```

Remote API clients must send the token in
`X-Agent-Context-Probe-Token`. This control header is removed before proxying
and is never sent to an upstream provider. Browser users are redirected to
`/auth`; successful login creates an HttpOnly, SameSite=Strict session cookie.
Use a TLS reverse proxy when traffic crosses an untrusted network.

## Configuration

Copy `config.yaml.example` to `config.yaml` when file-based configuration is
needed:

```yaml
server:
  host: "127.0.0.1"
  port: 3001
  timeouts:
    read: 10m
    write: 10m
    idle: 10m

providers:
  anthropic:
    base_url: "https://api.anthropic.com"
    version: "2023-06-01"
    max_retries: 3
  openai:
    base_url: "https://api.openai.com/v1"
    # api_key: "prefer OPENAI_API_KEY"

storage:
  # Omit db_path to use the operating system standard data directory.
  # db_path: "/custom/path/requests.db"
  # backup_dir: "/custom/path/backups"
  max_capture_bytes: 10485760

web:
  show_raw_stream_events: false
  raw_request_max_display_chars: 0
  raw_response_max_display_chars: 0
```

Configuration value priority is command-line override, environment variable,
configuration file, then built-in default. Configuration file selection is
`--config`, then `ACP_CONFIG`, then the standard configuration path. Database
selection is `--data-dir`, then `DB_PATH`, then `ACP_DATA_DIR`, then
`storage.db_path`, then the standard data directory. Relative paths in YAML are
resolved from the selected configuration file directory.

Environment variables:

| Variable                | Purpose                                     |
| ----------------------- | ------------------------------------------- |
| `ACP_CONFIG`            | Explicit configuration file path            |
| `ACP_DATA_DIR`          | Data directory used when `DB_PATH` is unset |
| `ACP_HOST`              | Listen host; defaults to `127.0.0.1`        |
| `ACP_PORT`              | Listen port; defaults to `3001`             |
| `PORT`                  | Backward-compatible port alias              |
| `ACP_ACCESS_TOKEN`      | Required for non-loopback listening         |
| `READ_TIMEOUT`          | Go duration such as `30s` or `10m`          |
| `WRITE_TIMEOUT`         | Go duration                                 |
| `IDLE_TIMEOUT`          | Go duration                                 |
| `ANTHROPIC_FORWARD_URL` | Anthropic upstream base URL                 |
| `ANTHROPIC_VERSION`     | Anthropic API version                       |
| `ANTHROPIC_MAX_RETRIES` | Anthropic retry count                       |
| `OPENAI_BASE_URL`       | OpenAI-compatible upstream API prefix       |
| `OPENAI_API_KEY`        | Optional upstream OpenAI key                |
| `DB_PATH`               | SQLite database path                        |

`storage.max_capture_bytes` limits retained OpenAI request and response bytes;
it does not truncate the proxied exchange. The raw display limits affect only
Dashboard rendering and not the stored value.

### Database schema upgrades

SQLite schema versions are recorded with `PRAGMA user_version`. A database
whose schema is newer than the executable is rejected to prevent downgrade
writes. When an existing database requires an upgrade, Agent Context Probe:

1. removes persisted sensitive header values;
2. creates a consistent SQLite backup under the configured backup directory;
3. validates the backup with `PRAGMA quick_check`;
4. applies all pending migrations in one transaction.

Backup files are never deleted automatically. `doctor` reports the current and
target schema without performing the migration.

## Optional subagent routing

```yaml
subagents:
  enable: true
  mappings:
    code-reviewer: "gpt-4o"
    deep-reasoning: "o3"
```

Mappings are applied only when the matching Claude Code subagent definition is
available. Unmapped models continue through the normal provider selection.

## Docker

The runtime image contains one process and one exposed port. It has no Node.js
runtime or `node_modules`.

```bash
docker build -t agent-context-probe .
docker run --rm \
  -p 3001:3001 \
  -e ACP_ACCESS_TOKEN='replace-with-a-long-random-value' \
  -v ./data:/app/data \
  agent-context-probe
```

The container listens on `0.0.0.0`, so the access token is mandatory. Open
`http://127.0.0.1:3001/` and enter the same token. API clients must send the
control header described above. SQLite data is stored at
`/app/data/requests.db` by default.

## Development

```bash
make install
make build       # SPA build, asset synchronization, then Go build
make dev         # build and start the single process
make run-proxy   # build the SPA and run the Go process through go run
make run-web     # optional Vite development server with /api proxying
make clean
make db-reset
```

Release validation:

```bash
goreleaser check
goreleaser release --snapshot --clean --skip=publish
```

Pushing a `v*` tag triggers `.github/workflows/release.yml`. The workflow builds
the tag artifacts once without publishing, tests those exact archives through
install/start/upgrade/uninstall without Go, Node.js, or npm in `PATH`, then
uploads the same files to the GitHub Release and publishes the `linux/amd64`
plus `linux/arm64` GHCR image. Archives include SHA-256 checksums and SPDX JSON
SBOMs; GitHub generates provenance attestations for release artifacts and the
container image.

Validation commands:

```bash
cd proxy && go test ./...
cd ../web && npm run typecheck && npm test
cd ../web && npm run lint && npm run build
```

Type checking, tests, lint, and the production SPA build are separate checks;
run them independently when diagnosing an existing codebase violation.

## Project structure

```text
agent-context-probe/
├── proxy/
│   ├── cmd/agent-context-probe/  # CLI entry point
│   └── internal/
│       ├── app/                  # application composition and lifecycle
│       ├── buildinfo/            # injected version, commit, and build time
│       ├── capture/              # persistence redaction policy
│       ├── handler/              # protocol and management handlers
│       └── webui/                # embedded SPA and fallback handler
├── web/                          # React/Vite SPA source
├── Dockerfile                    # single-process runtime image
├── Makefile
├── run.ps1
└── run.sh
```

## License

MIT License. See [LICENSE](LICENSE).
