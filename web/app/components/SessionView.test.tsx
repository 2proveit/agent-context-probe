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

test("preserves completed turn output and inserts a later user prompt before an interrupted turn", () => {
  const firstRequest: RequestRecord = {
    id: "request-turn-1",
    requestId: "request-turn-1",
    timestamp: "2026-08-18T17:20:18.100+08:00",
    method: "POST",
    endpoint: "/v1/chat/completions",
    headers: {},
    routedModel: "iFinD-Atlas",
    body: {
      model: "iFinD-Atlas",
      messages: [{ role: "user", content: "Write 3,200 Chinese characters" }],
    },
    response: {
      statusCode: 200,
      headers: {},
      body: {
        choices: [
          {
            finish_reason: "stop",
            message: { content: "The 3,200-character article is complete." },
          },
        ],
        usage: { prompt_tokens: 10, completion_tokens: 8 },
      },
      responseTime: 1200,
      isStreaming: false,
      completedAt: "2026-08-18T17:20:19.300+08:00",
    },
  };
  const followUpMessages = [
    { role: "user", content: "Write 3,200 Chinese characters" },
    {
      role: "assistant",
      content: "The 3,200-character article is complete.",
    },
    {
      role: "user",
      content:
        "<lumi_workspace>workspace metadata</lumi_workspace>\n\nCorrection: write 64,000 Chinese characters in one pass",
    },
  ];
  const toolRequest: RequestRecord = {
    ...firstRequest,
    id: "request-turn-2-tool",
    requestId: "request-turn-2-tool",
    timestamp: "2026-08-18T17:24:42.100+08:00",
    body: { model: "iFinD-Atlas", messages: followUpMessages },
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
                  id: "call-todo",
                  function: { name: "todowrite", arguments: "{}" },
                },
              ],
            },
          },
        ],
        usage: { prompt_tokens: 20, completion_tokens: 5 },
      },
      responseTime: 1000,
      isStreaming: false,
      completedAt: "2026-08-18T17:24:43.100+08:00",
    },
  };
  const interruptedRequest: RequestRecord = {
    ...firstRequest,
    id: "request-turn-2-interrupted",
    requestId: "request-turn-2-interrupted",
    timestamp: "2026-08-18T17:26:26.100+08:00",
    body: {
      model: "iFinD-Atlas",
      messages: [
        ...followUpMessages,
        {
          role: "assistant",
          content: "",
          tool_calls: [
            {
              id: "call-todo",
              function: { name: "todowrite", arguments: "{}" },
            },
          ],
        },
        { role: "tool", tool_call_id: "call-todo", content: "done" },
      ],
    },
    response: {
      statusCode: 200,
      headers: {},
      responseTime: 300000,
      isStreaming: true,
      completedAt: "2026-08-18T17:31:26.100+08:00",
      streamError: "upstream response stream was interrupted",
    },
  };
  const requests = [firstRequest, toolRequest, interruptedRequest];
  const summary: SessionSummary = {
    sessionId: "session-multi-turn",
    kind: "root",
    title: "Write 3,200 Chinese characters",
    model: "iFinD-Atlas",
    status: "interrupted",
    requestCount: requests.length,
    toolCallCount: 1,
    inputTokens: 30,
    outputTokens: 13,
    responseTimeMs: 302200,
    elapsedTimeMs: 668000,
    firstTimestamp: firstRequest.timestamp,
    lastTimestamp: interruptedRequest.timestamp,
  };

  const markup = renderToStaticMarkup(
    <SessionView
      sessions={[summary]}
      total={1}
      selectedSessionId={summary.sessionId}
      detail={{ summary, requests }}
      isLoadingList={false}
      isLoadingDetail={false}
      onSelectSession={() => undefined}
      onOpenRequest={() => undefined}
    />
  );

  assert.match(markup, /Step 1 · Turn 1 Output · Model: iFinD-Atlas/);
  assert.equal(
    markup.match(/Correction: write 64,000 Chinese characters in one pass/g)
      ?.length,
    1
  );
  assert.match(markup, /User Prompt · Turn 2/);
  assert.match(markup, /Response interrupted/);
  assert.match(markup, /Active E2E 6m 45s/);
  assert.match(markup, /User wait 4m 23s/);
  assert.match(markup, /Span 11m 8s/);
  assert.match(markup, /Turn 1 · 1\.20s/);
  assert.match(markup, /Turn 2 · 6m 44s/);
  assert.match(markup, /data-user-wait-ms="262800"/);
  assert.match(markup, /data-turn-toggle="1"[^>]*aria-expanded="true"/);
  assert.match(markup, /data-turn-toggle="2"[^>]*aria-expanded="true"/);
  assert.match(markup, /data-turn-content="1"/);
  assert.match(markup, /data-turn-content="2"/);
  assert.match(
    markup,
    /data-turn-duration-comparison="1"[^>]*data-turn-duration-percent="0\.3"/
  );
  assert.match(
    markup,
    /data-turn-duration-comparison="2"[^>]*data-turn-duration-percent="100"/
  );
  assert.match(markup, /Longest turn/);
  assert.ok(
    markup.indexOf("Step 1 · Turn 1 Output") <
      markup.indexOf("User Prompt · Turn 2")
  );
  assert.ok(
    markup.indexOf("User Prompt · Turn 2") <
      markup.indexOf("Response interrupted")
  );
});

