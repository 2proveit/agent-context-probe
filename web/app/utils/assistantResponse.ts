/* eslint-disable @typescript-eslint/no-explicit-any -- Provider response bodies are protocol-dependent JSON. */

import { getAPIProtocol, getUsage } from './models';

export interface AssistantResponseSegments {
  reasoning: string;
  output: string;
  reasoningTokens?: number;
  contentTokens?: number;
}

function joinText(items: any[], types?: string[]) {
  return items
    .filter(item => !types || types.includes(item?.type))
    .map(item => item?.text ?? item?.thinking ?? item?.summary_text ?? '')
    .filter(Boolean)
    .join('\n');
}

function chatContentText(content: unknown) {
  if (typeof content === 'string') return content;
  return Array.isArray(content) ? joinText(content, ['text', 'output_text']) : '';
}

export function normalizeAssistantResponse(
  body: any,
  endpoint?: string,
): AssistantResponseSegments {
  const protocol = getAPIProtocol(endpoint);
  const usage = getUsage(body);
  let reasoning = '';
  let output = '';
  let hasToolCalls = false;
  let tokenSplitAvailable = true;

  if (protocol === 'OpenAI Chat Completions') {
    const message = body?.choices?.[0]?.message ?? {};
    reasoning = typeof message.reasoning_content === 'string'
      ? message.reasoning_content
      : joinText(Array.isArray(message.reasoning_content) ? message.reasoning_content : []);
    output = chatContentText(message.content);
    if (!output && typeof message.refusal === 'string') output = message.refusal;
    hasToolCalls = Array.isArray(message.tool_calls) && message.tool_calls.length > 0;
  } else if (protocol === 'OpenAI Responses') {
    const outputs = Array.isArray(body?.output) ? body.output : [];
    const reasoningParts: string[] = [];
    const outputParts: string[] = [];

    for (const item of outputs) {
      if (item?.type === 'reasoning') {
        reasoningParts.push(joinText([
          ...(Array.isArray(item.summary) ? item.summary : []),
          ...(Array.isArray(item.content) ? item.content : []),
        ]));
      } else if (item?.type === 'message') {
        const content = Array.isArray(item.content) ? item.content : [];
        reasoningParts.push(joinText(content, ['reasoning_text', 'summary_text']));
        outputParts.push(joinText(content, ['output_text', 'text']));
      } else if (item?.type === 'function_call') {
        hasToolCalls = true;
      }
    }
    reasoning = reasoningParts.filter(Boolean).join('\n');
    output = outputParts.filter(Boolean).join('\n');
  } else {
    const content = Array.isArray(body?.content) ? body.content : [];
    reasoning = joinText(content, ['thinking']);
    output = joinText(content, ['text']);
    hasToolCalls = content.some((item: any) => item?.type === 'tool_use');
    tokenSplitAvailable = !reasoning;
  }

  const reasoningTokens = usage?.reasoning ? usage.reasoning : undefined;
  const contentTokens = usage && output && !hasToolCalls && tokenSplitAvailable
    ? Math.max(usage.output - usage.reasoning, 0)
    : undefined;

  return { reasoning, output, reasoningTokens, contentTokens };
}
