import assert from 'node:assert/strict';
import test from 'node:test';
import { normalizeAssistantResponse } from './assistantResponse';

test('normalizes Chat Completions reasoning, content, and token details', () => {
  const result = normalizeAssistantResponse({
    choices: [{ message: { reasoning_content: 'chat reasoning', content: 'chat output' } }],
    usage: {
      completion_tokens: 30,
      completion_tokens_details: { reasoning_tokens: 18 },
    },
  }, '/v1/chat/completions');

  assert.deepEqual(result, {
    reasoning: 'chat reasoning',
    output: 'chat output',
    reasoningTokens: 18,
    contentTokens: 12,
  });
});

test('normalizes Responses reasoning summaries and output text', () => {
  const result = normalizeAssistantResponse({
    output: [
      { type: 'reasoning', summary: [{ type: 'summary_text', text: 'response summary' }] },
      { type: 'message', content: [{ type: 'output_text', text: 'response output' }] },
    ],
    usage: {
      output_tokens: 44,
      output_tokens_details: { reasoning_tokens: 31 },
    },
  }, '/v1/responses');

  assert.deepEqual(result, {
    reasoning: 'response summary',
    output: 'response output',
    reasoningTokens: 31,
    contentTokens: 13,
  });
});

test('normalizes Anthropic thinking and text without inventing a token split', () => {
  const result = normalizeAssistantResponse({
    content: [
      { type: 'thinking', thinking: 'anthropic thinking', signature: 'signature' },
      { type: 'text', text: 'anthropic output' },
    ],
    usage: { output_tokens: 25 },
  }, '/v1/messages');

  assert.deepEqual(result, {
    reasoning: 'anthropic thinking',
    output: 'anthropic output',
    reasoningTokens: undefined,
    contentTokens: undefined,
  });
});

test('uses Anthropic output tokens for a text-only response', () => {
  const result = normalizeAssistantResponse({
    content: [{ type: 'text', text: 'anthropic output' }],
    usage: { output_tokens: 9 },
  }, '/v1/messages');

  assert.equal(result.contentTokens, 9);
});
