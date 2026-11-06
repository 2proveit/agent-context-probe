# Downstream response fault injection

[Back to README configuration](../README.md#configuration).

Enterprise deployments may use self-hosted models with incomplete adaptations
in open-source inference frameworks, gateways, or middleware, producing malformed
responses or lower reliability than the model provider's official endpoint.
Response corruption simulates these failures to verify that our harness detects
invalid model output and handles it with appropriate recovery or fallback behavior.

`response_corruption` simulates unstable model responses **received by the
harness**. Requests still go to the configured provider unchanged. After routing,
provider conversion, and model-alias restoration, the proxy can mutate selected output fields of an
HTTP 200 response in the client's own protocol.

```text
Harness -- original request --> Proxy --> Model provider
Harness <-- faulty response -- Proxy <-- provider response
```

## Quick start

The feature defaults to disabled. Copy the section from [config.yaml.example](../config.yaml.example)
and restart the proxy. To exercise one failure deterministically:

```yaml
response_corruption:
  enabled: true
  every_n_requests: 1
  seed: 42
  modes: [empty_stop]
```

## Fault modes

| Mode                       | What the downstream harness receives                                                                                                                                                           |
| -------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `empty_stop`               | Removes visible text and tool calls while retaining any remote reasoning. Chat ends with `stop`, Messages with `end_turn`; Responses retains its remote lifecycle and reasoning.               |
| `repetition`               | A complete text response with `果树。` repeated 1,400 times and a replacement character.                                                                                                       |
| `pseudo_tags`              | A complete text response containing fake tool-return and system-warning tags.                                                                                                                  |
| `invalid_tool_arguments`   | Remote tool calls with deliberately invalid argument JSON, retaining their original names and IDs. For non-streaming Messages, `tool_use.input` is deliberately a string instead of an object. |
| `xml_tool_name`            | A tool call whose name contains XML-style function/parameter text instead of a registered tool name; arguments remain valid `{}`.                                                              |
| `reasoning_repetition`     | Repeated text in existing reasoning fields, streamed incrementally; leaves visible output and actual tool calls unchanged.                                                                     |
| `truncated_tool_arguments` | Actual argument JSON prefixes without closing delimiters, with a complete length-limited stream and real usage.                                                                                |
| `truncate_stream`          | Only a prefix of the first SSE application chunk, followed by EOF without the protocol's terminal event. This is incomplete SSE inside a normally ended HTTP response, not a TCP reset.        |

`reasoning_repetition` changes only existing nonempty reasoning fields (Chat
`reasoning_content`, Messages `thinking`, and Responses reasoning text/summary).
It leaves visible content, actual tool calls, finish states and usage intact.
Each existing nonempty SSE reasoning delta becomes eight copies of a fixed
repeat unit and is flushed immediately; it does not wait for stream completion
or invent reasoning when the provider returns none. JSON reasoning fields use
64 copies. Responses delta, done and final item snapshots remain consistent.
This exercises detection during generation and downstream cancellation, unlike
`repetition`, which targets visible text. Captured streams missing terminal
frames after a guard retry do not by themselves prove an upstream disconnect.

`truncated_tool_arguments` retains actual tool names, IDs and argument prefixes,
but stops before the outer JSON object/array closes. Prefix scanning carries
quote/escape/nesting state across SSE deltas, so nested JSON and UTF-8 strings
remain intact. The SSE itself still ends normally: Chat uses `length`, Messages
uses `max_tokens`, and Responses uses `response.incomplete` / `incomplete` with
`max_output_tokens`. Actual usage and terminal events are retained; missing
usage stays missing. No tool call or successful terminal event is invented when
absent upstream. Messages JSON cannot carry partial object bytes, so its
`tool_use.input` is deliberately a string containing the incomplete JSON prefix.
This models the row-163 case (unfinished arguments with a complete length-limited
stream), separately from `truncate_stream`, which cuts the SSE bytes themselves.

`xml_tool_name` reproduces a different tool-protocol failure observed in
`req_c3187a9d53b84415b06b29e885172a22`: the tool name becomes
`<function=REMOTE_TOOL>\n<parameter=name>\nsynthetic-fault\n</parameter>\n</function>`,
while arguments stay valid empty JSON `{}`. It emits a normal tool-call finish
and complete stream, so a harness must validate the registered tool name even
when argument decoding succeeds. It is available for all three protocols, both
JSON and SSE. No tool is executed by the proxy.

For Chat Completions SSE, `xml_tool_name` collects each remote tool name
(including names split across frames), emits the XML name with empty arguments,
and emits `{}` in an arguments continuation without repeating the call ID/name.
All actual tools and their indices are retained. If the remote supplied a single
tool frame, an arguments continuation is inserted using the same response ID and
tool index, without inventing an SSE event ID. Reasoning before the first tool
frame streams immediately; tool frames are buffered until a finish event or EOF
so fragmented names can be assembled. Each emitted SSE frame is written and
flushed separately; HTTP/TCP may still coalesce writes. Provider delays and
historical badcase reasoning are not replayed.

## Metadata and usage

Response IDs, tool-call IDs, Responses item/call references, model names,
timestamps, usage, and unrelated extension fields come from the actual remote
response. Unknown fields and large numeric values are preserved. Target output
fields and the finish reason required by the selected text fault are modified;
removing tool output in a text/empty fault necessarily removes its call fields.
Tool faults modify actual returned calls, rather than inventing a call from the
request's tool declarations. If the provider returns no tool call, a selected
tool fault leaves the response unchanged.

Usage is retained exactly when supplied, including detail fields, null values,
and measured zeros. Missing usage remains missing. These are **provider token
counts**, not a recount of the corrupted text received by the harness. Incomplete
or unrecognized JSON/SSE is passed through without fabricating successful
terminal events, IDs, or usage.

## Response eligibility and selection

The first three modes are based on the observed shapes of repetition,
pseudo-tag contamination, and reasoning-only empty-stop badcases. They simulate
those failures without relying on the provider to reproduce them. Mode selection
happens after the provider has returned HTTP 200. Upstream 4xx/5xx responses and
connection failures remain unchanged; health, Dashboard, and model-list endpoints
are excluded.

Omit `modes` to select randomly among all eight. `truncate_stream` is excluded for
non-SSE responses. Tool-call fault modes (`invalid_tool_arguments`,
`xml_tool_name`, `truncated_tool_arguments`) are excluded when no tool is specified. Responses with no
applicable mode pass through without advancing the counter.

`every_n_requests: N` injects on eligible response N, 2N, 3N, and so on.
For example, N=3 yields healthy, healthy, faulty, healthy, healthy, faulty.
N=1 injects on every eligible response. Only successful HTTP 200 generation
responses with at least one applicable mode count. Health checks, Dashboard,
model listings, upstream errors and connection failures do not count. The
counter is shared across all three generation endpoints, models and sessions
within one proxy instance, advances at final response-header selection, and
resets on restart. Concurrent responses count in selection order, not incoming
request order; use an isolated proxy for a specific harness experiment.

The feature still defaults to disabled. The interval must be a positive integer
when enabled; zero is accepted only while disabled, and negative values are
rejected. The former `probability` setting has been replaced: migrate an enabled
configuration by setting `every_n_requests` explicitly. An old enabled config
without an interval fails startup instead of silently enabling random injection.

Mode selection remains seeded and random; duplicate names weight the choice.
The same seed and eligible response order repeat the selected modes, while the
interval guarantees trigger positions independently of the seed. Select a single
mode to reproduce both the fault type and trigger position deterministically.

`truncate_after_bytes` defaults to 256 (also when set to zero). The truncation
point is the smaller of that limit and half of the first write's bytes, so chunk
boundaries can affect the actual prefix. Invalid intervals, unknown modes,
and negative limits fail startup even when injection is disabled.

## Delivery and capture

The injector is the final response layer so internal protocol conversion cannot
repair the fault before it reaches the harness. JSON is mutated after a complete
payload arrives; SSE is parsed across arbitrary write boundaries, retaining event
metadata and references. Except for `truncate_stream`, injection lets the provider
finish normally so final usage and terminal events remain available.
`truncate_stream` forwards a real byte prefix and cancels further provider work.
Credentials, provider URLs, and outgoing request bodies are untouched.

The client receives `X-Agent-Context-Probe-Corruption` (selected mode) and
`X-Agent-Context-Probe-Corruption-Id` (local selection ID). These headers do not
replace IDs in the response body. Selection does not guarantee a mutation if the
remote lacks the target field, for example a tool fault on a text-only response.
Captured headers label applied faults with
`X-Agent-Context-Probe-Corruption-Capture: upstream-before-injection`. SQLite
stores provider-side bytes before mutation; truncation can leave that capture
incomplete with a stream error. It does not record the mutated downstream bytes.
Capture the harness-side raw response and compare it with the same request's
provider capture when assessing metadata preservation or harness recovery.

## Validation

Local checks exercise three protocols, both JSON and SSE, byte-for-byte unchanged
upstream requests, downstream fault contents, real metadata and absent usage, truncation cancellation,
seed replay, concurrency, and an illustrative bounded retry after an empty stop:

```bash
cd proxy
go test -race ./internal/app ./internal/config ./internal/middleware ./internal/handler -run 'TestResponseCorruption|TestInjectedStream|TestDownstreamConsumer'
```

These tests establish the proxy's fault-delivery behavior. The retry consumer is
a small test client, not Lumi or OpenCode acceptance. To assess a real harness,
point its model endpoint at this proxy, start with one mode and
`every_n_requests: 1`, and inspect the harness's validation, retries, tool dispatch, termination, and
final delivery. Test mixed healthy/faulty responses afterward with a larger
interval, for example `every_n_requests: 3`. Restore `enabled: false` and restart
when the experiment is finished.

## Legacy request corruption

The earlier experimental `request_corruption` implementation mutated upstream
requests and did not test this failure direction. It has been replaced; use the
`response_corruption` section above. Earlier request-pollution replay artifacts
are historical evidence of that superseded experiment, not harness-stability
results.