test("renders later prompts across Anthropic Messages and OpenAI Responses without treating tool results as turns", () => {
  const cases: Array<{
    name: string;
    followUp: string;
    requests: RequestRecord[];
  }> = [
    {
      name: "anthropic",
      followUp: "Anthropic follow-up",
      requests: [
        {
          id: "anthropic-turn-1",
          requestId: "anthropic-turn-1",
          timestamp: "2026-08-18T18:00:00.000+08:00",
          method: "POST",
          endpoint: "/v1/messages",
          headers: {},
          body: {
            model: "claude-test",
            messages: [
              {
                role: "user",
                content: [{ type: "text", text: "Initial Anthropic prompt" }],
              },
            ],
          },
          response: {
            statusCode: 200,
            headers: {},
            body: {
              content: [{ type: "text", text: "Anthropic turn one output" }],
              stop_reason: "end_turn",
              usage: { input_tokens: 10, output_tokens: 5 },
            },
            responseTime: 1000,
            isStreaming: false,
            completedAt: "2026-08-18T18:00:01.000+08:00",
          },
        },
        {
          id: "anthropic-turn-2",
          requestId: "anthropic-turn-2",
          timestamp: "2026-08-18T18:01:00.000+08:00",
          method: "POST",
          endpoint: "/v1/messages",
          headers: {},
          body: {
            model: "claude-test",
            messages: [
              {
                role: "user",
                content: [{ type: "text", text: "Initial Anthropic prompt" }],
              },
              {
                role: "assistant",
                content: [{ type: "text", text: "Anthropic turn one output" }],
              },
              {
                role: "user",
                content: [
                  {
                    type: "tool_result",
                    tool_use_id: "toolu-read",
                    content: "Anthropic tool payload",
                  },
                ],
              },
              {
                role: "user",
                content: [{ type: "text", text: "Anthropic follow-up" }],
              },
            ],
          },
          response: {
            statusCode: 200,
            headers: {},
            responseTime: 300000,
            isStreaming: true,
            completedAt: "2026-08-18T18:06:00.000+08:00",
            streamError: "upstream response stream was interrupted",
          },
        },
      ],
    },
    {
      name: "responses",
      followUp: "Responses follow-up",
      requests: [
        {
          id: "responses-turn-1",
          requestId: "responses-turn-1",
          timestamp: "2026-08-18T19:00:00.000+08:00",
          method: "POST",
          endpoint: "/v1/responses",
          headers: {},
          body: { model: "responses-test", input: "Initial Responses prompt" },
          response: {
            statusCode: 200,
            headers: {},
            body: {
              status: "completed",
              output: [
                {
                  type: "message",
                  content: [
                    { type: "output_text", text: "Responses turn one output" },
                  ],
                },
              ],
              usage: { input_tokens: 10, output_tokens: 5 },
            },
            responseTime: 1000,
            isStreaming: false,
            completedAt: "2026-08-18T19:00:01.000+08:00",
          },
        },
        {
          id: "responses-turn-2",
          requestId: "responses-turn-2",
          timestamp: "2026-08-18T19:01:00.000+08:00",
          method: "POST",
          endpoint: "/v1/responses",
          headers: {},
          body: {
            model: "responses-test",
            input: [
              {
                role: "user",
                content: [
                  { type: "input_text", text: "Initial Responses prompt" },
                ],
              },
              {
                type: "function_call_output",
                call_id: "call-read",
                output: "Responses tool payload",
              },
              {
                role: "user",
                content: [{ type: "input_text", text: "Responses follow-up" }],
              },
            ],
          },
          response: {
            statusCode: 200,
            headers: {},
            responseTime: 300000,
            isStreaming: true,
            completedAt: "2026-08-18T19:06:00.000+08:00",
            streamError: "upstream response stream was interrupted",
          },
        },
      ],
    },
  ];

  for (const testCase of cases) {
    const [firstRequest, lastRequest] = testCase.requests;
    const summary: SessionSummary = {
      sessionId: `session-${testCase.name}`,
      kind: "root",
      title: `${testCase.name} multi-turn session`,
      model: firstRequest.body?.model,
      status: "interrupted",
      requestCount: testCase.requests.length,
      toolCallCount: 0,
      inputTokens: 10,
      outputTokens: 5,
      responseTimeMs: 301000,
      elapsedTimeMs: 360000,
      firstTimestamp: firstRequest.timestamp,
      lastTimestamp: lastRequest.timestamp,
    };
    const markup = renderToStaticMarkup(
      <SessionView
        sessions={[summary]}
        total={1}
        selectedSessionId={summary.sessionId}
        detail={{ summary, requests: testCase.requests }}
        isLoadingList={false}
        isLoadingDetail={false}
        onSelectSession={() => undefined}
        onOpenRequest={() => undefined}
      />
    );

    assert.equal(
      markup.match(/data-user-prompt-turn="2"/g)?.length,
      1,
      testCase.name
    );
    assert.doesNotMatch(markup, /data-user-prompt-turn="3"/, testCase.name);
    assert.equal(
      markup.match(new RegExp(testCase.followUp, "g"))?.length,
      1,
      testCase.name
    );
    assert.match(markup, /Step 1 · Turn 1 Output/, testCase.name);
    assert.match(markup, /Response interrupted/, testCase.name);
  }
});
