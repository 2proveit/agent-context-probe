# Agent framework compatibility

Agent Context Probe inspects **model HTTP traffic**, not a framework's internal
execution graph. Point the framework's model client at the proxy and select a
matching configured upstream. The supported wire protocols are Anthropic
Messages (`/v1/messages`), OpenAI Chat Completions (`/v1/chat/completions`), and
OpenAI Responses (`/v1/responses`), with JSON and SSE responses.

## Integration matrix

| Client / framework | Model protocol covered | Session integration |
| --- | --- | --- |
| Codex CLI / TypeScript SDK | Responses | Uses native `Thread-Id`; `X-Codex-Parent-Thread-Id` links children. Shared `Session-Id` must not merge agents. |
| DeepSeek Harness (`dsh`) | Messages with the official DeepSeek adapter | Uses native `X-DeepSeek-Harness-Session-Id` and links subagents when `dsh_session_log` carries their lineage. |
| OpenCode | Chat Completions or Messages, according to provider | Recognizes `X-OpenCode-Session` and existing affinity headers when supplied. |
| Claude Code / Claude Agent SDK | Messages | Recognizes `X-Claude-Code-Session-Id` when supplied; custom headers can supply an explicit identity. |
| LangChain / LangGraph with ChatOpenAI | Chat Completions or Responses | Set model client `default_headers`; LangGraph checkpoint `configurable.thread_id` alone is not a proxy integration. |
| OpenAI Agents SDK | Responses or Chat Completions | Set `ModelSettings.extra_headers` or model-client headers; a memory `Session` alone is not a proxy integration. |
| AutoGen OpenAI model client | Chat Completions | Set `default_headers`. |
| CrewAI native OpenAI LLM | Chat Completions or Responses | Set `default_headers`. Other provider backends must emit one of the supported protocols. |

These are protocol and identity contracts, not promises about every release or
provider backend of each framework. Tests exercise representative model payloads
and mock HTTP capture through SQLite and the session API; they do not run the
third-party frameworks or make live model requests. Native Gemini, Bedrock,
Vertex, framework tracing exports, and model transports other than HTTP/SSE are
not implemented by these integrations.

## Identity contract

Use one stable ID per displayed agent/conversation thread and a different ID for
each independent user session. Reuse it across model calls, tool rounds and
retries. Supply the direct parent's ID for a child thread. Do not reuse a root's
per-thread ID for every child.

Accepted identity headers, in priority order (case-insensitive; blank values are
skipped):

1. `Thread-Id`
2. `X-Thread-Id`
3. `X-DeepSeek-Harness-Session-Id`
4. `X-Session-Affinity`
5. `X-OpenCode-Session`
6. `X-Claude-Code-Session-Id`
7. `Session-Id`
8. `X-Session-Id`
9. `X-Conversation-Id`

Parent headers, in priority order: `X-Codex-Parent-Thread-Id`,
`X-Parent-Thread-Id`, `X-Parent-Session-Id`. Their values must match the parent's
selected identity. Missing parents remain visible as roots. Parent metadata can
arrive on a later request. Shared session membership alone cannot establish a
direct parent (a child may actually be a grandchild).

Do not use a user ID, model name, client name, trace/span ID, or run-wide ID as a
substitute for a conversation thread. Framework memory/checkpoint identifiers
are not read from prompts or arbitrary body metadata. Requests with no recognized
transport header remain separate as `request:<requestId>` instead of being
silently merged. For concurrency, use per-client or per-request headers; do not
mutate a shared global header dictionary while other runs are executing.

### DeepSeek Harness

The [official DeepSeek adapter](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/deepseek-llm-api-wire-extensions.md)
sends its native session header to the configured gateway for calls associated
with a session. No custom identity header is needed in that case. Direct calls
without a session ID and requests from adapters that omit any recognized
identity header remain separate. The Harness user ID is not a session key.

Unless an explicit parent header is present, ACP uses
`dsh_session_log.session.parentSession` when the version-1 extension marks
the session as a `subagent` and its ID matches the selected native session ID.
This also supports nested subagents. Ordinary forks do not count as subagents.
If the log upload is disabled or the parent was not captured, ACP displays the
session separately unless another capture supplies the missing information.
ACP stores only the derived parent ID from the extension. Older Messages
captures did not retain this metadata; a fresh capture with lineage is needed
to link those sessions.

### Common Python setup

