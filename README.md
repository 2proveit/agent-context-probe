# Agent Context Probe

![Agent Context Probe Demo](demo.gif)

A transparent proxy for forwarding, capturing, and visualizing Anthropic Messages,
OpenAI Chat Completions, and OpenAI Responses API requests, with optional Claude
Code subagent routing to different LLM providers.

## What It Does

Agent Context Probe serves three main purposes:

1. **Multi-protocol API Proxy**: Transparently forwards Anthropic Messages,
   OpenAI Chat Completions, and OpenAI Responses requests.
2. **Request Monitor**: Stores requests and responses in SQLite and displays
   normalized messages, tool calls, tool results, model routing, latency, token
   usage, and raw payloads in the web dashboard.
3. **Session View**: Groups captured requests into sessions (including
   delegated subagent sessions) and visualizes each session as a trace
   waterfall with context/token usage and reasoning content.
4. **Agent Routing (Optional)**: Routes specific Claude Code subagents to
   different LLM providers (for example, route `code-reviewer` to GPT-4o).

## Features

- **Transparent Proxy**: Forwards Anthropic and OpenAI-compatible requests without changing the client workflow
- **Agent Routing (Optional)**: Map specific Claude Code agents to different LLM models
- **Request Monitoring**: SQLite-based logging of all API interactions
- **Multi-protocol Endpoints**: Supports `/v1/messages`, `/v1/chat/completions`, and `/v1/responses`
- **Web Dashboard**: Refreshable request history with detailed request and response visualization
- **Request Filtering**: Filter history by time range, model, or request header
- **Tool-call Inspection**: Normalized tool calls and tool results across supported protocols
- **Streaming Inspection**: Stores raw SSE while presenting the completed structured response
- **Session View**: Groups requests by session (via session/parent-session headers),
  including nested subagent sessions, with a trace waterfall, context/token usage,
  and reasoning-content breakdown
- **Easy Setup**: One-command startup for both services

## Quick Start

### Prerequisites

- Go 1.20 or later
- Node.js 20 or later, including npm
- Claude Code
- Windows 10/11 with Windows PowerShell 5.1 or later, or macOS with Bash
- Docker Desktop only if using the containerized deployment

The proxy uses a pure-Go SQLite driver. Native Windows builds do not require
GCC, MinGW, or a separate SQLite installation.

### Windows startup

Run the following commands in PowerShell:

```powershell
git clone https://github.com/2proveit/agent-context-probe.git
Set-Location agent-context-probe
Copy-Item config.yaml.example config.yaml
powershell -NoProfile -ExecutionPolicy Bypass -File .\run.ps1
```

`run.ps1` downloads the Go modules, installs the web dependencies when needed,
builds `bin\proxy.exe`, and starts both services. Keep that PowerShell window
open. In a second PowerShell window, route Claude Code through the proxy:

```powershell
$env:ANTHROPIC_BASE_URL = "http://localhost:3001"
claude
```

Subsequent starts use the same command:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\run.ps1
```

### macOS startup

Run the following commands in Terminal:

```bash
git clone https://github.com/2proveit/agent-context-probe.git
cd agent-context-probe
cp config.yaml.example config.yaml
./run.sh
```

`run.sh` downloads the Go modules, installs the web dependencies when needed,
builds `bin/proxy`, and starts both services. Keep that terminal open. In a
second terminal, route Claude Code through the proxy:

```bash
export ANTHROPIC_BASE_URL=http://localhost:3001
claude
```

Subsequent starts can use either `./run.sh` or `make dev`.

### Docker startup

On Windows, configure Docker Desktop to use Linux containers. The default
single-line `docker build` and `docker run` commands below work in PowerShell;
the multi-line persistent-volume examples use Bash syntax.

1. **Clone the repository**

   ```bash
   git clone https://github.com/2proveit/agent-context-probe.git
   cd agent-context-probe
   ```

2. **Configure the proxy**

   ```bash
   cp config.yaml.example config.yaml
   # Edit config.yaml as needed
   ```

3. **Build and run with Docker**

   ```bash
   # Build the image
   docker build -t agent-context-probe .

   # Run with default settings
   docker run -p 3001:3001 -p 5173:5173 agent-context-probe
   ```

4. **Run with persistent data and custom configuration**

   ```bash
   # Create a data directory for persistent SQLite database
   mkdir -p ./data

   # Option 1: Run with config file (recommended)
   docker run -p 3001:3001 -p 5173:5173 \
     -v ./data:/app/data \
     -v ./config.yaml:/app/config.yaml:ro \
     agent-context-probe

   # Option 2: Run with environment variables
   docker run -p 3001:3001 -p 5173:5173 \
     -v ./data:/app/data \
     -e ANTHROPIC_FORWARD_URL=https://api.anthropic.com \
     -e PORT=3001 \
     -e WEB_PORT=5173 \
     agent-context-probe
   ```

5. **Docker Compose (alternative)**

   ```yaml
   # docker-compose.yml
   version: "3.8"
   services:
     agent-context-probe:
       build: .
       ports:
         - "3001:3001"
         - "5173:5173"
       volumes:
         - ./data:/app/data
         - ./config.yaml:/app/config.yaml:ro # Mount config file
       environment:
         - ANTHROPIC_FORWARD_URL=https://api.anthropic.com
         - PORT=3001
         - WEB_PORT=5173
         - DB_PATH=/app/data/requests.db
   ```

   Then run: `docker-compose up`

### Using the Proxy

For Claude Code or another Anthropic-compatible client, set the base URL in the
shell that will launch the client.

Windows PowerShell:

```powershell
$env:ANTHROPIC_BASE_URL = "http://localhost:3001"
```

macOS:

```bash
export ANTHROPIC_BASE_URL=http://localhost:3001
```

Then launch Claude Code using the `claude` command.

This will route Claude Code's requests through the proxy for monitoring.

For an OpenAI-compatible client, set the client's base URL to:

```text
http://localhost:3001/v1
```

Configure this in the client itself. In the proxy process,
`OPENAI_BASE_URL` means the upstream provider URL, not the local listening URL.

The proxy accepts:

- `POST /v1/messages` for the Anthropic Messages API
- `POST /v1/chat/completions` for OpenAI Chat Completions
- `POST /v1/responses` for the OpenAI Responses API
- `GET /v1/models` for OpenAI-compatible model discovery

Client authorization headers are forwarded by default. If
`providers.openai.api_key` or `OPENAI_API_KEY` is configured, that key replaces
the incoming authorization header for requests forwarded to the OpenAI
provider.

### Access Points

- **Web Dashboard**: http://localhost:5173
- **API Proxy**: http://localhost:3001
- **Health Check**: http://localhost:3001/health

## Advanced Usage

### Running Services Separately

On macOS, the services can be run independently in separate terminals:

```bash
# Run proxy only
make run-proxy

