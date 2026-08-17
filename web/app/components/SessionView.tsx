/* eslint-disable @typescript-eslint/no-explicit-any -- OpenAI and Anthropic response blocks are runtime JSON. */

import { useState } from 'react';
import {
  AlertCircle,
  Bot,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Clock3,
  FileText,
  GitBranch,
  Loader2,
  MessageSquareText,
  Timer,
  Wrench,
  XCircle,
  Zap,
} from 'lucide-react';
import type { RequestRecord, SessionDetail, SessionSummary } from '../types';
import { getUsage } from '../utils/models';

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

const STATUS_STYLES: Record<string, { label: string; className: string; dot: string }> = {
  completed: { label: 'Completed', className: 'bg-emerald-50 text-emerald-700 border-emerald-200', dot: 'bg-emerald-500' },
  error: { label: 'Error', className: 'bg-red-50 text-red-700 border-red-200', dot: 'bg-red-500' },
  interrupted: { label: 'Interrupted', className: 'bg-red-50 text-red-700 border-red-200', dot: 'bg-red-500' },
  pending: { label: 'Pending', className: 'bg-amber-50 text-amber-700 border-amber-200', dot: 'bg-amber-500' },
  'awaiting-tool': { label: 'Tool call', className: 'bg-blue-50 text-blue-700 border-blue-200', dot: 'bg-blue-500' },
  'awaiting-result': { label: 'No parent result', className: 'bg-amber-50 text-amber-700 border-amber-200', dot: 'bg-amber-500' },
  captured: { label: 'Captured', className: 'bg-gray-50 text-gray-600 border-gray-200', dot: 'bg-gray-400' },
};

function statusStyle(status: string) {
  return STATUS_STYLES[status] ?? STATUS_STYLES.captured;
}

function formatCompactNumber(value: number) {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}m`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}k`;
  return value.toLocaleString();
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
  if (sameDay) return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  return date.toLocaleDateString([], { month: 'short', day: 'numeric' });
}

function stripContextBlocks(value: string) {
  return value
    .replace(/<user_memory_context>[\s\S]*?<\/user_memory_context>/g, '')
    .replace(/<memory_maintenance_job>[\s\S]*?<\/memory_maintenance_job>/g, '')
    .replace(/<lumi_workspace>[\s\S]*?<\/lumi_workspace>/g, '')
    .trim();
}

function initialUserPrompt(requests: RequestRecord[]) {
  for (const request of requests) {
    for (const message of request.body?.messages ?? []) {
      if (message.role !== 'user') continue;
      if (typeof message.content === 'string') return stripContextBlocks(message.content);
      if (Array.isArray(message.content)) {
        const text = message.content
          .map((block: any) => block?.text ?? '')
          .filter(Boolean)
          .join('\n');
        if (text) return stripContextBlocks(text);
      }
    }
  }
  return '';
}

function responseText(request: RequestRecord) {
  const body = request.response?.body;
  const chatContent = body?.choices?.[0]?.message?.content;
  if (typeof chatContent === 'string') return chatContent;

  if (Array.isArray(body?.content)) {
    return body.content
      .filter((item: any) => item?.type === 'text' && typeof item?.text === 'string')
      .map((item: any) => item.text)
      .join('\n');
  }

  if (Array.isArray(body?.output)) {
    return body.output
      .flatMap((item: any) => Array.isArray(item?.content) ? item.content : [])
      .filter((item: any) => ['output_text', 'text'].includes(item?.type) && typeof item?.text === 'string')
      .map((item: any) => item.text)
      .join('\n');
  }
  return '';
}

function parseArguments(value: unknown) {
  if (typeof value !== 'string') return value;
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
      name: item?.function?.name ?? 'tool',
      arguments: parseArguments(item?.function?.arguments),
    });
  }
  for (const item of body?.content ?? []) {
    if (item?.type === 'tool_use') {
      calls.push({ id: item.id, name: item.name ?? 'tool', arguments: item.input });
    }
  }
  for (const item of body?.output ?? []) {
    if (item?.type === 'function_call') {
      calls.push({ id: item.call_id ?? item.id, name: item.name ?? 'tool', arguments: parseArguments(item.arguments) });
    }
  }
  return calls;
}

