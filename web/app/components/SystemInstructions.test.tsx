import assert from "node:assert/strict";
import test from "node:test";
import { renderToStaticMarkup } from "react-dom/server";
import type { RequestRecord, SessionDetail } from "../types";
import RequestDetailContent from "./RequestDetailContent";
import SessionView from "./SessionView";
import SystemInstructions from "./SystemInstructions";
import { getSystemInstructions } from "../utils/systemInstructions";

// MessageContent uses a DOM element to escape text. Supply only that API while
// rendering request details in Node, and restore the environment after each render.
function renderDetail(captured: RequestRecord) {
  const previous = Object.getOwnPropertyDescriptor(globalThis, "document");
  Object.defineProperty(globalThis, "document", {
    configurable: true,
    value: {
      createElement: () => ({
        textContent: "",
        get innerHTML() {
          return this.textContent
            .replace(/&/g, "&amp;")
            .replace(/</g, "&lt;")
            .replace(/>/g, "&gt;");
        },
      }),
    },
  });
  try {
    return renderToStaticMarkup(<RequestDetailContent request={captured} />);
  } finally {
    if (previous) Object.defineProperty(globalThis, "document", previous);
    else Reflect.deleteProperty(globalThis, "document");
  }
}

function request(body: RequestRecord["body"], index = 0): RequestRecord {
  return {
    id: `request-${index}`,
    requestId: `request-${index}`,
    timestamp: "2026-11-10T20:10:07+08:00",
    method: "POST",
    endpoint: "/v1/responses",
    headers: {},
    body,
  };
}

function session(requests: RequestRecord[]) {
  const detail: SessionDetail = {
    summary: {
      sessionId: "instructions-session",
      kind: "root",
      title: "Inspect instructions",
      status: "captured",
      requestCount: requests.length,
      toolCallCount: 0,
      inputTokens: 0,
      outputTokens: 0,
      responseTimeMs: 0,
      elapsedTimeMs: 0,
      firstTimestamp: requests[0].timestamp,
      lastTimestamp: requests.at(-1)!.timestamp,
    },
    requests,
  };
  return renderToStaticMarkup(
    <SessionView
      sessions={[detail.summary]}
      total={1}
      selectedSessionId={detail.summary.sessionId}
      detail={detail}
      isLoadingList={false}
      isLoadingDetail={false}
      onSelectSession={() => undefined}
      onOpenRequest={() => undefined}
    />
  );
}

const developerInput = (text: string) => ({
  role: "developer",
  content: [{ type: "input_text", text }],
});

test("Responses developer input is discoverable as system instructions in both views", () => {
  const captured = request({
    input: [
      developerInput("CORE_INSTRUCTIONS"),
      developerInput("WORKSPACE_INSTRUCTIONS"),
      { role: "user", content: "USER_TASK" },
    ],
  });
  for (const markup of [renderDetail(captured), session([captured])]) {
    assert.match(markup, /data-testid="system-instructions"/);
    assert.match(markup, /System Instructions/);
    assert.match(markup, /CORE_INSTRUCTIONS/);
    assert.match(markup, /WORKSPACE_INSTRUCTIONS/);
    assert.match(markup, /Developer/);
    assert.match(markup, /2 items/);
  }
});

test("Chat Completions system and developer messages appear in the instruction section", () => {
  const captured = request({
    messages: [
      { role: "system", content: "SYSTEM_POLICY" },
      {
        role: "developer",
        content: [{ type: "text", text: "DEVELOPER_POLICY" }],
      },
      { role: "user", content: "USER_TASK" },
    ],
  });
  for (const markup of [renderDetail(captured), session([captured])]) {
    assert.match(markup, /data-testid="system-instructions"/);
    assert.match(markup, /SYSTEM_POLICY/);
    assert.match(markup, /DEVELOPER_POLICY/);
    assert.match(markup, /2 items/);
  }
});