# Run web interface only (in another terminal)
make run-web
```

On Windows, run the services in two PowerShell windows:

```powershell
# PowerShell window 1
Set-Location proxy
go run .\cmd\proxy

# PowerShell window 2, from the repository root
Set-Location web
npm.cmd run dev
```

### Available Make Commands (macOS)

```bash
make install    # Install all dependencies
make build      # Build both services
make dev        # Run in development mode
make clean      # Clean build artifacts
make db-reset   # Reset database
make help       # Show all commands
```

## Configuration

### Basic Setup

Create a `config.yaml` file (or copy from `config.yaml.example`):

```yaml
server:
  port: 3001

providers:
  anthropic:
    base_url: "https://api.anthropic.com"

  openai: # if enabling subagent routing
    api_key: "your-openai-key" # Or set OPENAI_API_KEY env var
    base_url: "https://api.openai.com/v1"

storage:
  db_path: "requests.db"
  max_capture_bytes: 10485760

web:
  show_raw_stream_events: false
  raw_request_max_display_chars: 0
  raw_response_max_display_chars: 0
```

`providers.openai.base_url` is an API prefix. The proxy appends
`/chat/completions` or `/responses` while preserving any gateway path prefix.
Streaming events are stored as raw SSE and as a structured result only after a
valid terminal event is received.

`storage.max_capture_bytes` limits only the request and response data retained
in SQLite; it does not truncate the proxied exchange. Set it to `0` for
unlimited capture.

`web.raw_request_max_display_chars` and
`web.raw_response_max_display_chars` limit only the number of characters
rendered in the web UI. Both default to `0` (unlimited), and copy actions still
use the complete value retained in SQLite.

> **Security:** Request and response headers are stored and displayed exactly
> as received, including authorization and cookie headers. Protect access to
> both the dashboard and `requests.db`.

### Subagent Configuration (Optional)

The proxy supports routing specific Claude Code agents to different LLM providers. This is an **optional** feature that's disabled by default.

#### Enabling Subagent Routing

1. **Enable the feature** in `config.yaml`:

```yaml
subagents:
  enable: true # Set to true to enable subagent routing
  mappings:
    code-reviewer: "gpt-4o"
    data-analyst: "o3"
    doc-writer: "gpt-3.5-turbo"
```

2. **Set up your Claude Code agents** following Anthropic's official documentation:
   - 📖 **[Claude Code Subagents Documentation](https://docs.anthropic.com/en/docs/claude-code/sub-agents)**

3. **How it works**: When Claude Code uses a subagent that matches one of your mappings, the proxy will automatically route the request to the specified model instead of Claude.

### Practical Examples

**Example 1: Code Review Agent → GPT-4o**

```yaml
# config.yaml
subagents:
  enable: true
  mappings:
    code-reviewer: "gpt-4o"
```

Use case: Route code review tasks to GPT-4o for faster responses while keeping complex coding tasks on Claude.

**Example 2: Reasoning Agent → O3**

```yaml
# config.yaml
subagents:
  enable: true
  mappings:
    deep-reasoning: "o3"
