/* eslint-disable @typescript-eslint/no-explicit-any -- OpenAI and Anthropic response blocks are runtime JSON. */

import { useState } from "react";
import {
  AlertCircle,
  Bot,
  Brain,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Clock3,
  Copy,
  FileText,
  GitBranch,
  Loader2,
  MessageSquareText,
  Timer,
  Wrench,
  XCircle,
  Zap,
} from "lucide-react";
import type {
  RequestRecord,
  SessionDetail,
  SessionSummary,
  ToolExecutionWindow,
} from "../types";
import { normalizeAssistantResponse } from "../utils/assistantResponse";
import { getUsage } from "../utils/models";

interface SessionViewProps {
  sessions: SessionSummary[];
  total: number;
  selectedSessionId: string | null;
  detail: SessionDetail | null;
  isLoadingList: boolean;
  isLoadingDetail: boolean;
  onSelectSession: (sessionId: string) => void;
  onOpenRequest: (request: RequestRecord) => void;
}

interface TimelineToolCall {
  id?: string;
  name: string;
  arguments?: unknown;
}

interface TimelineToolResult {
  content: unknown;
  isError: boolean;
}

interface WaterfallRange {
  startMs: number;
  durationMs: number;
}

interface SessionContextMetrics {
  inputTokens: number;
  inputTokensCached: number | null;
  outputTokens: number;
  prefixCacheHitRate: number | null;
}

const STATUS_STYLES: Record<
  string,
  { label: string; className: string; dot: string }
> = {
  completed: {
    label: "Completed",
    className: "bg-emerald-50 text-emerald-700 border-emerald-200",
    dot: "bg-emerald-500",
  },
  error: {
    label: "Error",
    className: "bg-red-50 text-red-700 border-red-200",
    dot: "bg-red-500",
  },
  interrupted: {
    label: "Interrupted",
    className: "bg-red-50 text-red-700 border-red-200",
    dot: "bg-red-500",
  },
  pending: {
    label: "Pending",
    className: "bg-amber-50 text-amber-700 border-amber-200",
    dot: "bg-amber-500",
  },
  "awaiting-tool": {
    label: "Tool call",
    className: "bg-blue-50 text-blue-700 border-blue-200",
    dot: "bg-blue-500",
  },
  "awaiting-result": {
    label: "No parent result",
    className: "bg-amber-50 text-amber-700 border-amber-200",
    dot: "bg-amber-500",
  },
  captured: {
    label: "Captured",
    className: "bg-gray-50 text-gray-600 border-gray-200",
    dot: "bg-gray-400",
  },
};

function statusStyle(status: string) {
  return STATUS_STYLES[status] ?? STATUS_STYLES.captured;
}

function formatCompactNumber(value: number) {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}m`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}k`;
  return value.toLocaleString();
}

function formatCount(value: number, noun: string) {
  return `${value} ${noun}${value === 1 ? "" : "s"}`;
}

function formatDuration(milliseconds: number) {
  if (milliseconds < 1_000) return `${milliseconds}ms`;
  const seconds = milliseconds / 1_000;
  if (seconds < 60) return `${seconds.toFixed(seconds >= 10 ? 1 : 2)}s`;
  const minutes = Math.floor(seconds / 60);
  return `${minutes}m ${Math.round(seconds % 60)}s`;
}

function relativeDate(timestamp: string) {
  const date = new Date(timestamp);
  const now = new Date();
  const sameDay = date.toDateString() === now.toDateString();
  if (sameDay)
    return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  return date.toLocaleDateString([], { month: "short", day: "numeric" });
}

function timestampHasSubsecondPrecision(timestamp: string) {
  const separator = timestamp.indexOf("T");
  return separator >= 0 && timestamp.slice(separator + 1).includes(".");
}

function stripContextBlocks(value: string) {
  return value
    .replace(/<user_memory_context>[\s\S]*?<\/user_memory_context>/g, "")
    .replace(/<memory_maintenance_job>[\s\S]*?<\/memory_maintenance_job>/g, "")
    .replace(/<lumi_workspace>[\s\S]*?<\/lumi_workspace>/g, "")
    .trim();
}

function initialUserPrompt(requests: RequestRecord[]) {
  for (const request of requests) {
    for (const message of request.body?.messages ?? []) {
      if (message.role !== "user") continue;
      if (typeof message.content === "string")
        return stripContextBlocks(message.content);
      if (Array.isArray(message.content)) {
        const text = message.content
          .map((block: any) => block?.text ?? "")
          .filter(Boolean)
          .join("\n");
        if (text) return stripContextBlocks(text);
      }
    }
  }
  return "";
}

function parseArguments(value: unknown) {
  if (typeof value !== "string") return value;
  try {
    return JSON.parse(value);
  } catch {
    return value;
  }
}

function responseToolCalls(request: RequestRecord): TimelineToolCall[] {
  const body = request.response?.body;
  const calls: TimelineToolCall[] = [];
  for (const item of body?.choices?.[0]?.message?.tool_calls ?? []) {
    calls.push({
      id: item?.id,
      name: item?.function?.name ?? "tool",
      arguments: parseArguments(item?.function?.arguments),
    });
  }
  for (const item of body?.content ?? []) {
    if (item?.type === "tool_use") {
      calls.push({
        id: item.id,
        name: item.name ?? "tool",
        arguments: item.input,
      });
    }
  }
  for (const item of body?.output ?? []) {
    if (item?.type === "function_call") {
      calls.push({
        id: item.call_id ?? item.id,
        name: item.name ?? "tool",
        arguments: parseArguments(item.arguments),
      });
    }
  }
  return calls;
}

