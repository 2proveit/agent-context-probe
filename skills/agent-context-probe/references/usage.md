# Setup, identity and evidence queries

- [Setup and client integration](#setup-and-client-integration)
- [Session identity](#session-identity)
- [API queries](#api-queries)
- [Fault experiments](#fault-experiments)

## Setup and client integration

Prefer an existing release executable. From a source checkout, use `make install`
and `make build`, then `./bin/agent-context-probe`. Build requires Go 1.21+ and
Node.js 20+; the built executable is standalone. Supported CLI commands:

```text
agent-context-probe version
agent-context-probe doctor [--config FILE] [--data-dir DIR] [--host HOST] [--port PORT]
agent-context-probe start [--config FILE] [--data-dir DIR] [--host HOST] [--port PORT]
```

Run `doctor` while stopped: it checks configuration, embedded assets, storage
schema and whether it can bind the listen address, without contacting a provider.
It will report a port failure against an already running instance. There is no
`stop`, `status`, `sessions` or `requests` CLI subcommand; inspect HTTP APIs.

Default proxy/Dashboard origin: `http://127.0.0.1:3001`.

| Direction | Setting | Value |
| --- | --- | --- |
| Proxy → OpenAI-compatible upstream | `providers.openai.base_url` or proxy-process `OPENAI_BASE_URL` | Actual upstream API prefix, usually ending in `/v1` |
| Proxy → Anthropic upstream | `providers.anthropic.base_url` or `ANTHROPIC_FORWARD_URL` | Actual upstream origin |
| OpenAI-compatible target client → proxy | Client `base_url` / equivalent | `http://127.0.0.1:3001/v1` |
| Anthropic target client → proxy | Client base URL or target-process `ANTHROPIC_BASE_URL` | `http://127.0.0.1:3001` |

Keep these settings process-scoped. In particular, exporting the local client
`OPENAI_BASE_URL` into the proxy process can create a forwarding loop. Retain the
intended upstream model name and credentials. Proxy-process `OPENAI_API_KEY`,
when configured, replaces the incoming OpenAI authorization header.

Select configuration with `--config`, then `ACP_CONFIG`, then the platform's
standard config path. CLI settings override environment, YAML and defaults.
Select database location with `--data-dir`, `DB_PATH`, `ACP_DATA_DIR`, YAML
`storage.db_path`, then the standard data directory. Relative YAML paths resolve
from the configuration directory. Do not assume the current directory's
`requests.db` is in use.

For non-loopback listening, the proxy requires `ACP_ACCESS_TOKEN` (or its config
equivalent). Send it as `X-Agent-Context-Probe-Token` on management and model
requests; it is separate from the model-provider credential. `/health` remains
unauthenticated. Keep tokens out of logs, URLs and captured artifacts.

## Session identity

Select the first nonempty identity in this order, matching header names without
case sensitivity:

`Thread-Id` → `X-Thread-Id` → `X-Session-Affinity` → `X-OpenCode-Session` →
`X-Claude-Code-Session-Id` → `Session-Id` → `X-Session-Id` → `X-Conversation-Id`.

Select the parent from `X-Codex-Parent-Thread-Id`, then `X-Parent-Thread-Id`, then
`X-Parent-Session-Id`. The parent's value must match the selected parent identity.
If an older installed release lacks aliases, use `X-Session-Affinity` and
`X-Parent-Session-Id`. Check its version before assuming current checkout behavior.

| Client | Integration point |
| --- | --- |
| Codex CLI / TS SDK | Inspect native `Thread-Id`, `Session-Id`, `X-Codex-Parent-Thread-Id`; do not override them with shared IDs |
| OpenCode | Inspect supplied `X-OpenCode-Session` / affinity headers; use provider-specific base URL configuration |
| Claude Code / Agent SDK | Inspect supplied session headers; use `ANTHROPIC_CUSTOM_HEADERS` (newline-separated) / SDK environment when explicit identity is needed |
| LangChain / LangGraph | Set `ChatOpenAI(default_headers=...)`; separately set the graph's checkpoint thread ID if needed |
| OpenAI Agents SDK | Set `ModelSettings(extra_headers=...)` or model-client headers; a memory `Session` alone is insufficient |
| AutoGen | Set OpenAI model client `default_headers` |
| CrewAI native OpenAI provider | Set LLM `default_headers`; verify selected provider emits a supported protocol |

For generic clients, use `{"X-Thread-Id": "unique-conversation-thread-id"}`.
For a separately displayed child, use a new ID and
`{"X-Parent-Thread-Id": "parent-thread-id"}`. Reuse the IDs across tool rounds
and retries; do not create one per model call or mutate a shared client's global
headers during parallel runs. Environment-level Claude headers are inherited by
subagents and cannot alone separate their timelines. Only promise separate
subagent traces when the host propagates per-agent IDs independently.

Only OpenAI Chat Completions, Responses and Anthropic Messages over HTTP/JSON/SSE
are supported. Do not claim native Gemini, Bedrock, Vertex, WebSocket model
transport, or framework-internal execution tracing. A Responses
`previous_response_id` cannot recover unseen history by itself.

If this source checkout is available, read `docs/agent-framework-compatibility.md`
for framework snippets. The bundled instructions above remain usable when this
skill is copied into another project's skill directory.

## API queries

Use the same proxy origin, without `/v1`, for management routes:

| Method and route | Query / response |
| --- | --- |
| `GET /health` | `{status, timestamp}`; checks proxy availability only |
| `GET /api/sessions` | `page`, `limit`, `model`, `header`, `since`; returns `{sessions, total}` |
| `GET /api/sessions?session=ID` | Exact per-agent identity; returns `{session: {summary, requests, toolWindows, children}}` |
| `GET /api/requests` | Same list filters; returns `{requests, total}` |
| `GET /api/ui-config` | Dashboard display configuration |

`model` and `header` are case-insensitive substring filters; `header` searches
stored header names/values, not a `name=value` expression. `since` must be RFC3339
with an explicit timezone. Lists paginate; session grouping precedes pagination.
Filters restrict source requests, so a filtered tree can omit parents or steps.
Inspect a suspected incomplete tree again with those filters removed.

Use `requestId`, not the frontend-only normalized `id`, when correlating records.
There is no HTTP endpoint for retrieving one request by ID; select it from paged
`requests`, or fetch its known session. Read `body` for structured content;
OpenAI raw SSE is in `response.bodyText`, Messages chunks in
`response.streamingChunks`. Raw fields may be absent for nonstreaming captures.

Fetch a filtered session list or one tree without putting credentials in shell
arguments. Set `PROBE_URL`, optional `PROBE_SESSION_ID`, `PROBE_HEADER`,
`PROBE_MODEL`, `PROBE_SINCE`, `PROBE_PAGE` and `PROBE_LIMIT` as needed. Read the
access token from the existing `ACP_ACCESS_TOKEN` environment variable:

```python
import json
import os
from urllib.parse import urlencode
from urllib.request import Request, urlopen

base = os.environ.get("PROBE_URL", "http://127.0.0.1:3001").rstrip("/")
params = {"page": os.environ.get("PROBE_PAGE", "1"),
          "limit": os.environ.get("PROBE_LIMIT", "10")}
for key, env in [("session", "PROBE_SESSION_ID"), ("header", "PROBE_HEADER"),
                 ("model", "PROBE_MODEL"), ("since", "PROBE_SINCE")]:
    if os.environ.get(env):
        params[key] = os.environ[env]
headers = {}
if os.environ.get("ACP_ACCESS_TOKEN"):
    headers["X-Agent-Context-Probe-Token"] = os.environ["ACP_ACCESS_TOKEN"]
request = Request(base + "/api/sessions?" + urlencode(params), headers=headers)
with urlopen(request, timeout=30) as response:
    data = json.load(response)
# Print only IDs and status first; inspect selected content separately.
rows = ([data["session"]["summary"]] if "session" in data
        else data.get("sessions", []))
for row in rows:
    print(json.dumps({k: row.get(k) for k in
        ["sessionId", "parentSessionId", "status", "requestCount"]}))
```

Requests can include sensitive prompts or tool outputs even though credentials
are redacted by the proxy. Keep raw records local and disclose only relevant,
redacted excerpts. Prefer API reads over manipulating the live SQLite file.

## Fault experiments

Read `docs/response-fault-injection.md` in the source checkout before changing
`response_corruption`. Keep it disabled for ordinary evidence collection. For an
explicitly requested test, isolate the test instance/data directory, record the
original configuration, select the requested mode, and restore the original
state afterward. Upstream requests remain real and may incur model usage.

Inspect `X-Agent-Context-Probe-Corruption-*` response headers for applied faults.
SQLite stores upstream-before-injection bytes; compare them with captured
harness-side bytes. Guard-triggered retries or cancellation may leave incomplete
upstream captures. Do not attribute injected output or its resulting interruption
to the provider without checking the fault configuration and headers.
