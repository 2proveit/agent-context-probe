export type APIProtocol =
  | 'Anthropic Messages'
  | 'OpenAI Chat Completions'
  | 'OpenAI Responses';

export function getAPIProtocol(endpoint?: string): APIProtocol {
  if (endpoint?.includes('/responses')) return 'OpenAI Responses';
  if (endpoint?.includes('/chat/completions')) return 'OpenAI Chat Completions';
  return 'Anthropic Messages';
}

export function getProtocolBadgeClasses(endpoint?: string): string {
  switch (getAPIProtocol(endpoint)) {
    case 'OpenAI Responses':
      return 'bg-emerald-100 text-emerald-700';
    case 'OpenAI Chat Completions':
      return 'bg-green-100 text-green-700';
    default:
      return 'bg-orange-100 text-orange-700';
  }
}

export function isOpenAIModel(model: string | null | undefined): boolean {
  if (!model) return false;
  return model.startsWith('gpt-') || model.startsWith('o');
}

export function getProviderName(
  model: string | null | undefined,
  endpoint?: string,
): 'OpenAI' | 'Anthropic' {
  return getAPIProtocol(endpoint).startsWith('OpenAI') || isOpenAIModel(model)
    ? 'OpenAI'
    : 'Anthropic';
}

// Retained for callers that used the old helper. The actual endpoint is always
// authoritative when it is available.
export function getChatCompletionsEndpoint(
  model: string | null | undefined,
  defaultEndpoint?: string,
): string {
  if (defaultEndpoint) return defaultEndpoint;
  return isOpenAIModel(model) ? '/v1/chat/completions' : '/v1/messages';
}

export function getUsage(body: any): {
  input: number;
  output: number;
  total: number;
  cached: number;
  reasoning: number;
} | null {
  const usage = body?.usage;
  if (!usage || typeof usage !== 'object') return null;

  const input = usage.input_tokens ?? usage.prompt_tokens ?? 0;
  const output = usage.output_tokens ?? usage.completion_tokens ?? 0;
  return {
    input,
    output,
    total: usage.total_tokens ?? input + output,
    cached:
      usage.cache_read_input_tokens ??
      usage.input_tokens_details?.cached_tokens ??
      usage.prompt_tokens_details?.cached_tokens ??
      0,
    reasoning:
      usage.output_tokens_details?.reasoning_tokens ??
      usage.completion_tokens_details?.reasoning_tokens ??
      0,
  };
}
