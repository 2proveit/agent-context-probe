import { useEffect, useState, type ReactNode } from 'react';
import { 
  ChevronDown, 
  Info, 
  Settings, 
  Cpu, 
  MessageCircle, 
  Brain, 
  User, 
  Bot, 
  Target,
  Copy,
  Check,
  ArrowLeftRight,
  Activity,
  Clock,
  Wifi,
  Calendar,
  List,
  FileText,
  Wrench
} from 'lucide-react';
import { MessageContent } from './MessageContent';
import { formatJSON, formatJSONForCopy, limitDisplayText, normalizeDisplayLimit } from '../utils/formatters';
import {
  getAPIProtocol,
  getProviderName,
  getProtocolBadgeClasses,
  getUsage,
} from '../utils/models';
import type { RequestRecord } from '../types';

interface RequestDetailContentProps {
  request: RequestRecord;
  onGrade: () => void;
}

const DETAIL_SECTION_CLASS = 'overflow-hidden rounded-lg border border-gray-200 bg-white shadow-[0_1px_2px_rgba(15,23,42,0.04)]';
const DETAIL_SECTION_HEADER_CLASS = 'cursor-pointer border-b border-gray-200 bg-gray-50/70 px-4 py-3 transition-colors hover:bg-gray-100/70';
const DETAIL_SECTION_TITLE_CLASS = 'flex items-center gap-2 text-sm font-semibold text-gray-900';
const DETAIL_SECTION_ICON_CLASS = 'h-3.5 w-3.5';
const DETAIL_SECTION_CHEVRON_CLASS = 'h-4 w-4 text-gray-400 transition-transform';
const DETAIL_SECTION_BODY_CLASS = 'p-4';
const DETAIL_COPY_BUTTON_CLASS = 'inline-flex items-center gap-1.5 rounded-md border border-gray-200 bg-white px-2.5 py-1.5 text-xs font-medium text-gray-600 transition-colors hover:border-blue-200 hover:bg-blue-50 hover:text-blue-700';

