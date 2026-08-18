export type APIProtocol =
  "Anthropic Messages" | "OpenAI Chat Completions" | "OpenAI Responses";

export function getAPIProtocol(endpoint?: string): APIProtocol {
  if (endpoint?.includes("/responses")) return "OpenAI Responses";
  if (endpoint?.includes("/chat/completions")) return "OpenAI Chat Completions";
  return "Anthropic Messages";
}

export function getProtocolBadgeClasses(endpoint?: string): string {
  switch (getAPIProtocol(endpoint)) {
    case "OpenAI Responses":
      return "bg-emerald-100 text-emerald-700";
    case "OpenAI Chat Completions":
      return "bg-green-100 text-green-700";
    default:
      return "bg-orange-100 text-orange-700";
  }
}

export function isOpenAIModel(model: string | null | undefined): boolean {
  if (!model) return false;
  return model.startsWith("gpt-") || model.startsWith("o");
}

export function getProviderName(
  model: string | null | undefined,
  endpoint?: string
): "OpenAI" | "Anthropic" {
  return getAPIProtocol(endpoint).startsWith("OpenAI") || isOpenAIModel(model)
    ? "OpenAI"
    : "Anthropic";
}

// Retained for callers that used the old helper. The actual endpoint is always
// authoritative when it is available.
export function getChatCompletionsEndpoint(
  model: string | null | undefined,
  defaultEndpoint?: string
): string {
  if (defaultEndpoint) return defaultEndpoint;
  return isOpenAIModel(model) ? "/v1/chat/completions" : "/v1/messages";
}

export function getUsage(body: unknown): {
  input: number;
  contextInput: number;
  output: number;
  total: number;
  cached: number;
  cacheAvailable: boolean;
  prefixCacheHitRate: number | null;
  reasoning: number;
} | null {
  if (!body || typeof body !== "object") return null;
  const usageValue = (body as Record<string, unknown>).usage;
  if (!usageValue || typeof usageValue !== "object") return null;
  const usage = usageValue as Record<string, unknown>;
  const inputDetails =
    usage.input_tokens_details && typeof usage.input_tokens_details === "object"
      ? (usage.input_tokens_details as Record<string, unknown>)
      : {};
  const promptDetails =
    usage.prompt_tokens_details &&
    typeof usage.prompt_tokens_details === "object"
      ? (usage.prompt_tokens_details as Record<string, unknown>)
      : {};
  const outputDetails =
    usage.output_tokens_details &&
    typeof usage.output_tokens_details === "object"
      ? (usage.output_tokens_details as Record<string, unknown>)
      : {};
  const completionDetails =
    usage.completion_tokens_details &&
    typeof usage.completion_tokens_details === "object"
      ? (usage.completion_tokens_details as Record<string, unknown>)
      : {};

  const numberValue = (value: unknown) =>
    typeof value === "number" && Number.isFinite(value) ? value : 0;
  const input = numberValue(usage.input_tokens ?? usage.prompt_tokens);
  const output = numberValue(usage.output_tokens ?? usage.completion_tokens);
  const cachedValue =
    usage.input_tokens_cached ??
    usage.cache_read_input_tokens ??
    inputDetails.cached_tokens ??
    promptDetails.cached_tokens ??
    usage.prompt_cache_hit_tokens ??
    usage.cached_tokens;
  const cacheAvailable = cachedValue !== undefined && cachedValue !== null;
  const cached = numberValue(cachedValue);
  const anthropicCacheUsage =
    usage.cache_read_input_tokens !== undefined ||
    usage.cache_creation_input_tokens !== undefined;
  const contextInput = anthropicCacheUsage
    ? input +
      numberValue(usage.cache_read_input_tokens) +
      numberValue(usage.cache_creation_input_tokens)
    : input;
  const explicitHitRate =
    usage.prefix_cache_hit_rate ?? usage.prompt_cache_hit_rate;
  const normalizedExplicitHitRate =
    typeof explicitHitRate === "number" && Number.isFinite(explicitHitRate)
      ? explicitHitRate > 1
        ? explicitHitRate / 100
        : explicitHitRate
      : null;
  const prefixCacheHitRate =
    normalizedExplicitHitRate ??
    (cacheAvailable && contextInput > 0 ? cached / contextInput : null);
  return {
    input,
    contextInput,
    output,
    total: numberValue(usage.total_tokens) || contextInput + output,
    cached,
    cacheAvailable,
    prefixCacheHitRate,
    reasoning: numberValue(
      outputDetails.reasoning_tokens ?? completionDetails.reasoning_tokens
    ),
  };
}