Use the model and credentials accepted by the configured upstream. These snippets
show client configuration only; retain the host application's run/tool logic.
Create the following once per conversation, and reuse `headers` for its calls:

```python
from uuid import uuid4

thread_id = str(uuid4())
headers = {"X-Thread-Id": thread_id}
base_url = "http://127.0.0.1:3001/v1"
```

For a child with its own model client/settings:

```python
child_headers = {
    "X-Thread-Id": str(uuid4()),
    "X-Parent-Thread-Id": thread_id,
}
```

If the host uses a single shared model conversation for handoffs, keep one thread
rather than inventing subagent trees from tool names. Separate child timelines
require the host to propagate distinct child identities to model requests.

### LangChain / LangGraph

```python
from langchain_openai import ChatOpenAI

llm = ChatOpenAI(
    model="your-upstream-model",
    base_url=base_url,
    default_headers=headers,
)
# Bind this llm in the graph. If you use a checkpointer, also supply its
# configurable.thread_id when invoking the graph; it serves a separate purpose.
```

### OpenAI Agents SDK

```python
from openai import AsyncOpenAI
from agents import Agent, ModelSettings, OpenAIResponsesModel

client = AsyncOpenAI(base_url=base_url)
agent = Agent(
    name="assistant",
    model=OpenAIResponsesModel(model="your-upstream-model", openai_client=client),
    model_settings=ModelSettings(extra_headers=headers),
)
# Use child_headers on a separately displayed child agent's ModelSettings.
```

### AutoGen

```python
from autogen_ext.models.openai import OpenAIChatCompletionClient

client = OpenAIChatCompletionClient(
    model="gpt-4o",
    base_url=base_url,
    default_headers=headers,
)
# Non-OpenAI model names may also need AutoGen's model_info configuration.
```

### CrewAI (native OpenAI provider)

```python
from crewai import LLM

llm = LLM(
    model="openai/gpt-4o",
    base_url=base_url,
    default_headers=headers,
)
```

### Claude Code / Claude Agent SDK

For a separately launched Claude Code conversation:

```bash
ANTHROPIC_BASE_URL=http://127.0.0.1:3001 \
ANTHROPIC_CUSTOM_HEADERS="X-Thread-Id: $(uuidgen)" \
claude
```

If `ANTHROPIC_CUSTOM_HEADERS` is already set, append the identity line to its
existing newline-separated contents instead of discarding them. Reuse the ID
when resuming that conversation. Environment-level headers are inherited by
subagents: this setup identifies the conversation, and does not by itself
separate internal Claude subagents. Supply per-agent IDs and parent headers only
when your host can propagate them independently.

With the Python Claude Agent SDK, the same environment settings can be passed in
`ClaudeAgentOptions(env={...})`. Keep the identity stable for the intended SDK
conversation; a callback `session_id` is not automatically a model request header.

## History ordering

Messages are processed in original wire order. Consecutive user messages, such
as context plus a task, form one turn. Assistant messages, tool calls and tool
results separate follow-ups. Reconstructed history interleaves prompts with tool
calls instead of displaying all tools first. Existing captured response calls
remain deduplicated from reconstructed history. Historical timing is unknown;
no durations are inferred from message positions.

Responses that use only `previous_response_id` do not carry the full earlier
history. The proxy can combine calls it captured under the same thread ID, but
cannot recover unseen history from a provider using that response ID.

## Primary integration references

- [LangChain ChatOpenAI client options](https://github.com/langchain-ai/langchain/blob/master/libs/partners/openai/langchain_openai/chat_models/base.py)
- [LangGraph checkpoint thread identity](https://github.com/langchain-ai/langgraph/blob/main/libs/checkpoint/langgraph/checkpoint/base/__init__.py)
- [Agents SDK sessions](https://github.com/openai/openai-agents-python/blob/main/docs/sessions/index.md) and [Responses header merging](https://github.com/openai/openai-agents-python/blob/main/src/agents/models/openai_responses.py)
- [AutoGen OpenAI client configuration](https://github.com/microsoft/autogen/blob/main/python/packages/autogen-ext/src/autogen_ext/models/openai/_openai_client.py)
- [CrewAI native OpenAI client options](https://github.com/crewAIInc/crewAI/blob/main/lib/crewai/src/crewai/llms/providers/openai/completion.py)
- [Claude Code environment variables](https://code.claude.com/docs/en/env-vars) and [Claude Agent SDK options](https://github.com/anthropics/claude-agent-sdk-python/blob/main/src/claude_agent_sdk/types.py)
