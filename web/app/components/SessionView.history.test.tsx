import assert from "node:assert/strict";
import test from "node:test";
import { renderToStaticMarkup } from "react-dom/server";
import type { RequestRecord, SessionDetail } from "../types";
import SessionView from "./SessionView";

function renderHistory(
  body: RequestRecord["body"],
  endpoint = "/v1/responses"
) {
  const request: RequestRecord = {
    id: "history",
    requestId: "history",
    timestamp: "2026-11-09T13:20:44Z",
    method: "POST",
    endpoint,
    headers: {},
    body,
  };
  const detail: SessionDetail = {
    summary: {
      sessionId: "thread",
      kind: "root",
      title: "trace",
      status: "captured",
      requestCount: 1,
      toolCallCount: 2,
      inputTokens: 0,
      outputTokens: 0,
      responseTimeMs: 0,
      elapsedTimeMs: 0,
      firstTimestamp: request.timestamp,
      lastTimestamp: request.timestamp,
    },
    requests: [request],
  };
  return renderToStaticMarkup(
    <SessionView
      sessions={[detail.summary]}
      total={1}
      selectedSessionId="thread"
      detail={detail}
      isLoadingList={false}
      isLoadingDetail={false}
      onSelectSession={() => undefined}
      onOpenRequest={() => undefined}
    />
  );
}
function ordered(markup: string, ...markers: string[]) {
  let previous = -1;
  for (const marker of markers) {
    const position = markup.indexOf(marker);
    assert.ok(
      position > previous,
      `Expected ${marker} after previous marker (${previous}), got ${position}`
    );
    previous = position;
  }
}
const call = (id: string) => ({
  type: "function_call",
  call_id: id,
  name: id,
  arguments: "{}",
});
const user = (text: string) => ({
  role: "user",
  content: [{ type: "input_text", text }],
});

test("Codex context and task stay before submit_analysis in partial history", () => {
  const markup = renderHistory({
    input: [
      user("AGENTS_CONTEXT"),
      user("ACTUAL_TASK"),
      call("submit_analysis"),
    ],
  });
  ordered(
    markup,
    "AGENTS_CONTEXT",
    "ACTUAL_TASK",
    'data-tool-call-id="submit_analysis"'
  );
  assert.doesNotMatch(markup, /data-user-prompt-turn="2"/);
});

test("Responses history keeps true follow-ups between tool calls", () => {
  const markup = renderHistory({
    input: [
      user("INITIAL_TASK"),
      call("first_call"),
      { type: "function_call_output", call_id: "first_call", output: "done" },
      user("FOLLOW_UP"),
      call("submit_analysis"),
    ],
  });
  ordered(
    markup,
    "INITIAL_TASK",
    'data-tool-call-id="first_call"',
    "FOLLOW_UP",
    'data-tool-call-id="submit_analysis"'
  );
  assert.equal(markup.match(/data-user-prompt-turn="2"/g)?.length, 1);
});

test("OpenCode chat history keeps follow-ups between tool calls", () => {
  const toolMessage = (id: string) => ({
    role: "assistant",
    content: null,
    tool_calls: [{ id, function: { name: id, arguments: "{}" } }],
  });
  const markup = renderHistory(
    {
      messages: [
        { role: "user", content: "INITIAL_TASK" },
        toolMessage("first_call"),
        { role: "tool", tool_call_id: "first_call", content: "done" },
        { role: "user", content: "FOLLOW_UP" },
        toolMessage("submit_analysis"),
      ],
    },
    "/v1/chat/completions"
  );
  ordered(
    markup,
    "INITIAL_TASK",
    'data-tool-call-id="first_call"',
    "FOLLOW_UP",
    'data-tool-call-id="submit_analysis"'
  );
});

test("Anthropic tool results do not become prompts or move follow-ups", () => {
  const markup = renderHistory(
    {
      messages: [
        { role: "user", content: "INITIAL_TASK" },
        {
          role: "assistant",
          content: [
            {
              type: "tool_use",
              id: "first_call",
              name: "first_call",
              input: {},
            },
          ],
        },
        {
          role: "user",
          content: [
            {
              type: "tool_result",
              tool_use_id: "first_call",
              content: "RESULT_TEXT",
            },
          ],
        },
        { role: "user", content: "FOLLOW_UP" },
        {
          role: "assistant",
          content: [
            {
              type: "tool_use",
              id: "submit_analysis",
              name: "submit_analysis",
              input: {},
            },
          ],
        },
      ],
    },
    "/v1/messages"
  );
  ordered(
    markup,
    "INITIAL_TASK",
    'data-tool-call-id="first_call"',
    "FOLLOW_UP",
    'data-tool-call-id="submit_analysis"'
  );
  assert.equal(markup.match(/data-user-prompt-turn="2"/g)?.length, 1);
});

test("a historical tool before the first visible prompt stays before it", () => {
  const markup = renderHistory({
    input: [call("first_call"), user("VISIBLE_TASK"), call("submit_analysis")],
  });
  ordered(
    markup,
    'data-tool-call-id="first_call"',
    "VISIBLE_TASK",
    'data-tool-call-id="submit_analysis"'
  );
});

test("repeated identical prompts separated by a tool remain distinct turns", () => {
  const markup = renderHistory({
    input: [
      user("REPEAT_TASK"),
      call("first_call"),
      user("REPEAT_TASK"),
      call("submit_analysis"),
    ],
  });
  const firstPrompt = markup.indexOf("REPEAT_TASK");
  const secondPrompt = markup.indexOf("REPEAT_TASK", firstPrompt + 1);
  assert.ok(firstPrompt < markup.indexOf('data-tool-call-id="first_call"'));
  assert.ok(secondPrompt > markup.indexOf('data-tool-call-id="first_call"'));
  assert.ok(
    secondPrompt < markup.indexOf('data-tool-call-id="submit_analysis"')
  );
  assert.equal(markup.match(/data-user-prompt-turn="2"/g)?.length, 1);
});

test("assistant text alone separates genuine follow-ups", () => {
  const markup = renderHistory({
    input: [
      user("INITIAL_TASK"),
      {
        role: "assistant",
        content: [{ type: "output_text", text: "previous answer" }],
      },
      user("FOLLOW_UP"),
      call("submit_analysis"),
    ],
  });
  ordered(
    markup,
    "INITIAL_TASK",
    "FOLLOW_UP",
    'data-tool-call-id="submit_analysis"'
  );
  assert.equal(markup.match(/data-user-prompt-turn="2"/g)?.length, 1);
});
