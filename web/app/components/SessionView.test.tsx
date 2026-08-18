import assert from 'node:assert/strict';
import test from 'node:test';
import { renderToStaticMarkup } from 'react-dom/server';
import type { RequestRecord, SessionDetail, SessionSummary } from '../types';
import SessionView from './SessionView';

test('renders model metrics once per step instead of once per tool call', () => {
  const request: RequestRecord = {
    id: 'request-1',
    requestId: 'request-1',
    timestamp: '2026-08-14T16:32:24+08:00',
    method: 'POST',
    endpoint: '/v1/chat/completions',
    headers: {},
    routedModel: 'iFinD-Atlas',
    body: {
      model: 'iFinD-Atlas',
      messages: [{ role: 'user', content: 'Inspect memory files' }],
    },
    response: {
      statusCode: 200,
      headers: {},
      body: {
        choices: [{
          finish_reason: 'tool_calls',
          message: {
            tool_calls: [
              { id: 'call-read', function: { name: 'read', arguments: '{"filePath":"/tmp/memory.md"}' } },
              { id: 'call-glob', function: { name: 'glob', arguments: '{"path":"/tmp"}' } },
            ],
          },
        }],
        usage: { prompt_tokens: 400, completion_tokens: 281 },
      },
      responseTime: 7690,
      isStreaming: false,
      completedAt: '2026-08-14T16:32:31+08:00',
    },
  };
  const summary: SessionSummary = {
    sessionId: 'session-1',
    kind: 'root',
    title: 'Inspect memory files',
    model: 'iFinD-Atlas',
    status: 'awaiting-tool',
    requestCount: 1,
    toolCallCount: 2,
    inputTokens: 400,
    outputTokens: 281,
    responseTimeMs: 7690,
    elapsedTimeMs: 12000,
    firstTimestamp: request.timestamp,
    lastTimestamp: request.timestamp,
  };
  const detail: SessionDetail = { summary, requests: [request] };

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
    />,
  );

  assert.equal(markup.match(/281 output tokens/g)?.length, 1);
  assert.equal(markup.match(/7\.69s model latency/g)?.length, 1);
  assert.match(markup, /E2E 12\.0s/);
  assert.match(markup, />read</);
  assert.match(markup, />glob</);
  assert.doesNotMatch(markup, /~281 tokens/);
});
