import assert from "node:assert/strict";
import test from "node:test";
import { renderToStaticMarkup } from "react-dom/server";
import ToolCard from "./ToolCard";

const schema = {
  type: "object",
  properties: { analysis_id: { type: "string" } },
  required: ["analysis_id"],
};

test("renders flat Chat Completions, Responses and Anthropic tool schemas", () => {
  const tools = [
    {
      type: "function",
      function: {
        name: "analyze",
        description: "Analyze evidence",
        parameters: schema,
      },
    },
    {
      type: "function",
      name: "analyze",
      description: "Analyze evidence",
      parameters: schema,
    },
    { name: "analyze", description: "Analyze evidence", input_schema: schema },
  ];
  for (const tool of tools) {
    const markup = renderToStaticMarkup(<ToolCard tool={tool} index={0} />);
    assert.match(markup, /analyze/);
    assert.match(markup, /Analyze evidence/);
    assert.match(markup, /Input Schema/);
    assert.match(markup, /analysis_id/);
    assert.match(markup, /Copy schema/);
  }
});

test("renders Codex MCP namespace child names, descriptions and schemas", () => {
  const markup = renderToStaticMarkup(
    <ToolCard
      index={0}
      tool={{
        type: "namespace",
        name: "mcp__helix",
        description: "Helix tools",
        tools: [
          {
            type: "function",
            name: "submit_analysis",
            description: "Submit evidence",
            parameters: schema,
          },
          {
            type: "function",
            name: "read_analysis",
            description: "Read evidence",
            parameters: {
              type: "object",
              properties: { path: { type: "string" } },
            },
          },
        ],
      }}
    />
  );
  for (const value of [
    "mcp__helix",
    "submit_analysis",
    "Submit evidence",
    "analysis_id",
    "read_analysis",
    "Read evidence",
    "path",
  ])
    assert.ok(markup.includes(value), value);
  assert.equal(markup.match(/Input Schema/g)?.length, 2);
});

test("renders multi_agent_v1 definitions alongside flat tools", () => {
  const tools = [
    {
      type: "namespace",
      name: "multi_agent_v1",
      description: "Tools for spawning and managing sub-agents.",
      tools: [
        {
          type: "function",
          name: "spawn_agent",
          description: "Spawn a child",
          parameters: {
            type: "object",
            properties: { agent_type: { type: "string" } },
          },
        },
        {
          type: "function",
          name: "wait_agent",
          parameters: {
            type: "object",
            properties: { ids: { type: "array" } },
          },
        },
      ],
    },
    {
      type: "function",
      name: "get_goal",
      parameters: { type: "object", properties: {} },
    },
  ];
  const markup = renderToStaticMarkup(
    <>
      {tools.map((tool, index) => (
        <ToolCard key={index} tool={tool} index={index} />
      ))}
    </>
  );
  for (const value of [
    "spawn_agent",
    "agent_type",
    "wait_agent",
    "ids",
    "get_goal",
  ])
    assert.ok(markup.includes(value), value);
  assert.equal(markup.match(/Input Schema/g)?.length, 3);
});

test("qualifies duplicate child names with their namespace", () => {
  const markup = renderToStaticMarkup(
    <>
      {["mcp__one", "mcp__two"].map((name, index) => (
        <ToolCard
          key={name}
          index={index}
          tool={{
            type: "namespace",
            name,
            tools: [
              {
                type: "function",
                name: "search",
                parameters: { type: "object" },
              },
            ],
          }}
        />
      ))}
    </>
  );
  assert.match(markup, /data-tool-definition-name="mcp__one.search"/);
  assert.match(markup, /data-tool-definition-name="mcp__two.search"/);
});

test("handles empty or missing namespace definitions without inventing a schema", () => {
  for (const tool of [
    { type: "namespace", name: "empty", tools: [] },
    { type: "namespace", name: "missing" },
  ]) {
    const markup = renderToStaticMarkup(<ToolCard tool={tool} index={0} />);
    assert.match(markup, /No tool definitions captured/);
    assert.doesNotMatch(markup, /Input Schema/);
  }
});

test("renders long nested schemas completely", () => {
  const longSchema = {
    type: "object",
    description: "x".repeat(1500),
    properties: { LAST_SCHEMA_FIELD: { type: "string" } },
  };
  const markup = renderToStaticMarkup(
    <ToolCard
      index={0}
      tool={{
        type: "namespace",
        name: "mcp__large",
        tools: [{ type: "function", name: "submit", parameters: longSchema }],
      }}
    />
  );
  assert.match(markup, /LAST_SCHEMA_FIELD/);
});

test("preserves boolean JSON schemas", () => {
  const markup = renderToStaticMarkup(
    <ToolCard
      index={0}
      tool={{ type: "function", name: "deny", parameters: false }}
    />
  );
  assert.match(markup, /Input Schema/);
  assert.match(markup, />false</);
});