function toolResults(requests: RequestRecord[]) {
  const results = new Map<string, TimelineToolResult>();
  const save = (id: unknown, content: unknown, isError = false) => {
    if (typeof id === "string" && id) results.set(id, { content, isError });
  };

  for (const request of requests) {
    for (const message of request.body?.messages ?? []) {
      if (message.role === "tool") {
        save(message.tool_call_id, message.content);
      }
      if (!Array.isArray(message.content)) continue;
      for (const block of message.content) {
        if (block?.type === "tool_result") {
          save(block.tool_use_id, block.content, Boolean(block.is_error));
        }
      }
    }

    const input = request.body?.input;
    if (Array.isArray(input)) {
      for (const item of input) {
        if (item?.type === "function_call_output") {
          save(item.call_id ?? item.id, item.output, Boolean(item.is_error));
        }
      }
    }
  }
  return results;
}

function formatPayload(value: unknown) {
  if (value === undefined || value === null || value === "")
    return "No output captured";
  if (typeof value === "string") {
    try {
      return JSON.stringify(JSON.parse(value), null, 2);
    } catch {
      return value;
    }
  }
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function summarizeArguments(call: TimelineToolCall) {
  const args = call.arguments;
  if (!args || typeof args !== "object")
    return typeof args === "string" ? args : "";
  const record = args as Record<string, unknown>;
  if (call.name === "task") {
    return String(
      record.description ?? record.subagent_type ?? "Subagent task"
    );
  }
  for (const key of [
    "description",
    "command",
    "filePath",
    "path",
    "query",
    "name",
  ]) {
    if (typeof record[key] === "string" && record[key])
      return String(record[key]);
  }
  const keys = Object.keys(record);
  return keys.length ? keys.slice(0, 3).join(", ") : "";
}

function modelLabel(model?: string) {
  if (!model) return "Model";
  if (model.toLowerCase().includes("deepseek")) return "DeepSeek";
  if (model.toLowerCase().includes("atlas")) return "iFinD Atlas";
  return model;
}

function sessionWaterfallRange(
  summary: SessionSummary,
  requests: RequestRecord[]
): WaterfallRange {
  const summaryStart = Date.parse(summary.firstTimestamp);
  const requestStart = requests.length
    ? Date.parse(requests[0].timestamp)
    : Number.NaN;
  return {
    startMs: Number.isFinite(summaryStart)
      ? summaryStart
      : Number.isFinite(requestStart)
        ? requestStart
        : 0,
    durationMs: Math.max(summary.elapsedTimeMs, 1),
  };
}

function sessionContextMetrics(
  summary: SessionSummary,
  requests: RequestRecord[]
): SessionContextMetrics {
  const usages = requests
    .map((request) => getUsage(request.response?.body))
    .filter((usage): usage is NonNullable<ReturnType<typeof getUsage>> =>
      Boolean(usage)
    );
  if (usages.length === 0) {
    return {
      inputTokens: summary.inputTokens,
      inputTokensCached: null,
      outputTokens: summary.outputTokens,
      prefixCacheHitRate: null,
    };
  }

  const inputTokens = usages.reduce(
    (total, usage) => total + usage.contextInput,
    0
  );
  const outputTokens = usages.reduce((total, usage) => total + usage.output, 0);
  const allCachedCountsAvailable = usages.every(
    (usage) => usage.cacheAvailable
  );
  const inputTokensCached = allCachedCountsAvailable
    ? usages.reduce((total, usage) => total + usage.cached, 0)
    : null;
  const allHitRatesAvailable = usages.every(
    (usage) => usage.prefixCacheHitRate !== null
  );
  const prefixCacheHitRate =
    inputTokens > 0 && allHitRatesAvailable
      ? usages.reduce(
          (total, usage) =>
            total + (usage.prefixCacheHitRate ?? 0) * usage.contextInput,
          0
        ) / inputTokens
      : null;

  return { inputTokens, inputTokensCached, outputTokens, prefixCacheHitRate };
}

function formatObservedDuration(window: ToolExecutionWindow) {
  if (!window.complete) return "Timing unavailable";
  if (window.approximate && window.durationMs === 0)
    return "~<1s observed window";
  return `${window.approximate ? "~" : ""}${formatDuration(window.durationMs)} observed window`;
}

function TraceGridRow({
  left,
  right,
  className = "",
}: {
  left: React.ReactNode;
  right?: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      className={`grid min-w-0 grid-cols-1 gap-x-5 xl:grid-cols-[minmax(0,3fr)_minmax(240px,2fr)] ${className}`}
    >
      <div className="min-w-0">{left}</div>
      <div className="relative hidden min-w-0 xl:block">{right}</div>
    </div>
  );
}

function WaterfallBar({
  range,
  startTimestamp,
  durationMs,
  kind,
  approximate = false,
  label,
}: {
  range: WaterfallRange;
  startTimestamp?: string;
  durationMs: number;
  kind: "model" | "tool" | "subagent";
  approximate?: boolean;
  label: string;
}) {
  const startMs = startTimestamp ? Date.parse(startTimestamp) : Number.NaN;
  if (!Number.isFinite(startMs)) return null;
  const left = Math.max(
    0,
    Math.min(100, ((startMs - range.startMs) / range.durationMs) * 100)
  );
  const rawWidth = (Math.max(durationMs, 0) / range.durationMs) * 100;
  const width = Math.max(0, Math.min(rawWidth, 100 - left));
  const color =
    kind === "tool"
      ? "bg-amber-400 border-amber-500"
      : kind === "subagent"
        ? "bg-violet-500 border-violet-600"
        : "bg-blue-500 border-blue-600";
  const stripeStyle =
    kind === "tool"
      ? {
          backgroundImage:
            "repeating-linear-gradient(135deg, rgba(255,255,255,.45) 0, rgba(255,255,255,.45) 4px, transparent 4px, transparent 8px)",
        }
      : undefined;
  const durationLabel =
    approximate && durationMs === 0
      ? "~<1s"
      : `${approximate ? "~" : ""}${formatDuration(durationMs)}`;
  const timingKind =
    kind === "tool"
      ? "Observed tool window"
      : kind === "subagent"
        ? "Measured subagent E2E"
        : "Measured model latency";
  const startLabel = new Date(startMs).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
  const tooltipPosition = left > 65 ? "right-0" : "left-0";

  return (
    <div
      className="relative mt-0.5 h-6 overflow-visible rounded-md border border-gray-200 bg-gray-50"
      style={{
        backgroundImage:
          "linear-gradient(to right, transparent 49.75%, #e5e7eb 50%, transparent 50.25%)",
      }}
    >
      <button
        type="button"
        data-waterfall-kind={kind}
        data-approximate={approximate ? "true" : "false"}
        aria-label={`${label}, ${timingKind}, ${durationLabel}, starts ${startLabel}${approximate ? ", approximate" : ""}`}
        className={`group absolute top-1 h-4 rounded-sm border outline-none hover:z-30 focus:z-30 focus-visible:ring-2 focus-visible:ring-gray-900 focus-visible:ring-offset-1 ${color}`}
        style={{
          left: `${left}%`,
          width: `${width}%`,
          minWidth: "3px",
          ...stripeStyle,
        }}
      >
        <div
          data-waterfall-tooltip={kind}
          className={`pointer-events-none absolute bottom-full z-30 mb-2 hidden w-max max-w-72 rounded-lg bg-gray-950 px-3 py-2 text-left text-[11px] normal-case tracking-normal text-white shadow-lg group-hover:block group-focus:block ${tooltipPosition}`}
        >
          <div className="max-w-64 truncate font-medium">{label}</div>
          <div className="mt-1 flex items-center gap-2 whitespace-nowrap text-gray-300">
            <span>{timingKind}</span>
            <span className="text-gray-600">·</span>
            <span className="font-medium tabular-nums text-white">
              Duration {durationLabel}
            </span>
          </div>
          <div className="mt-0.5 whitespace-nowrap text-gray-400">
            Start {startLabel}
            {approximate ? " · Approximate" : ""}
          </div>
        </div>
      </button>
    </div>
  );
}

function WaterfallAxis({ range }: { range: WaterfallRange }) {
  return (
    <TraceGridRow
      className="mb-3 border-b border-gray-100 pb-2"
      left={
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-gray-500">
          <span className="font-medium uppercase tracking-wide text-gray-400">
            Tree + Waterfall
          </span>
          <span className="inline-flex items-center gap-1.5">
            <span className="h-2.5 w-4 rounded-sm bg-blue-500" />
            Measured model
          </span>
          <span className="inline-flex items-center gap-1.5">
            <span className="h-2.5 w-4 rounded-sm bg-violet-500" />
            Measured subagent
          </span>
          <span className="inline-flex items-center gap-1.5">
            <span className="h-2.5 w-4 rounded-sm border border-amber-500 bg-amber-300" />
            Observed tool window
          </span>
        </div>
      }
      right={
        <div className="flex justify-between text-[10px] tabular-nums text-gray-400">
          <span>0s</span>
          <span>{formatDuration(range.durationMs / 2)}</span>
          <span>E2E {formatDuration(range.durationMs)}</span>
        </div>
      }
    />
  );
}

function SessionContextUsage({ metrics }: { metrics: SessionContextMetrics }) {
  const totalTokens = metrics.inputTokens + metrics.outputTokens;
  const cachedLabel =
    metrics.inputTokensCached === null
      ? "N/A"
      : metrics.inputTokensCached.toLocaleString();
  const hitRateLabel =
    metrics.prefixCacheHitRate === null
      ? "N/A"
      : `${(metrics.prefixCacheHitRate * 100).toFixed(2)}%`;

  return (
    <button
      type="button"
      data-testid="session-context-metrics"
      aria-label={`Context usage: input tokens ${metrics.inputTokens}, cached input tokens ${cachedLabel}, output tokens ${metrics.outputTokens}, prefix cache hit rate ${hitRateLabel}`}
      className="group relative inline-flex items-center gap-1 rounded-md outline-none hover:text-gray-800 focus:text-gray-800 focus-visible:ring-2 focus-visible:ring-gray-900 focus-visible:ring-offset-2"
    >
      <Zap className="h-3.5 w-3.5" />
      <span>{formatCompactNumber(totalTokens)} tokens</span>
      <span
        data-testid="session-context-tooltip"
        className="pointer-events-none absolute right-0 top-full z-40 mt-2 hidden w-64 rounded-lg bg-gray-950 px-3 py-3 text-left font-mono text-[11px] leading-5 text-white shadow-lg group-hover:block group-focus:block"
      >
        <span className="mb-1 block font-sans text-xs font-medium text-white">
          Context usage
        </span>
        <span className="flex justify-between gap-4 text-gray-300">
          <span>input_tokens:</span>
          <span className="tabular-nums text-white">
            {metrics.inputTokens.toLocaleString()}
          </span>
        </span>
        <span className="ml-3 flex justify-between gap-4 border-l border-gray-700 pl-2 text-gray-400">
          <span>input_tokens_cached:</span>
          <span className="tabular-nums text-white">{cachedLabel}</span>
        </span>
        <span className="flex justify-between gap-4 text-gray-300">
          <span>output_tokens:</span>
          <span className="tabular-nums text-white">
            {metrics.outputTokens.toLocaleString()}
          </span>
        </span>
        <span className="flex justify-between gap-4 text-gray-300">
          <span>prefix_cache_hit_rate:</span>
          <span className="tabular-nums text-white">{hitRateLabel}</span>
        </span>
      </span>
    </button>
  );
}

function SessionListItem({
  session,
  selected,
  onSelect,
}: {
  session: SessionSummary;
  selected: boolean;
  onSelect: () => void;
}) {
  const style = statusStyle(session.status);
  return (
    <button
      type="button"
      onClick={onSelect}
      data-session-id={session.sessionId}
      className={`w-full border-b border-gray-200 px-4 py-4 text-left transition ${
        selected
          ? "bg-white shadow-[inset_3px_0_0_#2563eb]"
          : "bg-gray-50/50 hover:bg-white"
      }`}
      aria-current={selected ? "page" : undefined}
    >
      <div className="flex items-start gap-3">
        <div className="mt-1 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-gray-100 text-gray-600">
          {session.children?.length ? (
            <GitBranch className="h-4 w-4" />
          ) : (
            <MessageSquareText className="h-4 w-4" />
          )}
        </div>
        <div className="min-w-0 flex-1">
          <div className="line-clamp-2 text-sm font-medium leading-5 text-gray-900">
            {session.title}
          </div>
          <div className="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-gray-500">
            <span>{formatCount(session.requestCount, "step")}</span>
            {session.children?.length ? (
              <span>{formatCount(session.children.length, "subagent")}</span>
            ) : null}
            <span>{relativeDate(session.lastTimestamp)}</span>
          </div>
          <div className="mt-2 flex items-center justify-between gap-2">
            <span className="truncate font-mono text-[11px] text-gray-400">
              {session.sessionId}
            </span>
            <span
              className={`h-2 w-2 shrink-0 rounded-full ${style.dot}`}
              title={style.label}
            />
          </div>
        </div>
      </div>
    </button>
  );
}

function TimelineItem({
  icon,
  title,
  description,
  request,
  onOpenRequest,
  tone = "default",
}: {
  icon: React.ReactNode;
  title: string;
  description?: string;
  request: RequestRecord;
  onOpenRequest: (request: RequestRecord) => void;
  tone?: "default" | "error" | "task";
}) {
  const toneClasses =
    tone === "error"
      ? "border-red-200 bg-red-50/60 hover:bg-red-50"
      : tone === "task"
        ? "border-blue-200 bg-blue-50/50 hover:bg-blue-50"
        : "border-transparent hover:border-gray-200 hover:bg-gray-50";

  return (
    <button
      type="button"
      onClick={() => onOpenRequest(request)}
      data-request-id={request.requestId}
      className={`group flex w-full items-center gap-3 rounded-lg border px-3 py-2.5 text-left transition ${toneClasses}`}
    >
      <span className="flex h-7 w-7 shrink-0 items-center justify-center text-gray-500">
        {icon}
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex min-w-0 items-baseline gap-2">
          <span className="shrink-0 text-sm font-medium text-gray-900">
            {title}
          </span>
          {description ? (
            <span className="truncate text-sm text-gray-500">
              — {description}
            </span>
          ) : null}
        </span>
      </span>
      <span className="hidden shrink-0 items-center gap-3 text-xs text-gray-400 sm:flex">
        <span
          className={`h-2 w-2 rounded-full ${request.response?.streamError ? "bg-red-500" : "bg-emerald-500"}`}
        />
        <ChevronRight className="h-4 w-4 transition group-hover:translate-x-0.5" />
      </span>
    </button>
  );
}

function AssistantTimelineItem({
  kind,
  text,
  request,
  tokenCount,
}: {
  kind: "reasoning" | "output";
  text: string;
  request: RequestRecord;
  tokenCount?: number;
}) {
  const [expanded, setExpanded] = useState(false);
  const reasoning = kind === "reasoning";
  const title = reasoning ? "Thinking" : "Output";

  return (
    <div
      data-assistant-message-kind={kind}
      data-request-id={request.requestId}
      data-expanded={expanded ? "true" : "false"}
      className={`overflow-hidden rounded-lg border transition ${
        expanded
          ? "border-gray-200 bg-white shadow-sm"
          : "border-transparent hover:border-gray-200 hover:bg-gray-50"
      }`}
    >
      <button
        type="button"
        onClick={() => setExpanded((current) => !current)}
        aria-expanded={expanded}
        className="group flex w-full items-center gap-3 px-3 py-2.5 text-left"
      >
        <span className="flex h-7 w-7 shrink-0 items-center justify-center text-gray-500">
          {reasoning ? (
            <Brain className="h-4 w-4" />
          ) : (
            <MessageSquareText className="h-4 w-4" />
          )}
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex min-w-0 items-baseline gap-2">
            <span className="shrink-0 text-sm font-medium text-gray-900">
              {title}
            </span>
            <span className="truncate text-sm text-gray-500">
              — {text.replace(/\s+/g, " ").slice(0, 160)}
            </span>
          </span>
        </span>
        <span className="hidden shrink-0 items-center gap-3 text-xs text-gray-400 sm:flex">
          {tokenCount !== undefined ? (
            <span>
              {formatCompactNumber(tokenCount)}{" "}
              {reasoning ? "reasoning" : "content"} tokens
            </span>
          ) : null}
          <span
            className={`h-2 w-2 rounded-full ${request.response?.streamError ? "bg-red-500" : "bg-emerald-500"}`}
          />
          <ChevronDown
            className={`h-4 w-4 transition-transform ${expanded ? "rotate-180" : ""}`}
          />
        </span>
      </button>

      {expanded ? (
        <div className="border-t border-gray-200 px-4 pb-4 pt-3 sm:ml-11 sm:mr-3">
          <div className="mb-2 text-xs font-medium uppercase tracking-wide text-gray-500">
            {reasoning ? "Reasoning content" : "Model output"}
          </div>
          <div className="max-h-[32rem] overflow-auto whitespace-pre-wrap break-words rounded-lg border border-gray-200 bg-gray-50 p-4 text-sm leading-6 text-gray-700 scrollbar-custom">
            {text}
          </div>
        </div>
      ) : null}
    </div>
  );
}

function ToolTimelineItem({
  icon,
  call,
  result,
  request,
  delegatedSession,
  onOpenRequest,
  waterfallRange,
}: {
  icon: React.ReactNode;
  call: TimelineToolCall;
  result?: TimelineToolResult;
  request: RequestRecord;
  delegatedSession?: SessionDetail;
  onOpenRequest: (request: RequestRecord) => void;
  waterfallRange: WaterfallRange;
}) {
  const [expanded, setExpanded] = useState(false);
  const [outputExpanded, setOutputExpanded] = useState(false);
  const task = call.name === "task";
  const delegatedStatus = delegatedSession?.summary.status;
  const resultDot =
    delegatedStatus === "error" ||
    delegatedStatus === "interrupted" ||
    result?.isError
      ? "bg-red-500"
      : delegatedStatus === "completed" || result
        ? "bg-emerald-500"
        : delegatedSession
          ? "bg-amber-400"
          : "bg-gray-300";

  return (
    <div
      data-tool-call-id={call.id || `${request.requestId}:${call.name}`}
      data-expanded={expanded ? "true" : "false"}
      className={`overflow-hidden rounded-lg border transition ${
        expanded
          ? task
            ? "border-blue-200 bg-blue-50/30"
            : "border-gray-200 bg-white shadow-sm"
          : task
            ? "border-blue-200 bg-blue-50/50 hover:bg-blue-50"
            : "border-transparent hover:border-gray-200 hover:bg-gray-50"
      }`}
    >
      <button
        type="button"
        onClick={() => setExpanded((current) => !current)}
        aria-expanded={expanded}
        className="group flex w-full items-center gap-3 px-3 py-2.5 text-left"
      >
        <span className="flex h-7 w-7 shrink-0 items-center justify-center text-gray-500">
          {icon}
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex min-w-0 items-baseline gap-2">
            <span className="shrink-0 text-sm font-medium text-gray-900">
              {call.name}
            </span>
            <span className="truncate text-sm text-gray-500">
              — {summarizeArguments(call)}
            </span>
          </span>
        </span>
        <span className="hidden shrink-0 items-center gap-3 text-xs text-gray-400 sm:flex">
          <span className={`h-2 w-2 rounded-full ${resultDot}`} />
          <ChevronDown
            className={`h-4 w-4 transition-transform ${expanded ? "rotate-180" : ""}`}
          />
        </span>
      </button>

      {expanded ? (
        <div className="border-t border-gray-200 px-4 pb-4 pt-3 sm:ml-11 sm:mr-3">
          <div className="mb-2 text-xs font-medium uppercase tracking-wide text-gray-500">
            Input
          </div>
          <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-words rounded-lg border border-gray-200 bg-gray-50 p-4 font-mono text-xs leading-5 text-gray-700 scrollbar-custom">
            {formatPayload(call.arguments)}
          </pre>

          <div className="mt-3 overflow-hidden rounded-lg border border-gray-200 bg-white">
            <button
              type="button"
              onClick={() => setOutputExpanded((current) => !current)}
              aria-expanded={outputExpanded}
              className="flex w-full items-center gap-2 px-3 py-2.5 text-left text-sm text-gray-600 hover:bg-gray-50"
            >
              <ChevronRight
                className={`h-4 w-4 transition-transform ${outputExpanded ? "rotate-90" : ""}`}
              />
              <span className="font-medium">Output</span>
              <span className={`h-2 w-2 rounded-full ${resultDot}`} />
              {!result ? (
                <span className="text-xs text-gray-400">not captured</span>
              ) : null}
            </button>
            {outputExpanded ? (
              <pre
                className={`max-h-80 overflow-auto whitespace-pre-wrap break-words border-t p-4 font-mono text-xs leading-5 scrollbar-custom ${
                  result?.isError
                    ? "border-red-100 bg-red-50 text-red-700"
                    : "border-gray-200 bg-gray-50 text-gray-700"
                }`}
              >
                {formatPayload(result?.content)}
              </pre>
            ) : null}
          </div>

          {task && delegatedSession ? (
            <div className="mt-4">
              <div className="mb-2 text-xs font-medium uppercase tracking-wide text-gray-500">
                Delegated subagent
              </div>
              <SessionTimeline
                detail={delegatedSession}
                onOpenRequest={onOpenRequest}
                child
                waterfallRange={waterfallRange}
              />
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

function SessionTimeline({
  detail,
  onOpenRequest,
  child = false,
  waterfallRange,
}: {
  detail: SessionDetail;
  onOpenRequest: (request: RequestRecord) => void;
  child?: boolean;
  waterfallRange?: WaterfallRange;
}) {
  const prompt = initialUserPrompt(detail.requests);
  const [promptCopied, setPromptCopied] = useState(false);
  const summary = detail.summary;
  const style = statusStyle(summary.status);
  const contextMetrics = sessionContextMetrics(summary, detail.requests);
  const results = toolResults(detail.requests);
  const childByTaskCall = new Map(
    (detail.children ?? [])
      .filter((childDetail) => Boolean(childDetail.summary.taskCallId))
      .map((childDetail) => [
        childDetail.summary.taskCallId as string,
        childDetail,
      ])
  );
  const promptLabel = child ? "Task prompt" : "User Prompt";
  const range =
    waterfallRange ?? sessionWaterfallRange(summary, detail.requests);
  const windowsByRequest = new Map(
    (detail.toolWindows ?? []).map((window) => [window.requestId, window])
  );
  const measuredSpans = [
    ...detail.requests.map((request) => ({
      kind: "model" as const,
      id: request.requestId,
      durationMs: request.response?.responseTime ?? 0,
    })),
    ...(detail.children ?? []).map((childDetail) => ({
      kind: "subagent" as const,
      id: childDetail.summary.sessionId,
      durationMs: childDetail.summary.elapsedTimeMs,
    })),
  ];
  const longestMeasured = measuredSpans.reduce<
    (typeof measuredSpans)[number] | undefined
  >(
    (longest, span) =>
      !longest || span.durationMs > longest.durationMs ? span : longest,
    undefined
  );
  const longestToolWindow = (detail.toolWindows ?? [])
    .filter((window) => window.complete)
    .reduce<ToolExecutionWindow | undefined>(
      (longest, window) =>
        !longest || window.durationMs > longest.durationMs ? window : longest,
      undefined
    );

  const copyPrompt = async () => {
    try {
      await navigator.clipboard.writeText(prompt);
      setPromptCopied(true);
      window.setTimeout(() => setPromptCopied(false), 2_000);
    } catch (error) {
      console.error("Failed to copy prompt:", error);
    }
  };

  return (
    <section
      data-session-timeline-id={summary.sessionId}
      className={child ? "rounded-xl border border-gray-200 bg-white" : ""}
    >
      <div
        className={`flex flex-col gap-3 ${child ? "border-b border-gray-200 px-5 py-4" : "pb-5"} sm:flex-row sm:items-center sm:justify-between`}
      >
        <div className="flex min-w-0 items-center gap-3">
          <div
            className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-lg ${child ? "bg-blue-50 text-blue-600" : "bg-gray-900 text-white"}`}
          >
            <Bot className="h-5 w-5" />
          </div>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h3 className="truncate text-base font-semibold text-gray-900">
                {child ? summary.agentName || "Subagent" : "Root Agent"}
              </h3>
              <span className="text-sm text-gray-500">
                {modelLabel(summary.model)}
              </span>
              {child ? (
                <span className="text-sm text-gray-500">
                  {summary.taskDescription}
                </span>
              ) : null}
              <span
                className={`rounded-full border px-2 py-0.5 text-[11px] font-medium ${style.className}`}
              >
                {style.label}
              </span>
            </div>
            <p className="mt-0.5 truncate font-mono text-xs text-gray-400">
              {summary.sessionId}
            </p>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-3 text-xs text-gray-500">
          <span>{formatCount(summary.toolCallCount, "tool call")}</span>
          <span>{formatCount(summary.requestCount, "step")}</span>
          <SessionContextUsage metrics={contextMetrics} />
          <span
            className="inline-flex items-center gap-1"
            title="End-to-end wall-clock span, including tool calls, subagents, and idle gaps"
          >
            <Timer className="h-3.5 w-3.5" />
            E2E {formatDuration(summary.elapsedTimeMs)}
          </span>
        </div>
      </div>

      <div className={child ? "px-5 py-5" : ""}>
        {prompt ? (
          <div className="mb-4 rounded-xl border border-gray-200 bg-stone-50 px-4 py-3">
            <div className="mb-1 flex items-center justify-between gap-3">
              <div className="flex items-center gap-2 text-xs font-medium uppercase tracking-wide text-gray-500">
                <FileText className="h-3.5 w-3.5" />
                {promptLabel}
              </div>
              <button
                type="button"
                onClick={copyPrompt}
                data-testid="copy-session-prompt"
                title={`Copy ${promptLabel}`}
                aria-label={`Copy ${promptLabel}`}
                className="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium text-gray-500 transition hover:bg-white hover:text-gray-800"
              >
                {promptCopied ? (
                  <Check className="h-3.5 w-3.5 text-emerald-600" />
                ) : (
                  <Copy className="h-3.5 w-3.5" />
                )}
                <span>{promptCopied ? "Copied" : "Copy"}</span>
              </button>
            </div>
            <p className="whitespace-pre-wrap break-words text-sm leading-6 text-gray-700">
              {prompt}
            </p>
          </div>
        ) : null}

        <WaterfallAxis range={range} />

        <div className="relative space-y-1 pl-5 before:absolute before:bottom-2 before:left-[7px] before:top-2 before:w-px before:bg-gray-200">
          {detail.requests.map((rawRequest, index) => {
            const request = {
              ...rawRequest,
              id: rawRequest.id || rawRequest.requestId,
            };
            const response = normalizeAssistantResponse(
              request.response?.body,
              request.endpoint
            );
            const text = response.output;
            const reasoning = response.reasoning;
            const tools = responseToolCalls(request);
            const usage = getUsage(request.response?.body);
            const toolWindow =
              windowsByRequest.get(request.requestId) ??
              (tools.length
                ? {
                    requestId: request.requestId,
                    toolNames: tools.map((tool) => tool.name),
                    durationMs: 0,
                    approximate: true,
                    complete: false,
                  }
                : undefined);
            const isFinalOutput =
              index === detail.requests.length - 1 &&
              Boolean(text) &&
              tools.length === 0;
            const stepLabel = `Step ${index + 1}${isFinalOutput ? " · Final Output" : ""} · Model: ${request.routedModel || request.body?.model || "Unknown"}`;
            const delegatedSpans = tools
              .map((tool) =>
                tool.id ? childByTaskCall.get(tool.id) : undefined
              )
              .filter((delegated): delegated is SessionDetail =>
                Boolean(delegated)
              );
            return (
              <div key={request.requestId} className="relative pb-3">
                <div className="absolute -left-5 top-3 h-3.5 w-3.5 rounded-full border-2 border-white bg-gray-300 ring-1 ring-gray-200" />
                <TraceGridRow
                  className="mb-1.5"
                  left={
                    <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 px-3 text-[11px] tracking-wide text-gray-400">
                      <span className="flex flex-wrap items-center gap-2">
                        <span>{stepLabel}</span>
                        {longestMeasured?.kind === "model" &&
                        longestMeasured.id === request.requestId ? (
                          <span className="rounded-full bg-blue-50 px-2 py-0.5 font-medium text-blue-700">
                            Longest measured span
                          </span>
                        ) : null}
                      </span>
                      <span className="flex flex-wrap items-center justify-end gap-3">
                        {usage ? (
                          <span>
                            {formatCompactNumber(usage.output)} output tokens
                          </span>
                        ) : null}
                        {request.response?.responseTime !== undefined ? (
                          <span>
                            {formatDuration(request.response.responseTime)}{" "}
                            model latency
                          </span>
                        ) : null}
                        <span>
                          {new Date(request.timestamp).toLocaleTimeString()}
                        </span>
                      </span>
                    </div>
                  }
                  right={
                    request.response?.responseTime !== undefined ? (
                      <WaterfallBar
                        range={range}
                        startTimestamp={request.timestamp}
                        durationMs={request.response.responseTime}
                        kind="model"
                        approximate={
                          !timestampHasSubsecondPrecision(request.timestamp)
                        }
                        label={stepLabel}
                      />
                    ) : null
                  }
                />
                <div className="space-y-1">
                  {reasoning ? (
                    <TraceGridRow
                      left={
                        <AssistantTimelineItem
                          kind="reasoning"
                          text={reasoning}
                          request={request}
                          tokenCount={response.reasoningTokens}
                        />
                      }
                    />
                  ) : null}
                  {text ? (
                    <TraceGridRow
                      left={
                        <AssistantTimelineItem
                          kind="output"
                          text={text}
                          request={request}
                          tokenCount={response.contentTokens}
                        />
                      }
                    />
                  ) : null}
                  {toolWindow ? (
                    <TraceGridRow
                      left={
                        <div
                          data-tool-window-request-id={request.requestId}
                          className="flex min-h-9 flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border border-amber-200 bg-amber-50/70 px-3 py-2 text-xs"
                          title="Inferred from model response completion to the next LLM request containing all matching tool results"
                        >
                          <Wrench className="h-3.5 w-3.5 text-amber-700" />
                          <span className="font-medium text-gray-800">
                            {tools.length === 1
                              ? `Tool: ${tools[0].name}`
                              : `Tool batch · ${tools.map((tool) => tool.name).join(", ")}`}
                          </span>
                          <span className="text-gray-500">
                            {formatObservedDuration(toolWindow)}
                          </span>
                          {longestToolWindow?.requestId ===
                          request.requestId ? (
                            <span className="rounded-full bg-amber-100 px-2 py-0.5 font-medium text-amber-800">
                              Longest observed tool window
                            </span>
                          ) : null}
                        </div>
                      }
                      right={
                        toolWindow.complete ? (
                          <WaterfallBar
                            range={range}
                            startTimestamp={toolWindow.startTimestamp}
                            durationMs={toolWindow.durationMs}
                            kind="tool"
                            approximate={toolWindow.approximate}
                            label={`${tools.length === 1 ? `Tool: ${tools[0].name}` : "Tool batch"} · ${formatObservedDuration(toolWindow)}`}
                          />
                        ) : (
                          <div className="pt-2 text-center text-[11px] text-gray-400">
                            Timing unavailable
                          </div>
                        )
                      }
                    />
                  ) : null}
                  {delegatedSpans.map((delegated) => (
                    <TraceGridRow
                      key={`subagent-span-${delegated.summary.sessionId}`}
                      left={
                        <div className="ml-5 flex min-h-8 flex-wrap items-center gap-2 rounded-lg border border-violet-100 bg-violet-50/60 px-3 py-1.5 text-xs text-gray-600">
                          <GitBranch className="h-3.5 w-3.5 text-violet-600" />
                          <span className="font-medium text-gray-800">
                            Subagent:{" "}
                            {delegated.summary.agentName ||
                              delegated.summary.title}
                          </span>
                          <span>
                            {formatDuration(delegated.summary.elapsedTimeMs)}{" "}
                            measured E2E
                          </span>
                          {longestMeasured?.kind === "subagent" &&
                          longestMeasured.id === delegated.summary.sessionId ? (
                            <span className="rounded-full bg-violet-100 px-2 py-0.5 font-medium text-violet-700">
                              Longest measured span
                            </span>
                          ) : null}
                        </div>
                      }
                      right={
                        <WaterfallBar
                          range={range}
                          startTimestamp={delegated.summary.firstTimestamp}
                          durationMs={delegated.summary.elapsedTimeMs}
                          kind="subagent"
                          approximate={
                            !timestampHasSubsecondPrecision(
                              delegated.summary.firstTimestamp
                            )
                          }
                          label={`Subagent: ${delegated.summary.agentName || delegated.summary.title} · ${formatDuration(delegated.summary.elapsedTimeMs)} measured E2E`}
                        />
                      }
                    />
                  ))}
                  {tools.map((tool, toolIndex) => (
                    <TraceGridRow
                      key={tool.id || `${tool.name}-${toolIndex}`}
                      left={
                        <ToolTimelineItem
                          icon={
                            tool.name === "task" ? (
                              <GitBranch className="h-4 w-4" />
                            ) : (
                              <Wrench className="h-4 w-4" />
                            )
                          }
                          call={tool}
                          result={tool.id ? results.get(tool.id) : undefined}
                          request={request}
                          delegatedSession={
                            tool.id ? childByTaskCall.get(tool.id) : undefined
                          }
                          onOpenRequest={onOpenRequest}
                          waterfallRange={range}
                        />
                      }
                    />
                  ))}
                  {request.response?.streamError ? (
                    <TraceGridRow
                      left={
                        <TimelineItem
                          icon={<XCircle className="h-4 w-4 text-red-500" />}
                          title="Response interrupted"
                          description={request.response.streamError}
                          request={request}
                          onOpenRequest={onOpenRequest}
                          tone="error"
                        />
                      }
                    />
                  ) : null}
                  {!reasoning &&
                  !text &&
                  tools.length === 0 &&
                  !request.response?.streamError ? (
                    <TraceGridRow
                      left={
                        <TimelineItem
                          icon={<CheckCircle2 className="h-4 w-4" />}
                          title="Response captured"
                          request={request}
                          onOpenRequest={onOpenRequest}
                        />
                      }
                    />
                  ) : null}
                </div>
              </div>
            );
          })}
        </div>

        {summary.resultMessage ? (
          <div
            className={`mt-2 flex items-start gap-2 rounded-lg border px-3 py-2.5 text-sm ${summary.status === "error" ? "border-red-200 bg-red-50 text-red-700" : "border-emerald-200 bg-emerald-50 text-emerald-700"}`}
          >
            {summary.status === "error" ? (
              <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
            ) : (
              <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" />
            )}
            <span>{summary.resultMessage}</span>
          </div>
        ) : null}
      </div>
    </section>
  );
}

export default function SessionView({
  sessions,
  total,
  selectedSessionId,
  detail,
  isLoadingList,
  isLoadingDetail,
  onSelectSession,
  onOpenRequest,
}: SessionViewProps) {
  return (
    <div
      data-testid="session-view"
      className="min-h-[720px] w-full overflow-hidden rounded-xl border border-gray-200 bg-white shadow-sm lg:h-[calc(100dvh-22rem)] lg:min-h-[560px]"
    >
      <div className="grid min-h-[720px] grid-cols-1 lg:h-full lg:min-h-0 lg:grid-cols-[clamp(330px,20vw,440px)_minmax(0,1fr)]">
        <aside
          data-testid="session-sidebar"
          className="border-b border-gray-200 bg-gray-50/70 lg:flex lg:min-h-0 lg:flex-col lg:border-b-0 lg:border-r"
        >
          <div className="flex h-16 shrink-0 items-center justify-between border-b border-gray-200 px-4">
            <div>
              <h2 className="text-sm font-semibold uppercase tracking-wider text-gray-900">
                Sessions
              </h2>
              <p className="mt-0.5 text-xs text-gray-400">
                {total} captured conversations
              </p>
            </div>
            {isLoadingList ? (
              <Loader2 className="h-4 w-4 animate-spin text-gray-400" />
            ) : null}
          </div>
          <div className="max-h-[654px] overflow-y-auto scrollbar-custom lg:min-h-0 lg:max-h-none lg:flex-1">
            {sessions.length ? (
              sessions.map((session) => (
                <SessionListItem
                  key={session.sessionId}
                  session={session}
                  selected={selectedSessionId === session.sessionId}
                  onSelect={() => onSelectSession(session.sessionId)}
                />
              ))
            ) : (
              <div className="px-6 py-16 text-center text-sm text-gray-400">
                No sessions found
              </div>
            )}
          </div>
        </aside>

        <div
          data-testid="session-detail-pane"
          className="min-w-0 bg-white lg:min-h-0"
        >
          {isLoadingDetail ? (
            <div className="flex min-h-[720px] items-center justify-center text-gray-400 lg:h-full lg:min-h-0">
              <Loader2 className="mr-2 h-5 w-5 animate-spin" /> Loading session…
            </div>
          ) : detail ? (
            <div className="max-h-[720px] overflow-y-auto scrollbar-custom lg:h-full lg:max-h-none">
              <header className="sticky top-0 z-10 border-b border-gray-200 bg-white/95 px-6 py-5 backdrop-blur">
                <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                  <div className="min-w-0">
                    <h1 className="truncate text-xl font-semibold text-gray-900">
                      {detail.summary.title}
                    </h1>
                    <p className="mt-1 text-sm text-gray-500">
                      {new Date(detail.summary.firstTimestamp).toLocaleString()}{" "}
                      ·{" "}
                      {formatCount(
                        detail.summary.children?.length ??
                          detail.children?.length ??
                          0,
                        "subagent"
                      )}
                    </p>
                  </div>
                  <div className="flex items-center gap-2 rounded-lg bg-gray-50 px-3 py-2 text-xs text-gray-500">
                    <Clock3 className="h-4 w-4" />
                    Last activity {relativeDate(detail.summary.lastTimestamp)}
                  </div>
                </div>
              </header>
              <div className="space-y-6 px-6 py-6">
                <SessionTimeline
                  detail={detail}
                  onOpenRequest={onOpenRequest}
                />
              </div>
            </div>
          ) : (
            <div className="flex min-h-[720px] flex-col items-center justify-center text-gray-400 lg:h-full lg:min-h-0">
              <MessageSquareText className="mb-3 h-8 w-8" />
              Select a session to inspect its timeline
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