function DetailSectionIcon({
  name,
  className,
  children,
}: {
  name: string;
  className: string;
  children: ReactNode;
}) {
  return (
    <span
      data-section-icon={name}
      className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-md border ${className}`}
    >
      {children}
    </span>
  );
}

export default function RequestDetailContent({ request, onGrade }: RequestDetailContentProps) {
  const [expandedSections, setExpandedSections] = useState<Record<string, boolean>>({
    overview: true,
    conversation: true,
  });
  const [copied, setCopied] = useState<Record<string, boolean>>({});
  const [showRawStreamEvents, setShowRawStreamEvents] = useState(false);
  const [rawRequestMaxDisplayChars, setRawRequestMaxDisplayChars] = useState(0);
  const [rawResponseMaxDisplayChars, setRawResponseMaxDisplayChars] = useState(0);

  useEffect(() => {
    fetch('/api/ui-config')
      .then(response => response.ok ? response.json() : null)
      .then(config => {
        setShowRawStreamEvents(Boolean(config?.showRawStreamEvents));
        setRawRequestMaxDisplayChars(normalizeDisplayLimit(config?.rawRequestMaxDisplayChars));
        setRawResponseMaxDisplayChars(normalizeDisplayLimit(config?.rawResponseMaxDisplayChars));
      })
      .catch(() => {
        setShowRawStreamEvents(false);
        setRawRequestMaxDisplayChars(0);
        setRawResponseMaxDisplayChars(0);
      });
  }, []);

  const protocol = getAPIProtocol(request.endpoint);
  const conversationItems = normalizeRequestMessages(request.body);

  const toggleSection = (section: string) => {
    setExpandedSections(prev => ({
      ...prev,
      [section]: !prev[section]
    }));
  };

  const handleCopy = async (content: string, key: string) => {
    try {
      await navigator.clipboard.writeText(content);
      setCopied(prev => ({ ...prev, [key]: true }));
      setTimeout(() => {
        setCopied(prev => ({ ...prev, [key]: false }));
      }, 2000);
    } catch (error) {
      console.error('Failed to copy to clipboard:', error);
    }
  };

  const getMethodColor = (method: string) => {
    const colors = {
      'GET': 'bg-green-50 text-green-700 border border-green-200',
      'POST': 'bg-blue-50 text-blue-700 border border-blue-200',
      'PUT': 'bg-yellow-50 text-yellow-700 border border-yellow-200',
      'DELETE': 'bg-red-50 text-red-700 border border-red-200'
    };
    return colors[method as keyof typeof colors] || 'bg-gray-50 text-gray-700 border border-gray-200';
  };

  const canGradeRequest = (request: RequestRecord) => {
    return request.body && 
           request.body.messages && 
           request.body.messages.some(msg => msg.role === 'user') &&
           request.endpoint.includes('/messages');
  };

  return (
    <div className="space-y-3">
      {/* Request Overview */}
      <div className="rounded-lg border border-gray-200 bg-white p-4 shadow-[0_1px_2px_rgba(15,23,42,0.04)]">
        <div className="mb-4 flex items-center justify-between">
          <h4 className={DETAIL_SECTION_TITLE_CLASS}>
            <DetailSectionIcon name="overview" className="border-sky-100 bg-sky-50 text-sky-600">
              <Info className={DETAIL_SECTION_ICON_CLASS} />
            </DetailSectionIcon>
            <span>Request Overview</span>
          </h4>
          {/* {!request.promptGrade && canGradeRequest(request) && (
            <button 
              onClick={onGrade}
              className="bg-purple-600 hover:bg-purple-700 text-white px-4 py-2 rounded-lg font-medium transition-colors flex items-center space-x-2"
            >
              <Target className="w-4 h-4" />
              <span>Grade This Prompt</span>
            </button>
          )} */}
        </div>
        <div className="grid grid-cols-1 gap-x-8 gap-y-3 text-sm lg:grid-cols-2">
          <div className="space-y-3">
            <div className="flex flex-wrap items-center gap-3">
              <span className="min-w-[72px] text-xs font-medium uppercase tracking-wide text-gray-500">Method</span>
              <span className={`rounded-md px-2 py-1 text-xs font-semibold uppercase tracking-wide ${getMethodColor(request.method)}`}>
                {request.method}
              </span>
            </div>
            <div className="flex min-w-0 flex-wrap items-center gap-3">
              <span className="min-w-[72px] text-xs font-medium uppercase tracking-wide text-gray-500">Endpoint</span>
              <code className="max-w-full break-all rounded border border-gray-200 bg-gray-50 px-2 py-1 font-mono text-xs text-gray-700">
                {request.endpoint}
              </code>
              <span className={`rounded-full px-2 py-1 text-xs font-semibold ${getProtocolBadgeClasses(request.endpoint)}`}>
                {protocol}
              </span>
            </div>
          </div>
          <div className="space-y-3">
            <div className="flex flex-wrap items-center gap-3">
              <span className="min-w-[72px] text-xs font-medium uppercase tracking-wide text-gray-500">Timestamp</span>
              <span className="text-gray-900">{new Date(request.timestamp).toLocaleString()}</span>
            </div>
            <div className="flex min-w-0 flex-wrap items-start gap-3">
              <span className="min-w-[72px] text-xs font-medium uppercase tracking-wide text-gray-500">User Agent</span>
              <span className="min-w-0 flex-1 break-words text-xs leading-5 text-gray-600">{request.headers['User-Agent']?.[0] || 'N/A'}</span>
            </div>
          </div>
        </div>
      </div>

      {/* Headers */}
      <div className={DETAIL_SECTION_CLASS}>
        <div 
          className={DETAIL_SECTION_HEADER_CLASS}
          onClick={() => toggleSection('headers')}
        >
          <div className="flex items-center justify-between">
            <h4 className={DETAIL_SECTION_TITLE_CLASS}>
              <DetailSectionIcon name="headers" className="border-blue-100 bg-blue-50 text-blue-600">
                <Settings className={DETAIL_SECTION_ICON_CLASS} />
              </DetailSectionIcon>
              <span>Request Headers</span>
            </h4>
            <ChevronDown className={`${DETAIL_SECTION_CHEVRON_CLASS} ${
              expandedSections.headers ? 'rotate-180' : ''
            }`} />
          </div>
        </div>
        {expandedSections.headers && (
          <div className={DETAIL_SECTION_BODY_CLASS}>
            <div className="rounded-lg border border-gray-200 bg-gray-50 p-4">
              <div className="flex items-center justify-between mb-2">
                <span className="text-sm font-medium text-gray-700">Headers</span>
                <button
                  onClick={() => handleCopy(formatJSON(request.headers), 'headers')}
                  className="p-1 text-gray-500 hover:text-gray-700 transition-colors"
                  title="Copy headers"
                >
                  {copied.headers ? (
                    <Check className="w-4 h-4 text-green-600" />
                  ) : (
                    <Copy className="w-4 h-4" />
                  )}
                </button>
              </div>
              <pre className="text-sm text-gray-700 overflow-x-auto">
                {formatJSON(request.headers)}
              </pre>
            </div>
          </div>
        )}
      </div>

      {request.body && (
        <>
          {/* System Messages */}
          {(request.body.system || request.body.instructions) && (
            <div className={DETAIL_SECTION_CLASS}>
              <div 
                className={DETAIL_SECTION_HEADER_CLASS}
                onClick={() => toggleSection('system')}
              >
                <div className="flex items-center justify-between">
                  <h4 className={DETAIL_SECTION_TITLE_CLASS}>
                    <DetailSectionIcon name="system" className="border-amber-100 bg-amber-50 text-amber-600">
                      <Cpu className={DETAIL_SECTION_ICON_CLASS} />
                    </DetailSectionIcon>
                    <span>System Instructions</span>
                    <span className="rounded-full border border-amber-200 bg-amber-50 px-2 py-0.5 text-[11px] font-medium text-amber-700">
                      {request.body.system?.length || 1} items
                    </span>
                  </h4>
                  <ChevronDown className={`${DETAIL_SECTION_CHEVRON_CLASS} ${
                    expandedSections.system ? 'rotate-180' : ''
                  }`} />
                </div>
              </div>
              {expandedSections.system && (
                <div className="space-y-3 p-4">
                  {(request.body.system || [{ text: request.body.instructions, type: 'instructions' }]).map((sys: any, index: number) => (
                    <div key={index} className="rounded-lg border border-amber-200 bg-amber-50/40 p-4">
                      <div className="mb-3 flex items-center justify-between">
                        <span className="text-sm font-medium text-gray-800">System Message #{index + 1}</span>
                        {sys.cache_control && (
                          <span className="text-xs bg-orange-100 text-orange-700 px-2 py-1 rounded-full border border-orange-200">
                            Cache: {sys.cache_control.type}
                          </span>
                        )}
                      </div>
                      <div className="bg-white rounded p-3 border border-gray-200">
                        <MessageContent content={{ type: 'text', text: sys.text }} />
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}

          {/* Tools */}
          {request.body.tools && request.body.tools.length > 0 && (
            <div className={DETAIL_SECTION_CLASS}>
              <div 
                className={DETAIL_SECTION_HEADER_CLASS}
                onClick={() => toggleSection('tools')}
              >
                <div className="flex items-center justify-between">
                  <h4 className={DETAIL_SECTION_TITLE_CLASS}>
                    <DetailSectionIcon name="tools" className="border-orange-100 bg-orange-50 text-orange-600">
                      <Wrench className={DETAIL_SECTION_ICON_CLASS} />
                    </DetailSectionIcon>
                    <span>Available Tools</span>
                    <span className="rounded-full border border-gray-200 bg-white px-2 py-0.5 text-[11px] font-medium text-gray-600">
                      {request.body.tools.length} tools
                    </span>
                  </h4>
                  <ChevronDown className={`${DETAIL_SECTION_CHEVRON_CLASS} ${
                    expandedSections.tools ? 'rotate-180' : ''
                  }`} />
                </div>
              </div>
              {expandedSections.tools && (
                <div className="space-y-3 p-4">
                  {request.body.tools.map((tool, index) => (
                    <ToolCard key={index} tool={tool} index={index} />
                  ))}
                </div>
              )}
            </div>
          )}

          {/* Conversation */}
          {conversationItems.length > 0 && (
            <div className={DETAIL_SECTION_CLASS} data-testid="conversation-section">
              <div 
                className={DETAIL_SECTION_HEADER_CLASS}
                onClick={() => toggleSection('conversation')}
                role="button"
                tabIndex={0}
                aria-expanded={Boolean(expandedSections.conversation)}
                data-testid="conversation-toggle"
                onKeyDown={(event) => {
                  if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault();
                    toggleSection('conversation');
                  }
                }}
              >
                <div className="flex items-center justify-between">
                  <h4 className={DETAIL_SECTION_TITLE_CLASS}>
                    <DetailSectionIcon name="conversation" className="border-emerald-100 bg-emerald-50 text-emerald-600">
                      <MessageCircle className={DETAIL_SECTION_ICON_CLASS} />
                    </DetailSectionIcon>
                    <span>Conversation</span>
                    <span className="rounded-full border border-gray-200 bg-white px-2 py-0.5 text-[11px] font-medium text-gray-600">
                      {conversationItems.length} items
                    </span>
                  </h4>
                  <ChevronDown className={`${DETAIL_SECTION_CHEVRON_CLASS} ${
                    expandedSections.conversation ? 'rotate-180' : ''
                  }`} />
                </div>
              </div>
              <div className={`grid transition-[grid-template-rows] duration-200 ease-out ${
                expandedSections.conversation ? 'grid-rows-[1fr]' : 'grid-rows-[0fr]'
              }`}>
                <div className="min-h-0 overflow-hidden">
                  <div className="max-h-[min(52dvh,560px)] space-y-3 overflow-y-auto p-4 scrollbar-custom">
                    {conversationItems.map((message, index) => (
                      <MessageBubble key={index} message={message} index={index} />
                    ))}
                  </div>
                </div>
              </div>
            </div>
          )}

          {/* Model Configuration */}
          <div className={DETAIL_SECTION_CLASS}>
            <div 
              className={DETAIL_SECTION_HEADER_CLASS}
              onClick={() => toggleSection('model')}
            >
              <div className="flex items-center justify-between">
                <h4 className={DETAIL_SECTION_TITLE_CLASS}>
                  <DetailSectionIcon name="model" className="border-violet-100 bg-violet-50 text-violet-600">
                    <Brain className={DETAIL_SECTION_ICON_CLASS} />
                  </DetailSectionIcon>
                  <span>Model Configuration</span>
                </h4>
                <ChevronDown className={`${DETAIL_SECTION_CHEVRON_CLASS} ${
                  expandedSections.model ? 'rotate-180' : ''
                }`} />
              </div>
            </div>
            {expandedSections.model && (
              <div className="space-y-4 p-4">
                {/* Model Routing Information */}
                {request.routedModel && request.routedModel !== request.originalModel && (
                  <div className="rounded-lg border border-blue-200 bg-blue-50/40 p-4">
                    <div className="flex items-center space-x-4">
                      <div className="flex-1">
                        <div className="flex items-center space-x-2 mb-2">
                          <span className="text-sm font-semibold text-purple-700">Requested Model</span>
                          <code className="text-xs bg-white px-2 py-1 rounded font-mono border border-purple-200">
                            {request.originalModel || request.body.model}
                          </code>
                        </div>
                        <div className="flex items-center space-x-3">
                          <div className="flex items-center space-x-2">
                            <ArrowLeftRight className="w-4 h-4 text-purple-600" />
                            <span className="text-xs text-purple-600 font-medium">Routed to</span>
                          </div>
                          <code className="text-sm bg-white px-3 py-1.5 rounded font-mono font-semibold border border-blue-200 text-blue-700">
                            {request.routedModel}
                          </code>
                          <span className="text-xs bg-blue-100 text-blue-700 px-2 py-1 rounded-full border border-blue-200">
                            {getProviderName(request.routedModel, request.endpoint)}
                          </span>
                        </div>
                      </div>
                      <div className="text-right">
                        <div className="text-xs text-gray-500 mb-1">Target Endpoint</div>
                        <code className="text-xs bg-white px-2 py-1 rounded font-mono border border-gray-200">
                          {request.endpoint}
                        </code>
                      </div>
                    </div>
                  </div>
                )}

                {/* Model Parameters */}
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  {!request.routedModel || request.routedModel === request.originalModel ? (
                    <div className="bg-gray-50 border border-gray-200 rounded-lg p-3">
                      <div className="text-xs text-gray-500 mb-1">Model</div>
                      <div className="text-sm font-medium text-gray-900">{request.originalModel || request.body.model || 'N/A'}</div>
                    </div>
                  ) : null}
                  <div className="bg-gray-50 border border-gray-200 rounded-lg p-3">
                    <div className="text-xs text-gray-500 mb-1">Max Tokens</div>
                    <div className="text-sm font-medium text-gray-900">
                      {(request.body.max_tokens ?? request.body.max_output_tokens)?.toLocaleString() || 'N/A'}
                    </div>
                  </div>
                  <div className="bg-gray-50 border border-gray-200 rounded-lg p-3">
                    <div className="text-xs text-gray-500 mb-1">Temperature</div>
                    <div className="text-sm font-medium text-gray-900">{request.body.temperature ?? 'N/A'}</div>
                  </div>
                  <div className="bg-gray-50 border border-gray-200 rounded-lg p-3">
                    <div className="text-xs text-gray-500 mb-1">Stream</div>
                    <div className="text-sm font-medium text-gray-900">
                      {request.body.stream ? '✅ Yes' : '❌ No'}
                    </div>
                  </div>
                </div>
              </div>
            )}
          </div>

          <div className={DETAIL_SECTION_CLASS}>
            <div
              className={DETAIL_SECTION_HEADER_CLASS}
              onClick={() => toggleSection('rawRequest')}
            >
              <div className="flex items-center justify-between">
                <h4 className={DETAIL_SECTION_TITLE_CLASS}>
                  <DetailSectionIcon name="raw-request" className="border-slate-200 bg-slate-50 text-slate-600">
                    <FileText className={DETAIL_SECTION_ICON_CLASS} />
                  </DetailSectionIcon>
                  <span>Raw Request JSON</span>
                </h4>
                <div className="flex items-center space-x-2">
                  <button
                    type="button"
                    onClick={(event) => {
                      event.stopPropagation();
                      handleCopy(formatJSONForCopy(request.body), 'rawRequest');
                    }}
                    className={DETAIL_COPY_BUTTON_CLASS}
                    title="Copy raw request JSON"
                    aria-label="Copy raw request JSON"
                  >
                    {copied.rawRequest ? (
                      <Check className="w-4 h-4 text-green-600" />
                    ) : (
                      <Copy className="w-4 h-4" />
                    )}
                    <span>{copied.rawRequest ? 'Copied' : 'Copy'}</span>
                  </button>
                  <ChevronDown className={`${DETAIL_SECTION_CHEVRON_CLASS} ${
                    expandedSections.rawRequest ? 'rotate-180' : ''
                  }`} />
                </div>
              </div>
            </div>
            {expandedSections.rawRequest && (
              <div className={DETAIL_SECTION_BODY_CLASS}>
                <pre className="max-h-96 overflow-auto rounded-lg border border-gray-200 bg-gray-50 p-4 font-mono text-xs leading-5 text-gray-700 scrollbar-custom">
                  {formatJSON(request.body, rawRequestMaxDisplayChars)}
                </pre>
              </div>
            )}
          </div>
        </>
      )}

      {/* API Response */}
      {request.response && (
        <ResponseDetails
          response={request.response}
          endpoint={request.endpoint}
          showRawStreamEvents={showRawStreamEvents}
          rawResponseMaxDisplayChars={rawResponseMaxDisplayChars}
        />
      )}

      {/* Prompt Grading Results */}
      {request.promptGrade && (
        <PromptGradingResults promptGrade={request.promptGrade} />
      )}
    </div>
  );
}

function normalizeRequestMessages(body?: RequestRecord['body']): Array<{ role: string; content: any }> {
  if (!body) return [];
  if (Array.isArray(body.messages)) {
    const toolNamesById = new Map<string, string>();
    for (const message of body.messages) {
      for (const toolCall of message.tool_calls || []) {
        if (toolCall?.id && toolCall?.function?.name) {
          toolNamesById.set(toolCall.id, toolCall.function.name);
        }
      }
      for (const content of Array.isArray(message.content) ? message.content : []) {
        if (content?.type === 'tool_use' && content?.id && content?.name) {
          toolNamesById.set(content.id, content.name);
        }
      }
    }

    return body.messages.map(message => {
      if (message.role === 'tool') {
        return {
          role: message.role,
          content: {
            type: 'function_call_output',
            call_id: message.tool_call_id,
            tool_call_id: message.tool_call_id,
            name: (message as any).name || (
              message.tool_call_id ? toolNamesById.get(message.tool_call_id) : undefined
            ),
            output: message.content,
          },
        };
      }

      return {
        role: message.role,
        content: message.tool_calls?.length
          ? [
              ...(message.content ? [{ type: 'text', text: message.content }] : []),
              ...message.tool_calls.map((toolCall: any) => ({
                ...toolCall,
                type: 'function_call',
              })),
            ]
          : Array.isArray(message.content)
            ? message.content.map((content: any) => (
                content?.type === 'tool_result'
                  ? {
                      ...content,
                      name: content.name || toolNamesById.get(content.tool_use_id),
                    }
                  : content
              ))
            : message.content,
      };
    });
  }
  if (typeof body.input === 'string') {
    return [{ role: 'user', content: body.input }];
  }
  if (!Array.isArray(body.input)) return [];

  return body.input.map((item: any) => {
    if (item?.role) return { role: item.role, content: item.content };
    if (item?.type === 'function_call_output') {
      return { role: 'tool', content: item };
    }
    if (item?.type === 'function_call') {
      return { role: 'assistant', content: item };
    }
    return { role: item?.role || item?.type || 'input', content: item };
  });
}

// Message bubble component
function MessageBubble({ message, index }: { message: any; index: number }) {
  const roleColors = {
    'user': 'border-gray-200 border-l-blue-500 bg-white',
    'assistant': 'border-gray-200 border-l-gray-400 bg-gray-50/60',
    'system': 'border-amber-200 border-l-amber-400 bg-amber-50/40'
  };

  const roleIcons = {
    'user': User,
    'assistant': Bot,
    'system': Settings
  };

  const roleIconColors = {
    'user': 'text-blue-600',
    'assistant': 'text-gray-600',
    'system': 'text-yellow-600'
  };

  const Icon = roleIcons[message.role as keyof typeof roleIcons] || User;

  return (
    <div className={`rounded-lg border border-l-2 p-4 ${roleColors[message.role as keyof typeof roleColors] || 'border-gray-200 border-l-gray-400 bg-gray-50/60'}`}>
      <div className="mb-3 flex items-center justify-between">
        <div className="flex items-center gap-2.5">
          <div className="flex h-7 w-7 items-center justify-center rounded-md border border-gray-200 bg-white">
            <Icon className={`h-3.5 w-3.5 ${roleIconColors[message.role as keyof typeof roleIconColors] || 'text-gray-600'}`} />
          </div>
          <span className="text-sm font-medium capitalize text-gray-900">{message.role}</span>
          <span className="rounded-full border border-gray-200 bg-white px-2 py-0.5 text-[11px] text-gray-500">
            #{index + 1}
          </span>
        </div>
      </div>
      <div>
        <MessageContent content={message.content} />
      </div>
    </div>
  );
}

// Placeholder for prompt grading results - you can expand this
function PromptGradingResults({ promptGrade }: { promptGrade: any }) {
  return (
    <div className="rounded-lg border border-gray-200 bg-white p-4">
      <h4 className="mb-4 text-sm font-semibold text-gray-900">Prompt Quality Analysis</h4>
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <span className="text-gray-700">Overall Score:</span>
          <span className="text-2xl font-bold text-blue-600">{promptGrade.score}/5</span>
        </div>
        <div className="text-sm text-gray-600">
          <p>{promptGrade.feedback}</p>
        </div>
      </div>
    </div>
  );
}

// Response Details Component
function ResponseDetails({
  response,
  endpoint,
  showRawStreamEvents,
  rawResponseMaxDisplayChars,
}: {
  response: NonNullable<RequestRecord['response']>;
  endpoint: string;
  showRawStreamEvents: boolean;
  rawResponseMaxDisplayChars: number;
}) {
  const [expandedSections, setExpandedSections] = useState<Record<string, boolean>>({});
  const [copied, setCopied] = useState<Record<string, boolean>>({});

  const toggleSection = (section: string) => {
    setExpandedSections(prev => ({
      ...prev,
      [section]: !prev[section]
    }));
  };

  const handleCopy = async (content: string, key: string) => {
    try {
      await navigator.clipboard.writeText(content);
      setCopied(prev => ({ ...prev, [key]: true }));
      setTimeout(() => {
        setCopied(prev => ({ ...prev, [key]: false }));
      }, 2000);
    } catch (error) {
      console.error('Failed to copy to clipboard:', error);
    }
  };

  const getStatusColor = (statusCode: number) => {
    if (statusCode >= 200 && statusCode < 300) {
      return { bg: 'bg-green-50', border: 'border-green-200', text: 'text-green-700', icon: 'text-green-600' };
    }
    if (statusCode >= 400 && statusCode < 500) {
      return { bg: 'bg-yellow-50', border: 'border-yellow-200', text: 'text-yellow-700', icon: 'text-yellow-600' };
    }
    if (statusCode >= 500) {
      return { bg: 'bg-red-50', border: 'border-red-200', text: 'text-red-700', icon: 'text-red-600' };
    }
    return { bg: 'bg-gray-50', border: 'border-gray-200', text: 'text-gray-700', icon: 'text-gray-600' };
  };

  // Parse streaming chunks to extract the final assembled text
  const parseStreamingResponse = (chunks: string[]) => {
    let assembledText = '';
    let rawData = chunks.join('');
    
    try {
      // Split by lines and process each SSE event
      const lines = rawData.split('\n').filter(line => line.trim());
      
      for (const line of lines) {
        // Look for data lines in SSE format
        if (line.startsWith('data: ')) {
          const jsonStr = line.substring(6).trim();
          
          // Skip non-JSON lines (like "data: [DONE]")
          if (!jsonStr.startsWith('{')) continue;
          
          try {
            const eventData = JSON.parse(jsonStr);
            
            // Extract text from content_block_delta events
            if (eventData.type === 'content_block_delta' && 
                eventData.delta && 
                eventData.delta.type === 'text_delta' && 
                typeof eventData.delta.text === 'string') {
              assembledText += eventData.delta.text;
            }
          } catch (parseError) {
            // Skip malformed JSON
            continue;
          }
        }
      }
      
      // If we successfully extracted text, return it
      if (assembledText.trim().length > 0) {
        return {
          finalText: assembledText,
          isFormatted: true,
          rawData: rawData
        };
      }
      
      // Fallback: try to find any text content in the raw data
      const textMatches = rawData.match(/"text":"([^"]+)"/g);
      if (textMatches) {
        let fallbackText = '';
        for (const match of textMatches) {
          const text = match.match(/"text":"([^"]+)"/)?.[1];
          if (text) {
            // Unescape common JSON escape sequences
            fallbackText += text.replace(/\\n/g, '\n').replace(/\\"/g, '"').replace(/\\\\/g, '\\');
          }
        }
        if (fallbackText.trim()) {
          return {
            finalText: fallbackText,
            isFormatted: true,
            rawData: rawData
          };
        }
      }
      
    } catch (error) {
      console.warn('Error parsing streaming response:', error);
    }
    
    // Ultimate fallback to raw concatenation
    return {
      finalText: rawData,
      isFormatted: false,
      rawData: rawData
    };
  };

  const statusColors = getStatusColor(response.statusCode);
  const completedAt = response.completedAt ? new Date(response.completedAt).toLocaleString() : 'Unknown';
  const usage = getUsage(response.body);
  const completeResponseBody = response.body
    ? formatJSON(response.body, 0)
    : (response.bodyText || '');
  const displayedResponseBody = limitDisplayText(
    completeResponseBody,
    rawResponseMaxDisplayChars,
  );

  return (
    <div className={DETAIL_SECTION_CLASS} data-testid="api-response-section">
      <div 
        className={DETAIL_SECTION_HEADER_CLASS}
        onClick={() => toggleSection('overview')}
      >
        <div className="flex items-center justify-between">
          <h4 className={DETAIL_SECTION_TITLE_CLASS}>
            <DetailSectionIcon name="api-response" className="border-cyan-100 bg-cyan-50 text-cyan-600">
              <ArrowLeftRight className={DETAIL_SECTION_ICON_CLASS} />
            </DetailSectionIcon>
            <span>API Response</span>
            <span className={`rounded-full border px-2 py-0.5 text-[11px] font-medium ${statusColors.bg} ${statusColors.text} ${statusColors.border}`}>
              {response.statusCode}
            </span>
            {response.streamError && (
              <span className="text-xs px-2 py-1 rounded-full border bg-red-50 text-red-700 border-red-200">
                {response.streamError}
              </span>
            )}
            {response.truncated && (
              <span className="text-xs px-2 py-1 rounded-full border bg-amber-50 text-amber-700 border-amber-200">
                Log truncated
              </span>
            )}
          </h4>
          <ChevronDown className={`${DETAIL_SECTION_CHEVRON_CLASS} ${
            expandedSections.overview ? 'rotate-180' : ''
          }`} />
        </div>
      </div>
      
      {expandedSections.overview && (
        <div className="space-y-4 p-4">
          {/* Response Overview */}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <div className={`${statusColors.bg} rounded-lg border ${statusColors.border} p-3`}>
              <div className="flex items-center space-x-2 mb-2">
                <Activity className={`w-4 h-4 ${statusColors.icon}`} />
                <span className={`text-xs font-medium ${statusColors.text}`}>Status</span>
              </div>
              <div className={`text-base font-semibold ${statusColors.text}`}>{response.statusCode}</div>
              <div className={`text-xs ${statusColors.text} opacity-75`}>
                {response.statusCode >= 200 && response.statusCode < 300 ? 'Success' :
                 response.statusCode >= 400 && response.statusCode < 500 ? 'Client Error' :
                 response.statusCode >= 500 ? 'Server Error' : 'Unknown'}
              </div>
            </div>
            
            <div className="rounded-lg border border-gray-200 bg-gray-50 p-3">
              <div className="flex items-center space-x-2 mb-2">
                <Clock className="h-4 w-4 text-gray-500" />
                <span className="text-xs font-medium text-gray-600">Response Time</span>
              </div>
              <div className="text-base font-semibold text-gray-900">{response.responseTime}ms</div>
              <div className="text-xs text-gray-500">
                {response.responseTime < 1000 ? 'Fast' : response.responseTime < 3000 ? 'Normal' : 'Slow'}
              </div>
            </div>
            
            <div className="rounded-lg border border-gray-200 bg-gray-50 p-3">
              <div className="flex items-center space-x-2 mb-2">
                <Wifi className="h-4 w-4 text-gray-500" />
                <span className="text-xs font-medium text-gray-600">Type</span>
              </div>
              <div className="text-base font-semibold text-gray-900">
                {response.isStreaming ? 'Stream' : 'Single'}
              </div>
              <div className="text-xs text-gray-500">
                {response.isStreaming ? 'Streaming' : 'Complete'}
              </div>
            </div>
            
            <div className="rounded-lg border border-gray-200 bg-gray-50 p-3">
              <div className="flex items-center space-x-2 mb-2">
                <Calendar className="w-4 h-4 text-gray-600" />
                <span className="text-xs font-medium text-gray-700">Completed</span>
              </div>
              <div className="text-sm font-semibold text-gray-900">{completedAt.split(' ')[1] || 'N/A'}</div>
              <div className="text-xs text-gray-700 opacity-75">{completedAt.split(' ')[0] || ''}</div>
            </div>
          </div>

          {/* Token Usage */}
          {usage && (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
              <div className="rounded-lg border border-gray-200 bg-gray-50 p-3">
                <div className="flex items-center space-x-2 mb-2">
                  <Brain className="h-4 w-4 text-gray-500" />
                  <span className="text-xs font-medium text-gray-600">Input Tokens</span>
                </div>
                <div className="text-base font-semibold text-gray-900">
                  {usage.input.toLocaleString()}
                </div>
                <div className="text-xs text-gray-500">Prompt</div>
              </div>
              
              <div className="rounded-lg border border-gray-200 bg-gray-50 p-3">
                <div className="flex items-center space-x-2 mb-2">
                  <MessageCircle className="h-4 w-4 text-gray-500" />
                  <span className="text-xs font-medium text-gray-600">Output Tokens</span>
                </div>
                <div className="text-base font-semibold text-gray-900">
                  {usage.output.toLocaleString()}
                </div>
                <div className="text-xs text-gray-500">Response</div>
              </div>
              
              <div className="rounded-lg border border-gray-200 bg-gray-50 p-3">
                <div className="flex items-center space-x-2 mb-2">
                  <Cpu className="h-4 w-4 text-gray-500" />
                  <span className="text-xs font-medium text-gray-600">Total Tokens</span>
                </div>
                <div className="text-base font-semibold text-gray-900">
                  {usage.total.toLocaleString()}
                </div>
                <div className="text-xs text-gray-500">Combined</div>
              </div>
              
              {usage.cached > 0 && (
                <div className="rounded-lg border border-gray-200 bg-gray-50 p-3">
                  <div className="flex items-center space-x-2 mb-2">
                    <Bot className="h-4 w-4 text-gray-500" />
                    <span className="text-xs font-medium text-gray-600">Cached Tokens</span>
                  </div>
                  <div className="text-base font-semibold text-gray-900">
                    {usage.cached.toLocaleString()}
                  </div>
                  <div className="text-xs text-gray-500">From Cache</div>
                </div>
              )}
            </div>
          )}

          {response.body && (
            <SemanticResponse body={response.body} endpoint={endpoint} />
          )}

          {/* Response Headers */}
          {response.headers && (
            <div className="overflow-hidden rounded-lg border border-gray-200 bg-white">
              <div 
                className="px-4 py-3 border-b border-gray-200 cursor-pointer"
                onClick={() => toggleSection('responseHeaders')}
              >
                <div className="flex items-center justify-between">
                  <h5 className="text-sm font-semibold text-gray-900 flex items-center space-x-2">
                    <List className="w-4 h-4 text-gray-600" />
                    <span>Response Headers</span>
                    <span className="text-xs bg-gray-200 text-gray-700 px-2 py-1 rounded-full">
                      {Object.keys(response.headers).length}
                    </span>
                  </h5>
                  <ChevronDown className={`w-4 h-4 text-gray-500 transition-transform ${
                    expandedSections.responseHeaders ? 'rotate-180' : ''
                  }`} />
                </div>
              </div>
              {expandedSections.responseHeaders && (
                <div className="p-4">
                  <div className="bg-gray-50 rounded-lg p-3 border border-gray-200">
                    <div className="flex items-center justify-between mb-2">
                      <span className="text-sm font-medium text-gray-700">Headers</span>
                      <button
                        onClick={() => handleCopy(formatJSON(response.headers), 'responseHeaders')}
                        className="p-1 text-gray-500 hover:text-gray-700 transition-colors"
                        title="Copy response headers"
                      >
                        {copied.responseHeaders ? (
                          <Check className="w-4 h-4 text-green-600" />
                        ) : (
                          <Copy className="w-4 h-4" />
                        )}
                      </button>
                    </div>
                    <pre className="text-xs text-gray-700 overflow-x-auto">
                      {formatJSON(response.headers)}
                    </pre>
                  </div>
                </div>
              )}
            </div>
          )}

          {/* Response Body */}
          {(response.body || (!response.isStreaming && response.bodyText)) && (
            <div className="overflow-hidden rounded-lg border border-gray-200 bg-white">
              <div 
                className="px-4 py-3 border-b border-gray-200 cursor-pointer"
                onClick={() => toggleSection('responseBody')}
              >
                <div className="flex items-center justify-between">
                  <h5 className="text-sm font-semibold text-gray-900 flex items-center space-x-2">
                    <FileText className="w-4 h-4 text-gray-600" />
                    <span>Response Body</span>
                    <span className="text-xs bg-blue-100 text-blue-700 px-2 py-1 rounded-full border border-blue-200">
                      {response.body ? 'JSON' : 'Text'}
                    </span>
                  </h5>
                  <ChevronDown className={`w-4 h-4 text-gray-500 transition-transform ${
                    expandedSections.responseBody ? 'rotate-180' : ''
                  }`} />
                </div>
              </div>
              {expandedSections.responseBody && (
                <div className="p-4">
                  <div className="bg-gray-50 rounded-lg p-3 border border-gray-200">
                    <div className="flex items-center justify-between mb-2">
                      <span className="text-sm font-medium text-gray-700">Response</span>
                      <button
                        onClick={() => handleCopy(
                          completeResponseBody,
                          'responseBody'
                        )}
                        className="p-1 text-gray-500 hover:text-gray-700 transition-colors"
                        title="Copy response body"
                      >
                        {copied.responseBody ? (
                          <Check className="w-4 h-4 text-green-600" />
                        ) : (
                          <Copy className="w-4 h-4" />
                        )}
                      </button>
                    </div>
                    <pre className="text-xs text-gray-700 overflow-x-auto max-h-96 overflow-y-auto">
                      {displayedResponseBody}
                    </pre>
                  </div>
                </div>
              )}
            </div>
          )}

          {/* Streaming Response */}
          {showRawStreamEvents && response.isStreaming && response.bodyText && (
            <div className="overflow-hidden rounded-lg border border-gray-200 bg-white">
              <div
                className="px-4 py-3 border-b border-gray-200 cursor-pointer"
                onClick={() => toggleSection('rawSSE')}
              >
                <div className="flex items-center justify-between">
                  <h5 className="text-sm font-semibold text-gray-900 flex items-center space-x-2">
                    <Wifi className="w-4 h-4 text-gray-600" />
                    <span>Raw SSE Events</span>
                  </h5>
                  <ChevronDown className={`w-4 h-4 text-gray-500 transition-transform ${
                    expandedSections.rawSSE ? 'rotate-180' : ''
                  }`} />
                </div>
              </div>
              {expandedSections.rawSSE && (
                <pre className="m-4 text-xs text-gray-600 overflow-auto max-h-96 bg-gray-100 rounded p-3 font-mono">
                  {limitDisplayText(response.bodyText, rawResponseMaxDisplayChars)}
                </pre>
              )}
            </div>
          )}

          {showRawStreamEvents && response.isStreaming && response.streamingChunks && response.streamingChunks.length > 0 && (() => {
            const parsed = parseStreamingResponse(response.streamingChunks);
            return (
              <div className="overflow-hidden rounded-lg border border-gray-200 bg-white">
                <div 
                  className="px-4 py-3 border-b border-gray-200 cursor-pointer"
                  onClick={() => toggleSection('streamingResponse')}
                >
                  <div className="flex items-center justify-between">
                    <h5 className="text-sm font-semibold text-gray-900 flex items-center space-x-2">
                      <Wifi className="w-4 h-4 text-gray-600" />
                      <span>Streaming Response</span>
                      <span className="text-xs bg-blue-100 text-blue-700 px-2 py-1 rounded-full border border-blue-200">
                        {response.streamingChunks.length} chunks
                      </span>
                      {parsed.isFormatted && (
                        <span className="text-xs bg-green-100 text-green-700 px-2 py-1 rounded-full border border-green-200">
                          Parsed
                        </span>
                      )}
                    </h5>
                    <ChevronDown className={`w-4 h-4 text-gray-500 transition-transform ${
                      expandedSections.streamingResponse ? 'rotate-180' : ''
                    }`} />
                  </div>
                </div>
                {expandedSections.streamingResponse && (
                  <div className="space-y-3 p-4">
                    {/* Clean Parsed Response */}
                    {parsed.isFormatted && (
                      <div className="bg-white rounded-lg p-4 border border-green-200">
                        <div className="flex items-center justify-between mb-3">
                          <h6 className="text-sm font-semibold text-green-900 flex items-center space-x-2">
                            <Check className="w-4 h-4" />
                            <span>Final Response (Clean)</span>
                          </h6>
                          <button
                            onClick={() => handleCopy(parsed.finalText, 'streamingClean')}
                            className="p-1 text-gray-500 hover:text-gray-700 transition-colors"
                            title="Copy clean response"
                          >
                            {copied.streamingClean ? (
                              <Check className="w-4 h-4 text-green-600" />
                            ) : (
                              <Copy className="w-4 h-4" />
                            )}
                          </button>
                        </div>
                        <div className="bg-gray-50 rounded p-3 border border-gray-200">
                          <pre className="text-sm text-gray-900 whitespace-pre-wrap leading-relaxed">
                            {parsed.finalText}
                          </pre>
                        </div>
                        <div className="mt-2 text-xs text-green-600">
                          Extracted clean text from streaming chunks
                        </div>
                      </div>
                    )}

                    {/* Raw Data (Collapsible) */}
                    <div className="bg-gray-50 rounded-lg border border-gray-200">
                      <div 
                        className="px-3 py-2 cursor-pointer flex items-center justify-between"
                        onClick={() => toggleSection('rawStreamingData')}
                      >
                        <span className="text-sm font-medium text-gray-700 flex items-center space-x-2">
                          <FileText className="w-4 h-4" />
                          <span>Raw Streaming Data</span>
                        </span>
                        <ChevronDown className={`w-4 h-4 text-gray-500 transition-transform ${
                          expandedSections.rawStreamingData ? 'rotate-180' : ''
                        }`} />
                      </div>
                      {expandedSections.rawStreamingData && (
                        <div className="px-3 pb-3">
                          <div className="flex items-center justify-between mb-2">
                            <span className="text-xs text-gray-600">SSE Events & Metadata</span>
                            <button
                              onClick={() => handleCopy(parsed.rawData, 'streamingRaw')}
                              className="p-1 text-gray-500 hover:text-gray-700 transition-colors"
                              title="Copy raw data"
                            >
                              {copied.streamingRaw ? (
                                <Check className="w-3 h-3 text-green-600" />
                              ) : (
                                <Copy className="w-3 h-3" />
                              )}
                            </button>
                          </div>
                          <pre className="text-xs text-gray-600 overflow-x-auto max-h-64 overflow-y-auto bg-gray-100 rounded p-2 font-mono">
                            {limitDisplayText(parsed.rawData, rawResponseMaxDisplayChars)}
                          </pre>
                        </div>
                      )}
                    </div>

                    <div className="text-xs text-gray-500">
                      {parsed.isFormatted 
                        ? `Successfully parsed ${response.streamingChunks.length} streaming chunks`
                        : `Raw display of ${response.streamingChunks.length} streaming chunks (parsing failed)`
                      }
                    </div>
                  </div>
                )}
              </div>
            );
          })()}
        </div>
      )}
    </div>
  );
}

function SemanticResponse({ body, endpoint }: { body: any; endpoint: string }) {
  const protocol = getAPIProtocol(endpoint);
  const items: Array<{ title: string; content: any; reasoning?: boolean }> = [];

  if (protocol === 'OpenAI Chat Completions') {
    for (const choice of Array.isArray(body?.choices) ? body.choices : []) {
      const message = choice?.message || {};
      if (message.reasoning_content) {
        items.push({
          title: `Choice ${choice.index ?? 0} · Reasoning`,
          content: message.reasoning_content,
          reasoning: true,
        });
      }
      if (message.content || message.refusal) {
        items.push({
          title: `Choice ${choice.index ?? 0} · Assistant · ${choice.finish_reason || 'complete'}`,
          content: message.content || message.refusal,
        });
      }
      for (const toolCall of Array.isArray(message.tool_calls) ? message.tool_calls : []) {
        items.push({
          title: `Tool Call · ${toolCall.function?.name || toolCall.id || 'unknown'}`,
          content: toolCall,
        });
      }
    }
  } else if (protocol === 'OpenAI Responses') {
    for (const [index, output] of (Array.isArray(body?.output) ? body.output : []).entries()) {
      if (output?.type === 'message') {
        for (const content of Array.isArray(output.content) ? output.content : []) {
          items.push({
            title: `Output ${index + 1} · ${content.type || 'message'}`,
            content,
            reasoning: content.type === 'reasoning_text',
          });
        }
      } else if (output?.type === 'reasoning') {
        items.push({ title: `Output ${index + 1} · Reasoning`, content: output, reasoning: true });
      } else {
        items.push({ title: `Output ${index + 1} · ${output?.type || 'item'}`, content: output });
      }
    }
  } else {
    for (const content of Array.isArray(body?.content) ? body.content : []) {
      items.push({ title: content.type || 'Content', content });
    }
  }

  if (body?.error) {
    items.unshift({ title: 'Error', content: body.error });
  }
  if (items.length === 0) return null;

  return (
    <div className="space-y-3 rounded-lg border border-gray-200 bg-gray-50 p-4">
      <h5 className="text-sm font-semibold text-gray-900">Structured Output</h5>
      {items.map((item, index) => item.reasoning ? (
        <details key={index} className="rounded-lg border border-gray-200 bg-white p-3">
          <summary className="cursor-pointer text-sm font-medium text-gray-700">{item.title}</summary>
          <div className="mt-3"><MessageContent content={item.content} /></div>
        </details>
      ) : (
        <div key={index} className="rounded-lg border border-gray-200 bg-white p-3">
          <div className="mb-2 text-xs font-semibold text-gray-600">{item.title}</div>
          <MessageContent content={item.content} />
        </div>
      ))}
    </div>
  );
}

// Tool Card Component
function ToolCard({ tool, index }: { tool: any; index: number }) {
  const [expanded, setExpanded] = useState(false);
  const [copiedSchema, setCopiedSchema] = useState(false);
  const toolName = tool.name || tool.function?.name || tool.type || 'Unknown Tool';
  const description = tool.description || tool.function?.description || '';
  const schema = tool.input_schema || tool.parameters || tool.function?.parameters;

  const handleCopySchema = async () => {
    try {
      await navigator.clipboard.writeText(formatJSON(schema));
      setCopiedSchema(true);
      setTimeout(() => setCopiedSchema(false), 2000);
    } catch (error) {
      console.error('Failed to copy schema:', error);
    }
  };

  // Parse description to identify code blocks and format them
  const formatDescription = (description: string) => {
    // Split by code blocks (text between backticks)
    const parts = description.split(/(`[^`]+`)/g);
    
    return parts.map((part, i) => {
      if (part.startsWith('`') && part.endsWith('`')) {
        // Code inline
        const code = part.slice(1, -1);
        return (
          <code key={i} className="bg-gray-100 text-gray-800 px-1.5 py-0.5 rounded text-xs font-mono">
            {code}
          </code>
        );
      }
      
      // Return non-code parts as plain text
      return <span key={i}>{part}</span>;
    });
  };

  const isLongDescription = description.length > 300;
  const displayDescription = expanded ? description : description.slice(0, 300);

  return (
    <div className="overflow-hidden rounded-lg border border-gray-200 bg-gray-50/60">
      <div className="p-4">
        <div className="mb-3 flex items-start justify-between">
          <div className="flex items-center gap-3">
            <div className="flex h-8 w-8 items-center justify-center rounded-md border border-gray-200 bg-white">
              <Wrench className="h-4 w-4 text-gray-500" />
            </div>
            <div>
              <h5 className="text-sm font-semibold text-gray-900">{toolName}</h5>
              <span className="text-xs text-gray-500">Tool #{index + 1}</span>
            </div>
          </div>
        </div>
        
        <div className="prose prose-sm max-w-none">
          <div className="text-sm text-gray-700 leading-relaxed space-y-2">
            <div className="whitespace-pre-wrap">
              {formatDescription(displayDescription)}
              {isLongDescription && !expanded && '...'}
            </div>
            {isLongDescription && (
              <button
                onClick={() => setExpanded(!expanded)}
                className="text-blue-600 hover:text-blue-700 text-xs font-medium mt-2"
              >
                {expanded ? 'Show less' : 'Show more'}
              </button>
            )}
          </div>
        </div>
        
        {schema && (
          <div className="mt-4">
            <div className="overflow-hidden rounded-lg border border-gray-200 bg-white">
              <div className="flex items-center justify-between border-b border-gray-200 bg-gray-50 px-4 py-2">
                <span className="text-xs font-semibold text-gray-700 flex items-center space-x-2">
                  <Settings className="w-3.5 h-3.5" />
                  <span>Input Schema</span>
                </span>
                <button
                  onClick={handleCopySchema}
                  className="p-1 text-gray-500 hover:text-gray-700 transition-colors"
                  title="Copy schema"
                >
                  {copiedSchema ? (
                    <Check className="w-3.5 h-3.5 text-green-600" />
                  ) : (
                    <Copy className="w-3.5 h-3.5" />
                  )}
                </button>
              </div>
              <div className="p-3">
                <pre className="text-xs text-gray-700 overflow-x-auto font-mono">
                  {formatJSON(schema)}
                </pre>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