```

Use case: Send complex reasoning tasks to O3 while using Claude for general coding.

**Example 3: Multiple Agents**

```yaml
# config.yaml
subagents:
  enable: true
  mappings:
    streaming-systems-engineer: "o3"
    frontend-developer: "gpt-4o-mini"
    security-auditor: "gpt-4o"
```

Use case: Different specialists for different tasks, optimizing for speed/cost/quality.

### Environment Variables

Override config via environment:

- `PORT` - Proxy server port
- `READ_TIMEOUT`, `WRITE_TIMEOUT`, `IDLE_TIMEOUT` - Go duration strings such as `30s` or `10m`
- `ANTHROPIC_FORWARD_URL` - Anthropic upstream base URL
- `ANTHROPIC_VERSION` - Anthropic API version
- `ANTHROPIC_MAX_RETRIES` - Maximum Anthropic retry count
- `OPENAI_BASE_URL` - OpenAI-compatible upstream API prefix
- `OPENAI_API_KEY` - Optional upstream OpenAI API key
- `DB_PATH` - SQLite database path

Subagent mappings are configured under `subagents.mappings` in `config.yaml`.
When running the web service separately, `BACKEND_URL` selects the proxy backend
and defaults to `http://localhost:3001`.

### Docker Environment Variables

All environment variables can be configured when running the Docker container:

| Variable                | Default                     | Description                                                                 |
| ----------------------- | --------------------------- | --------------------------------------------------------------------------- |
| `PORT`                  | `3001`                      | Proxy server port                                                           |
| `WEB_PORT`              | `5173`                      | Web dashboard port                                                          |
| `READ_TIMEOUT`          | `600`                       | Server read timeout (seconds; Docker entrypoint converts it to a duration)  |
| `WRITE_TIMEOUT`         | `600`                       | Server write timeout (seconds; Docker entrypoint converts it to a duration) |
| `IDLE_TIMEOUT`          | `600`                       | Server idle timeout (seconds; Docker entrypoint converts it to a duration)  |
| `ANTHROPIC_FORWARD_URL` | `https://api.anthropic.com` | Target Anthropic API URL                                                    |
| `ANTHROPIC_VERSION`     | `2023-06-01`                | Anthropic API version                                                       |
| `ANTHROPIC_MAX_RETRIES` | `3`                         | Maximum retry attempts                                                      |
| `OPENAI_BASE_URL`       | `https://api.openai.com/v1` | Target OpenAI-compatible API prefix                                         |
| `OPENAI_API_KEY`        | empty                       | Optional upstream OpenAI API key                                            |
| `DB_PATH`               | `/app/data/requests.db`     | SQLite database path                                                        |

Example with custom configuration:

```bash
docker run -p 3001:3001 -p 5173:5173 \
  -v ./data:/app/data \
  -e PORT=8080 \
  -e WEB_PORT=3000 \
  -e ANTHROPIC_FORWARD_URL=https://api.anthropic.com \
  -e DB_PATH=/app/data/custom.db \
  agent-context-probe
```

## Project Structure

```
agent-context-probe/
├── proxy/                  # Go proxy server
│   ├── cmd/               # Application entry points
│   ├── internal/          # Internal packages
│   └── go.mod            # Go dependencies
├── web/                   # React Remix frontend
│   ├── app/              # Remix application
│   └── package.json      # Node dependencies
├── run.ps1               # Windows PowerShell start script
├── run.sh                # macOS Bash start script
├── .env.example          # Environment template
└── README.md            # This file
```

## Features in Detail

### Request Monitoring

- All API requests logged to SQLite database
- Request history filtering by time range, model, and header
- Request/response body inspection
- Protocol-aware message, tool-call, and tool-result rendering
- Status, timing, token usage, and routing metadata

### Web Dashboard

- Refreshable captured-request history
- Interactive request explorer
- Normalized Anthropic Messages, OpenAI Chat Completions, and OpenAI Responses display
- Raw request and response inspection
- Optional raw SSE event viewer via `web.show_raw_stream_events`
- Independently configurable raw request/response display limits (unlimited by default)

### Session View

Toggle the dashboard's "Sessions" tab to view captured requests grouped into
sessions instead of a flat list. Sessions are derived from request headers
(`X-Session-Affinity`, `X-OpenCode-Session`, `X-Claude-Code-Session-Id`, and
`X-Parent-Session-Id` for parent/child linkage) — no additional configuration
is required. Each session shows:

- A tree + waterfall visualization of model calls, tool calls, and delegated
  subagent calls, with measured latency/end-to-end timing per step
- Context usage (input, cached input, output tokens, and prefix cache hit rate)
- Reasoning ("thinking") content separated from regular output, with token counts
- Nested subagent sessions rendered as child cards within their parent session

## License

MIT License - see [LICENSE](LICENSE) for details.
