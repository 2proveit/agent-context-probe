import assert from "node:assert/strict";
import test from "node:test";
import { renderToStaticMarkup } from "react-dom/server";
import type { RequestRecord, SessionDetail, SessionSummary } from "../types";
import SessionView from "./SessionView";

test("renders model metrics once per step instead of once per tool call", () => {
  const request: RequestRecord = {
    id: "request-1",
    requestId: "request-1",
    timestamp: "2026-08-14T16:32:24+08:00",
    method: "POST",
    endpoint: "/v1/chat/completions",
    headers: {},
    routedModel: "iFinD-Atlas",
    body: {
      model: "iFinD-Atlas",
      messages: [{ role: "user", content: "Inspect memory files" }],
    },
    response: {
      statusCode: 200,
      headers: {},
      body: {
        choices: [
          {
            finish_reason: "tool_calls",
            message: {
              tool_calls: [
                {
                  id: "call-read",
                  function: {
                    name: "read",
                    arguments: '{"filePath":"/tmp/memory.md"}',
                  },
                },
                {
                  id: "call-glob",
                  function: { name: "glob", arguments: '{"path":"/tmp"}' },
                },
              ],
            },
          },
        ],
        usage: {
          prompt_tokens: 400,
          completion_tokens: 281,
          prompt_tokens_details: { cached_tokens: 200 },
        },
      },
      responseTime: 7690,
      isStreaming: false,
      completedAt: "2026-08-14T16:32:31+08:00",
    },
  };
  const summary: SessionSummary = {
    sessionId: "session-1",
    kind: "root",
    title: "Inspect memory files",
    model: "iFinD-Atlas",
    status: "awaiting-tool",
    requestCount: 1,
    toolCallCount: 2,
    inputTokens: 400,
    outputTokens: 281,
    responseTimeMs: 7690,
    elapsedTimeMs: 12000,
    firstTimestamp: request.timestamp,
    lastTimestamp: request.timestamp,
  };
  const detail: SessionDetail = {
    summary,
    requests: [request],
    toolWindows: [
      {
        requestId: request.requestId,
        callIds: ["call-read", "call-glob"],
        toolNames: ["read", "glob"],
        startTimestamp: "2026-08-14T16:32:31.000+08:00",
        endTimestamp: "2026-08-14T16:32:34.400+08:00",
        durationMs: 3400,
        approximate: false,
        complete: true,
      },
    ],
  };

  const markup = renderToStaticMarkup(
    <SessionView
      sessions={[summary]}
      total={1}
      selectedSessionId={summary.sessionId}
      detail={detail}
      isLoadingList={false}
      isLoadingDetail={false}
      onSelectSession={() => undefined}
      onOpenRequest={() => undefined}
    />
  );

  assert.equal(markup.match(/281 output tokens/g)?.length, 1);
  assert.equal(markup.match(/7\.69s model latency/g)?.length, 1);
  assert.match(markup, /E2E 12\.0s/);
  assert.match(markup, />read</);
  assert.match(markup, />glob</);
  assert.match(markup, /Tool batch · read, glob/);
  assert.match(markup, /3\.40s observed window/);
  assert.equal(markup.match(/data-waterfall-kind="tool"/g)?.length, 1);
  assert.equal(markup.match(/data-waterfall-kind="model"/g)?.length, 1);
  assert.equal(markup.match(/data-waterfall-tooltip="tool"/g)?.length, 1);
  assert.match(markup, /Observed tool window/);
  assert.match(markup, /Duration/);
  assert.match(markup, /data-testid="session-context-metrics"/);
  assert.match(markup, /input_tokens:/);
  assert.match(markup, /input_tokens_cached:/);
  assert.match(markup, /output_tokens:/);
  assert.match(markup, /prefix_cache_hit_rate:/);
  assert.match(markup, /50\.00%/);
  assert.doesNotMatch(markup, /~281 tokens/);
});

test("labels the last text-only model step as Final Output", () => {
  const request: RequestRecord = {
    id: "request-final",
    requestId: "request-final",
    timestamp: "2026-08-14T16:32:40.100+08:00",
    method: "POST",
    endpoint: "/v1/responses",
    headers: {},
    routedModel: "iFinD-Atlas",
    body: { model: "iFinD-Atlas", input: "finish" },
    response: {
      statusCode: 200,
      headers: {},
      body: {
        output: [
          {
            type: "message",
            content: [{ type: "output_text", text: "Finished" }],
          },
        ],
        usage: { input_tokens: 10, output_tokens: 4 },
      },
      responseTime: 1200,
      isStreaming: false,
      completedAt: "2026-08-14T16:32:41.300+08:00",
    },
  };
  const summary: SessionSummary = {
    sessionId: "session-final",
    kind: "root",
    title: "finish",
    model: "iFinD-Atlas",
    status: "completed",
    requestCount: 1,
    toolCallCount: 0,
    inputTokens: 10,
    outputTokens: 4,
    responseTimeMs: 1200,
    elapsedTimeMs: 1200,
    firstTimestamp: request.timestamp,
    lastTimestamp: request.timestamp,
  };

  const markup = renderToStaticMarkup(
    <SessionView
      sessions={[summary]}
      total={1}
      selectedSessionId={summary.sessionId}
      detail={{ summary, requests: [request] }}
      isLoadingList={false}
      isLoadingDetail={false}
      onSelectSession={() => undefined}
      onOpenRequest={() => undefined}
    />
  );

  assert.match(markup, /Step 1 · Final Output · Model: iFinD-Atlas/);
});