test("Anthropic string system prompts render without calling map on a string", () => {
  const captured = request({ system: "STRING_SYSTEM_POLICY" });
  for (const markup of [renderDetail(captured), session([captured])]) {
    assert.match(markup, /data-testid="system-instructions"/);
    assert.match(markup, /STRING_SYSTEM_POLICY/);
    assert.match(markup, /1 item/);
  }
});

test("Anthropic blocks keep cache hints and top-level Responses instructions remain visible", () => {
  const captured = request({
    system: [
      {
        type: "text",
        text: "CACHED_POLICY",
        cache_control: { type: "ephemeral" },
      },
    ],
    instructions: "RESPONSES_POLICY",
  });
  for (const markup of [renderDetail(captured), session([captured])]) {
    assert.match(markup, /CACHED_POLICY/);
    assert.match(markup, /Cache: ephemeral/);
    assert.match(markup, /RESPONSES_POLICY/);
    assert.match(markup, /2 items/);
  }
});

test("session shows instruction changes at their model step without repeating unchanged prompts", () => {
  const first = {
    input: [
      developerInput("FIRST_POLICY"),
      { role: "user", content: "USER_TASK" },
    ],
  };
  const updated = {
    input: [
      developerInput("UPDATED_POLICY"),
      { role: "user", content: "USER_TASK" },
    ],
  };
  const markup = session([
    request(first),
    request(first, 1),
    request(updated, 2),
  ]);
  assert.equal(markup.match(/data-testid="system-instructions"/g)?.length, 2);
  assert.equal(markup.match(/FIRST_POLICY/g)?.length, 1);
  assert.equal(markup.match(/UPDATED_POLICY/g)?.length, 1);
  assert.match(markup, /Step 3/);
  assert.doesNotMatch(markup, /data-user-prompt-turn="2"/);
});

test("empty instructions do not create an empty panel", () => {
  const captured = request({
    system: [],
    instructions: "",
    input: [{ role: "user", content: "USER_TASK" }],
  });
  for (const markup of [renderDetail(captured), session([captured])]) {
    assert.doesNotMatch(markup, /data-testid="system-instructions"/);
    assert.doesNotMatch(markup, /System Instructions/);
  }
});

test("session marks removed instructions instead of implying the previous policy still applies", () => {
  const markup = session([
    request({ instructions: "FIRST_POLICY" }),
    request({}, 1),
    request({ input: [developerInput("RESTORED_POLICY")] }, 2),
  ]);
  assert.equal(markup.match(/data-testid="system-instructions"/g)?.length, 3);
  assert.match(markup, /Changed · Step 2/);
  assert.match(
    markup,
    /No system or developer instructions in this captured request/
  );
  assert.match(markup, /Changed · Step 3/);
  assert.match(markup, /RESTORED_POLICY/);
});

test("instruction extraction preserves source roles, text blocks, and literal markup", () => {
  const text =
    '<script>alert("prompt")</script>\n<system-reminder>Keep this text</system-reminder>';
  const instructions = getSystemInstructions({
    instructions: "TOP_POLICY",
    input: [
      { role: "system", content: [{ type: "input_text", text }] },
      {
        role: "developer",
        content: [
          { type: "input_text", text: "BLOCK_ONE" },
          { type: "input_text", text: "BLOCK_TWO" },
        ],
      },
      { role: "user", content: "IGNORE_USER_TASK" },
    ],
  });
  assert.deepEqual(
    instructions.map(({ role, source, text }) => ({ role, source, text })),
    [
      { role: "system", source: "instructions", text: "TOP_POLICY" },
      { role: "system", source: "input[0]", text },
      { role: "developer", source: "input[1]", text: "BLOCK_ONE\n\nBLOCK_TWO" },
    ]
  );
  const markup = renderToStaticMarkup(
    <SystemInstructions instructions={instructions} />
  );
  assert.match(markup, /&lt;script&gt;/);
  assert.match(markup, /&lt;system-reminder&gt;Keep this text/);
  assert.doesNotMatch(markup, /<script>/);
  assert.doesNotMatch(markup, /IGNORE_USER_TASK/);
});