function toolResults(requests: RequestRecord[]) {
  const results = new Map<string, TimelineToolResult>();
  const save = (id: unknown, content: unknown, isError = false) => {
    if (typeof id === 'string' && id) results.set(id, { content, isError });
  };

  for (const request of requests) {
    for (const message of request.body?.messages ?? []) {
      if (message.role === 'tool') {
        save(message.tool_call_id, message.content);
      }
      if (!Array.isArray(message.content)) continue;
      for (const block of message.content) {
        if (block?.type === 'tool_result') {
          save(block.tool_use_id, block.content, Boolean(block.is_error));
        }
      }
    }

    const input = request.body?.input;
    if (Array.isArray(input)) {
      for (const item of input) {
        if (item?.type === 'function_call_output') {
          save(item.call_id ?? item.id, item.output, Boolean(item.is_error));
        }
      }
    }
  }
  return results;
}

function formatPayload(value: unknown) {
  if (value === undefined || value === null || value === '') return 'No output captured';
  if (typeof value === 'string') {
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
  if (!args || typeof args !== 'object') return typeof args === 'string' ? args : '';
  const record = args as Record<string, unknown>;
  if (call.name === 'task') {
    return String(record.description ?? record.subagent_type ?? 'Subagent task');
  }
  for (const key of ['description', 'command', 'filePath', 'path', 'query', 'name']) {
    if (typeof record[key] === 'string' && record[key]) return String(record[key]);
  }
  const keys = Object.keys(record);
  return keys.length ? keys.slice(0, 3).join(', ') : '';
}

function modelLabel(model?: string) {
  if (!model) return 'Model';
  if (model.toLowerCase().includes('deepseek')) return 'DeepSeek';
  if (model.toLowerCase().includes('atlas')) return 'iFinD Atlas';
  return model;
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
        selected ? 'bg-white shadow-[inset_3px_0_0_#2563eb]' : 'bg-gray-50/50 hover:bg-white'
      }`}
      aria-current={selected ? 'page' : undefined}
    >
      <div className="flex items-start gap-3">
        <div className={`mt-1 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg ${session.purpose ? 'bg-violet-100 text-violet-700' : 'bg-gray-100 text-gray-600'}`}>
          {session.children?.length ? <GitBranch className="h-4 w-4" /> : <MessageSquareText className="h-4 w-4" />}
        </div>
        <div className="min-w-0 flex-1">
          <div className="line-clamp-2 text-sm font-medium leading-5 text-gray-900">{session.title}</div>
          <div className="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-gray-500">
            <span>{session.requestCount} requests</span>
            {session.children?.length ? <span>{session.children.length} subagents</span> : null}
            <span>{relativeDate(session.lastTimestamp)}</span>
          </div>
          <div className="mt-2 flex items-center justify-between gap-2">
            <span className="truncate font-mono text-[11px] text-gray-400">{session.sessionId}</span>
            <span className={`h-2 w-2 shrink-0 rounded-full ${style.dot}`} title={style.label} />
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
  tone = 'default',
}: {
  icon: React.ReactNode;
  title: string;
  description?: string;
  request: RequestRecord;
  onOpenRequest: (request: RequestRecord) => void;
  tone?: 'default' | 'error' | 'task';
}) {
  const usage = getUsage(request.response?.body);
  const toneClasses = tone === 'error'
    ? 'border-red-200 bg-red-50/60 hover:bg-red-50'
    : tone === 'task'
      ? 'border-blue-200 bg-blue-50/50 hover:bg-blue-50'
      : 'border-transparent hover:border-gray-200 hover:bg-gray-50';

  return (
    <button
      type="button"
      onClick={() => onOpenRequest(request)}
      data-request-id={request.requestId}
      className={`group flex w-full items-center gap-3 rounded-lg border px-3 py-2.5 text-left transition ${toneClasses}`}
    >
      <span className="flex h-7 w-7 shrink-0 items-center justify-center text-gray-500">{icon}</span>
      <span className="min-w-0 flex-1">
        <span className="flex min-w-0 items-baseline gap-2">
          <span className="shrink-0 text-sm font-medium text-gray-900">{title}</span>
          {description ? <span className="truncate text-sm text-gray-500">— {description}</span> : null}
        </span>
      </span>
      <span className="hidden shrink-0 items-center gap-3 text-xs text-gray-400 sm:flex">
        {usage ? <span>~{formatCompactNumber(usage.output)} tokens</span> : null}
        {request.response?.responseTime !== undefined ? <span>{formatDuration(request.response.responseTime)}</span> : null}
        <span className={`h-2 w-2 rounded-full ${request.response?.streamError ? 'bg-red-500' : 'bg-emerald-500'}`} />
        <ChevronRight className="h-4 w-4 transition group-hover:translate-x-0.5" />
      </span>
    </button>
  );
}

function OutputTimelineItem({
  text,
  request,
}: {
  text: string;
  request: RequestRecord;
}) {
  const [expanded, setExpanded] = useState(false);
  const usage = getUsage(request.response?.body);

  return (
    <div
      data-output-request-id={request.requestId}
      data-expanded={expanded ? 'true' : 'false'}
      className={`overflow-hidden rounded-lg border transition ${
        expanded ? 'border-gray-200 bg-white shadow-sm' : 'border-transparent hover:border-gray-200 hover:bg-gray-50'
      }`}
    >
      <button
        type="button"
        onClick={() => setExpanded(current => !current)}
        aria-expanded={expanded}
        className="group flex w-full items-center gap-3 px-3 py-2.5 text-left"
      >
        <span className="flex h-7 w-7 shrink-0 items-center justify-center text-gray-500">
          <MessageSquareText className="h-4 w-4" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex min-w-0 items-baseline gap-2">
            <span className="shrink-0 text-sm font-medium text-gray-900">Output</span>
            <span className="truncate text-sm text-gray-500">— {text.replace(/\s+/g, ' ').slice(0, 160)}</span>
          </span>
        </span>
        <span className="hidden shrink-0 items-center gap-3 text-xs text-gray-400 sm:flex">
          {usage ? <span>~{formatCompactNumber(usage.output)} tokens</span> : null}
          {request.response?.responseTime !== undefined ? <span>{formatDuration(request.response.responseTime)}</span> : null}
          <span className={`h-2 w-2 rounded-full ${request.response?.streamError ? 'bg-red-500' : 'bg-emerald-500'}`} />
          <ChevronDown className={`h-4 w-4 transition-transform ${expanded ? 'rotate-180' : ''}`} />
        </span>
      </button>

      {expanded ? (
        <div className="border-t border-gray-200 px-4 pb-4 pt-3 sm:ml-11 sm:mr-3">
          <div className="mb-2 text-xs font-medium uppercase tracking-wide text-gray-500">Model output</div>
          <div className="max-h-[32rem] overflow-auto whitespace-pre-wrap break-words rounded-lg border border-gray-200 bg-gray-50 p-4 text-sm leading-6 text-gray-700 scrollbar-custom">
            {text}
          </div>
          {request.response?.responseTime !== undefined ? (
            <div className="mt-2 text-xs text-gray-400">Duration: {formatDuration(request.response.responseTime)}</div>
          ) : null}
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
}: {
  icon: React.ReactNode;
  call: TimelineToolCall;
  result?: TimelineToolResult;
  request: RequestRecord;
  delegatedSession?: SessionDetail;
  onOpenRequest: (request: RequestRecord) => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const [outputExpanded, setOutputExpanded] = useState(false);
  const usage = getUsage(request.response?.body);
  const task = call.name === 'task';
  const delegatedStatus = delegatedSession?.summary.status;
  const resultDot = delegatedStatus === 'error' || delegatedStatus === 'interrupted' || result?.isError
    ? 'bg-red-500'
    : delegatedStatus === 'completed' || result
      ? 'bg-emerald-500'
      : delegatedSession
        ? 'bg-amber-400'
        : 'bg-gray-300';

  return (
    <div
      data-tool-call-id={call.id || `${request.requestId}:${call.name}`}
      data-expanded={expanded ? 'true' : 'false'}
      className={`overflow-hidden rounded-lg border transition ${
        expanded
          ? task ? 'border-blue-200 bg-blue-50/30' : 'border-gray-200 bg-white shadow-sm'
          : task ? 'border-blue-200 bg-blue-50/50 hover:bg-blue-50' : 'border-transparent hover:border-gray-200 hover:bg-gray-50'
      }`}
    >
      <button
        type="button"
        onClick={() => setExpanded(current => !current)}
        aria-expanded={expanded}
        className="group flex w-full items-center gap-3 px-3 py-2.5 text-left"
      >
        <span className="flex h-7 w-7 shrink-0 items-center justify-center text-gray-500">{icon}</span>
        <span className="min-w-0 flex-1">
          <span className="flex min-w-0 items-baseline gap-2">
            <span className="shrink-0 text-sm font-medium text-gray-900">{call.name}</span>
            <span className="truncate text-sm text-gray-500">— {summarizeArguments(call)}</span>
          </span>
        </span>
        <span className="hidden shrink-0 items-center gap-3 text-xs text-gray-400 sm:flex">
          {usage ? <span>~{formatCompactNumber(usage.output)} tokens</span> : null}
          {request.response?.responseTime !== undefined ? <span>{formatDuration(request.response.responseTime)}</span> : null}
          <span className={`h-2 w-2 rounded-full ${resultDot}`} />
          <ChevronDown className={`h-4 w-4 transition-transform ${expanded ? 'rotate-180' : ''}`} />
        </span>
      </button>

      {expanded ? (
        <div className="border-t border-gray-200 px-4 pb-4 pt-3 sm:ml-11 sm:mr-3">
          <div className="mb-2 text-xs font-medium uppercase tracking-wide text-gray-500">Input</div>
          <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-words rounded-lg border border-gray-200 bg-gray-50 p-4 font-mono text-xs leading-5 text-gray-700 scrollbar-custom">
            {formatPayload(call.arguments)}
          </pre>

          <div className="mt-3 overflow-hidden rounded-lg border border-gray-200 bg-white">
            <button
              type="button"
              onClick={() => setOutputExpanded(current => !current)}
              aria-expanded={outputExpanded}
              className="flex w-full items-center gap-2 px-3 py-2.5 text-left text-sm text-gray-600 hover:bg-gray-50"
            >
              <ChevronRight className={`h-4 w-4 transition-transform ${outputExpanded ? 'rotate-90' : ''}`} />
              <span className="font-medium">Output</span>
              <span className={`h-2 w-2 rounded-full ${resultDot}`} />
              {!result ? <span className="text-xs text-gray-400">not captured</span> : null}
            </button>
            {outputExpanded ? (
              <pre className={`max-h-80 overflow-auto whitespace-pre-wrap break-words border-t p-4 font-mono text-xs leading-5 scrollbar-custom ${
                result?.isError ? 'border-red-100 bg-red-50 text-red-700' : 'border-gray-200 bg-gray-50 text-gray-700'
              }`}>
                {formatPayload(result?.content)}
              </pre>
            ) : null}
          </div>

          {request.response?.responseTime !== undefined ? (
            <div className="mt-2 text-xs text-gray-400">Duration: {formatDuration(request.response.responseTime)}</div>
          ) : null}

          {task && delegatedSession ? (
            <div className="mt-4">
              <div className="mb-2 text-xs font-medium uppercase tracking-wide text-gray-500">Delegated subagent</div>
              <SessionTimeline detail={delegatedSession} onOpenRequest={onOpenRequest} child />
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
}: {
  detail: SessionDetail;
  onOpenRequest: (request: RequestRecord) => void;
  child?: boolean;
}) {
  const prompt = initialUserPrompt(detail.requests);
  const summary = detail.summary;
  const style = statusStyle(summary.status);
  const totalTokens = summary.inputTokens + summary.outputTokens;
  const results = toolResults(detail.requests);
  const childByTaskCall = new Map(
    (detail.children ?? [])
      .filter(childDetail => Boolean(childDetail.summary.taskCallId))
      .map(childDetail => [childDetail.summary.taskCallId as string, childDetail]),
  );

  return (
    <section
      data-session-timeline-id={summary.sessionId}
      className={child ? 'rounded-xl border border-gray-200 bg-white' : ''}
    >
      <div className={`flex flex-col gap-3 ${child ? 'border-b border-gray-200 px-5 py-4' : 'pb-5'} sm:flex-row sm:items-center sm:justify-between`}>
        <div className="flex min-w-0 items-center gap-3">
          <div className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-lg ${child ? 'bg-blue-50 text-blue-600' : 'bg-gray-900 text-white'}`}>
            <Bot className="h-5 w-5" />
          </div>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h3 className="truncate text-base font-semibold text-gray-900">
                {child ? summary.agentName || 'Subagent' : modelLabel(summary.model)}
              </h3>
              {child ? <span className="text-sm text-gray-500">{summary.taskDescription}</span> : null}
              <span className={`rounded-full border px-2 py-0.5 text-[11px] font-medium ${style.className}`}>{style.label}</span>
            </div>
            <p className="mt-0.5 truncate font-mono text-xs text-gray-400">{summary.sessionId}</p>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-3 text-xs text-gray-500">
          <span>{summary.toolCallCount} tool calls</span>
          <span>{summary.requestCount} model steps</span>
          <span className="inline-flex items-center gap-1"><Zap className="h-3.5 w-3.5" />{formatCompactNumber(totalTokens)} tokens</span>
          <span className="inline-flex items-center gap-1"><Timer className="h-3.5 w-3.5" />{formatDuration(summary.responseTimeMs)}</span>
        </div>
      </div>

      <div className={child ? 'px-5 py-5' : ''}>
        {prompt ? (
          <div className="mb-4 rounded-xl border border-gray-200 bg-stone-50 px-4 py-3">
            <div className="mb-1 flex items-center gap-2 text-xs font-medium uppercase tracking-wide text-gray-500">
              <FileText className="h-3.5 w-3.5" />
              {child ? 'Task prompt' : 'User request'}
            </div>
            <p className="line-clamp-2 whitespace-pre-wrap text-sm leading-6 text-gray-700">{prompt}</p>
          </div>
        ) : null}

        <div className="relative space-y-1 pl-5 before:absolute before:bottom-2 before:left-[7px] before:top-2 before:w-px before:bg-gray-200">
          {detail.requests.map((rawRequest, index) => {
            const request = { ...rawRequest, id: rawRequest.id || rawRequest.requestId };
            const text = responseText(request);
            const tools = responseToolCalls(request);
            return (
              <div key={request.requestId} className="relative pb-3">
                <div className="absolute -left-5 top-3 h-3.5 w-3.5 rounded-full border-2 border-white bg-gray-300 ring-1 ring-gray-200" />
                <div className="mb-1.5 flex items-center justify-between px-3 text-[11px] uppercase tracking-wide text-gray-400">
                  <span>Step {index + 1} · {modelLabel(request.routedModel || request.body?.model)}</span>
                  <span>{new Date(request.timestamp).toLocaleTimeString()}</span>
                </div>
                <div className="space-y-1">
                  {text ? (
                    <OutputTimelineItem
                      text={text}
                      request={request}
                    />
                  ) : null}
                  {tools.map((tool, toolIndex) => (
                    <ToolTimelineItem
                      key={tool.id || `${tool.name}-${toolIndex}`}
                      icon={tool.name === 'task' ? <GitBranch className="h-4 w-4" /> : <Wrench className="h-4 w-4" />}
                      call={tool}
                      result={tool.id ? results.get(tool.id) : undefined}
                      request={request}
                      delegatedSession={tool.id ? childByTaskCall.get(tool.id) : undefined}
                      onOpenRequest={onOpenRequest}
                    />
                  ))}
                  {request.response?.streamError ? (
                    <TimelineItem
                      icon={<XCircle className="h-4 w-4 text-red-500" />}
                      title="Response interrupted"
                      description={request.response.streamError}
                      request={request}
                      onOpenRequest={onOpenRequest}
                      tone="error"
                    />
                  ) : null}
                  {!text && tools.length === 0 && !request.response?.streamError ? (
                    <TimelineItem
                      icon={<CheckCircle2 className="h-4 w-4" />}
                      title="Response captured"
                      request={request}
                      onOpenRequest={onOpenRequest}
                    />
                  ) : null}
                </div>
              </div>
            );
          })}
        </div>

        {summary.resultMessage ? (
          <div className={`mt-2 flex items-start gap-2 rounded-lg border px-3 py-2.5 text-sm ${summary.status === 'error' ? 'border-red-200 bg-red-50 text-red-700' : 'border-emerald-200 bg-emerald-50 text-emerald-700'}`}>
            {summary.status === 'error' ? <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" /> : <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" />}
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
        <aside data-testid="session-sidebar" className="border-b border-gray-200 bg-gray-50/70 lg:flex lg:min-h-0 lg:flex-col lg:border-b-0 lg:border-r">
          <div className="flex h-16 shrink-0 items-center justify-between border-b border-gray-200 px-4">
            <div>
              <h2 className="text-sm font-semibold uppercase tracking-wider text-gray-900">Sessions</h2>
              <p className="mt-0.5 text-xs text-gray-400">{total} captured conversations</p>
            </div>
            {isLoadingList ? <Loader2 className="h-4 w-4 animate-spin text-gray-400" /> : null}
          </div>
          <div className="max-h-[654px] overflow-y-auto scrollbar-custom lg:min-h-0 lg:max-h-none lg:flex-1">
            {sessions.length ? sessions.map(session => (
              <SessionListItem
                key={session.sessionId}
                session={session}
                selected={selectedSessionId === session.sessionId}
                onSelect={() => onSelectSession(session.sessionId)}
              />
            )) : (
              <div className="px-6 py-16 text-center text-sm text-gray-400">No sessions found</div>
            )}
          </div>
        </aside>

        <div data-testid="session-detail-pane" className="min-w-0 bg-white lg:min-h-0">
          {isLoadingDetail ? (
            <div className="flex min-h-[720px] items-center justify-center text-gray-400 lg:h-full lg:min-h-0">
              <Loader2 className="mr-2 h-5 w-5 animate-spin" /> Loading session…
            </div>
          ) : detail ? (
            <div className="max-h-[720px] overflow-y-auto scrollbar-custom lg:h-full lg:max-h-none">
              <header className="sticky top-0 z-10 border-b border-gray-200 bg-white/95 px-6 py-5 backdrop-blur">
                <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                  <div className="min-w-0">
                    <h1 className="truncate text-xl font-semibold text-gray-900">{detail.summary.title}</h1>
                    <p className="mt-1 text-sm text-gray-500">
                      {new Date(detail.summary.firstTimestamp).toLocaleString()} · {detail.summary.children?.length ?? detail.children?.length ?? 0} subagents
                    </p>
                  </div>
                  <div className="flex items-center gap-2 rounded-lg bg-gray-50 px-3 py-2 text-xs text-gray-500">
                    <Clock3 className="h-4 w-4" />
                    Last activity {relativeDate(detail.summary.lastTimestamp)}
                  </div>
                </div>
              </header>
              <div className="space-y-6 px-6 py-6">
                <SessionTimeline detail={detail} onOpenRequest={onOpenRequest} />
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
