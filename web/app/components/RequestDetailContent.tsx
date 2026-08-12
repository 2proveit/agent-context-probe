import { useEffect, useState } from 'react';
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

interface Request {
  id: string;
  requestId: string;
  timestamp: string;
  method: string;
  endpoint: string;
  headers: Record<string, string[]>;
  originalModel?: string;
  routedModel?: string;
  body?: {
    [key: string]: any;
    model?: string;
    messages?: Array<{
      role: string;
      content: any;
      tool_calls?: any[];
      tool_call_id?: string;
    }>;
    system?: Array<{
      text: string;
      type: string;
      cache_control?: { type: string };
    }>;
    tools?: Array<{
      name: string;
      description: string;
      input_schema?: {
        type: string;
        properties?: Record<string, any>;
        required?: string[];
      };
    }>;
    max_tokens?: number;
    max_output_tokens?: number;
    temperature?: number;
    stream?: boolean;
    instructions?: any;
    input?: any;
  };
  response?: {
    statusCode: number;
    headers: Record<string, string[]>;
    body?: any;
    bodyText?: string;
    responseTime: number;
    streamingChunks?: string[];
    isStreaming: boolean;
    completedAt: string;
    truncated?: boolean;
    capturedBytes?: number;
    responseBytes?: number;
    streamError?: string;
  };
  promptGrade?: {
    score: number;
    criteria: Record<string, { score: number; feedback: string }>;
    feedback: string;
    improvedPrompt: string;
    gradingTimestamp: string;
  };
}

interface RequestDetailContentProps {
  request: Request;
  onGrade: () => void;
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

  const canGradeRequest = (request: Request) => {
    return request.body && 
           request.body.messages && 
           request.body.messages.some(msg => msg.role === 'user') &&
           request.endpoint.includes('/messages');
  };

  return (
    <div className="space-y-6">
      {/* Request Overview */}
      <div className="bg-white border border-gray-200 rounded-xl p-6 shadow-sm">
        <div className="flex items-center justify-between mb-4">
          <h4 className="text-lg font-semibold text-gray-900 flex items-center space-x-3">
            <Info className="w-5 h-5 text-blue-600" />
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
        <div className="grid grid-cols-2 gap-6 text-sm">
          <div className="space-y-3">
            <div className="flex items-center space-x-3">
              <span className="text-gray-500 font-medium min-w-[80px]">Method:</span>
              <span className={`px-3 py-1.5 rounded-lg text-xs font-semibold uppercase tracking-wide ${getMethodColor(request.method)}`}>
                {request.method}
              </span>
            </div>
            <div className="flex items-center space-x-3">
              <span className="text-gray-500 font-medium min-w-[80px]">Endpoint:</span>
              <code className="text-blue-600 bg-blue-50 px-2 py-1 rounded font-mono text-xs border border-blue-200">
                {request.endpoint}
              </code>
              <span className={`px-2 py-1 rounded-full text-xs font-semibold ${getProtocolBadgeClasses(request.endpoint)}`}>
                {protocol}
              </span>
            </div>
          </div>
          <div className="space-y-3">
            <div className="flex items-center space-x-3">
              <span className="text-gray-500 font-medium min-w-[80px]">Timestamp:</span>
              <span className="text-gray-900">{new Date(request.timestamp).toLocaleString()}</span>
            </div>
            <div className="flex items-center space-x-3">
              <span className="text-gray-500 font-medium min-w-[80px]">User Agent:</span>
              <span className="text-gray-600 text-xs">{request.headers['User-Agent']?.[0] || 'N/A'}</span>
            </div>
          </div>
        </div>
      </div>

      {/* Headers */}
      <div className="bg-white border border-gray-200 rounded-xl overflow-hidden shadow-sm">
        <div 
          className="bg-gray-50 px-6 py-4 border-b border-gray-200 cursor-pointer"
          onClick={() => toggleSection('headers')}
        >
          <div className="flex items-center justify-between">
            <h4 className="text-lg font-semibold text-gray-900 flex items-center space-x-3">
              <Settings className="w-5 h-5 text-blue-600" />
              <span>Request Headers</span>
            </h4>
            <ChevronDown className={`w-5 h-5 text-gray-500 transition-transform ${
              expandedSections.headers ? 'rotate-180' : ''
            }`} />
          </div>
        </div>
        {expandedSections.headers && (
          <div className="p-6">
            <div className="bg-gray-50 rounded-lg p-4 border border-gray-200">
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
            <div className="bg-white border border-gray-200 rounded-xl overflow-hidden shadow-sm">
              <div 
                className="bg-gray-50 px-6 py-4 border-b border-gray-200 cursor-pointer"
                onClick={() => toggleSection('system')}
              >
                <div className="flex items-center justify-between">
                  <h4 className="text-lg font-semibold text-gray-900 flex items-center space-x-3">
                    <Cpu className="w-5 h-5 text-yellow-600" />
                    <span>System Instructions</span>
                    <span className="text-xs bg-yellow-50 text-yellow-700 px-2 py-1 rounded-full border border-yellow-200">
                      {request.body.system?.length || 1} items
                    </span>
                  </h4>
                  <ChevronDown className={`w-5 h-5 text-gray-500 transition-transform ${
                    expandedSections.system ? 'rotate-180' : ''
                  }`} />
                </div>
              </div>
              {expandedSections.system && (
                <div className="p-6 space-y-4">
                  {(request.body.system || [{ text: request.body.instructions, type: 'instructions' }]).map((sys: any, index: number) => (
                    <div key={index} className="bg-yellow-50 border border-yellow-200 rounded-lg p-4">
                      <div className="flex items-center justify-between mb-3">
                        <span className="text-yellow-700 font-medium text-sm">System Message #{index + 1}</span>
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
            <div className="bg-white border border-gray-200 rounded-xl overflow-hidden shadow-sm">
              <div 
                className="bg-gray-50 px-6 py-4 border-b border-gray-200 cursor-pointer"
                onClick={() => toggleSection('tools')}
              >
                <div className="flex items-center justify-between">
                  <h4 className="text-lg font-semibold text-gray-900 flex items-center space-x-3">
                    <Wrench className="w-5 h-5 text-indigo-600" />
                    <span>Available Tools</span>
                    <span className="text-xs bg-indigo-50 text-indigo-700 px-2 py-1 rounded-full border border-indigo-200">
                      {request.body.tools.length} tools
                    </span>
                  </h4>
                  <ChevronDown className={`w-5 h-5 text-gray-500 transition-transform ${
                    expandedSections.tools ? 'rotate-180' : ''
                  }`} />
                </div>
              </div>
              {expandedSections.tools && (
                <div className="p-6 space-y-4">
                  {request.body.tools.map((tool, index) => (
                    <ToolCard key={index} tool={tool} index={index} />
                  ))}
                </div>
              )}
            </div>
          )}

          {/* Conversation */}
          {conversationItems.length > 0 && (
            <div className="bg-white border border-gray-200 rounded-xl overflow-hidden shadow-sm">
              <div 
                className="bg-gray-50 px-6 py-4 border-b border-gray-200 cursor-pointer"
                onClick={() => toggleSection('conversation')}
              >
                <div className="flex items-center justify-between">
                  <h4 className="text-lg font-semibold text-gray-900 flex items-center space-x-3">
                    <MessageCircle className="w-5 h-5 text-blue-600" />
                    <span>Conversation</span>
                    <span className="text-xs bg-blue-50 text-blue-700 px-2 py-1 rounded-full border border-blue-200">
                      {conversationItems.length} items
                    </span>
                  </h4>
                  <ChevronDown className={`w-5 h-5 text-gray-500 transition-transform ${
                    expandedSections.conversation ? 'rotate-180' : ''
                  }`} />
                </div>
              </div>
              {expandedSections.conversation && (
                <div className="p-6 space-y-4 max-h-[600px] overflow-y-auto">
                  {conversationItems.map((message, index) => (
                    <MessageBubble key={index} message={message} index={index} />
                  ))}
                </div>
              )}
            </div>
          )}

          {/* Model Configuration */}
          <div className="bg-white border border-gray-200 rounded-xl overflow-hidden shadow-sm">
            <div 
              className="bg-gray-50 px-6 py-4 border-b border-gray-200 cursor-pointer"
              onClick={() => toggleSection('model')}
            >
              <div className="flex items-center justify-between">
                <h4 className="text-lg font-semibold text-gray-900 flex items-center space-x-3">
                  <Brain className="w-5 h-5 text-purple-600" />
                  <span>Model Configuration</span>
                </h4>
                <ChevronDown className={`w-5 h-5 text-gray-500 transition-transform ${
                  expandedSections.model ? 'rotate-180' : ''
                }`} />
              </div>
            </div>
            {expandedSections.model && (
              <div className="p-6 space-y-4">
                {/* Model Routing Information */}
                {request.routedModel && request.routedModel !== request.originalModel && (
                  <div className="bg-gradient-to-r from-purple-50 to-blue-50 border border-purple-200 rounded-xl p-4">
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
                <div className="grid grid-cols-2 gap-4">
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

          <div className="bg-white border border-gray-200 rounded-xl overflow-hidden shadow-sm">
            <div
              className="bg-gray-50 px-6 py-4 border-b border-gray-200 cursor-pointer"
              onClick={() => toggleSection('rawRequest')}
            >
              <div className="flex items-center justify-between">
                <h4 className="text-lg font-semibold text-gray-900 flex items-center space-x-3">
                  <FileText className="w-5 h-5 text-gray-600" />
                  <span>Raw Request JSON</span>
                </h4>
                <div className="flex items-center space-x-2">
                  <button
                    type="button"
                    onClick={(event) => {
                      event.stopPropagation();
                      handleCopy(formatJSONForCopy(request.body), 'rawRequest');
                    }}
                    className="inline-flex items-center space-x-1.5 rounded-lg border border-gray-200 bg-white px-3 py-1.5 text-xs font-medium text-gray-600 shadow-sm transition-colors hover:border-blue-200 hover:bg-blue-50 hover:text-blue-700"
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
                  <ChevronDown className={`w-5 h-5 text-gray-500 transition-transform ${
                    expandedSections.rawRequest ? 'rotate-180' : ''
                  }`} />
                </div>
              </div>
            </div>
            {expandedSections.rawRequest && (
              <div className="p-6">
                <pre className="text-xs text-gray-700 overflow-auto max-h-96 bg-gray-50 rounded-lg p-4 border border-gray-200">
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

function normalizeRequestMessages(body?: Request['body']): Array<{ role: string; content: any }> {
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
    'user': 'bg-blue-50 border border-blue-200',
    'assistant': 'bg-gray-50 border border-gray-200',
    'system': 'bg-yellow-50 border border-yellow-200'
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
    <div className={`rounded-lg p-4 ${roleColors[message.role as keyof typeof roleColors] || 'bg-gray-50 border border-gray-200'}`}>
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center space-x-3">
          <div className="w-8 h-8 bg-white rounded-lg flex items-center justify-center border border-gray-200">
            <Icon className={`w-4 h-4 ${roleIconColors[message.role as keyof typeof roleIconColors] || 'text-gray-600'}`} />
          </div>
          <span className="font-medium capitalize text-gray-900">{message.role}</span>
          <span className="text-xs text-gray-500 bg-white px-2 py-1 rounded-full border border-gray-200">
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
    <div className="bg-white border border-gray-200 rounded-xl p-6 shadow-sm">
      <h4 className="text-lg font-semibold text-gray-900 mb-4">Prompt Quality Analysis</h4>
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
  response: NonNullable<Request['response']>;
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
    <div className="bg-white border border-gray-200 rounded-xl overflow-hidden shadow-sm border-l-4 border-l-blue-500">
      <div 
        className="bg-gray-50 px-6 py-4 border-b border-gray-200 cursor-pointer"
        onClick={() => toggleSection('overview')}
      >
        <div className="flex items-center justify-between">
          <h4 className="text-lg font-semibold text-gray-900 flex items-center space-x-3">
            <ArrowLeftRight className="w-5 h-5 text-blue-600" />
            <span>API Response</span>
            <span className={`text-xs px-2 py-1 rounded-full border ${statusColors.bg} ${statusColors.text} ${statusColors.border}`}>
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
          <ChevronDown className={`w-5 h-5 text-gray-500 transition-transform ${
            expandedSections.overview ? 'rotate-180' : ''
          }`} />
        </div>
      </div>
      
      {expandedSections.overview && (
        <div className="p-6 space-y-6">
          {/* Response Overview */}
          <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
            <div className={`${statusColors.bg} border ${statusColors.border} rounded-lg p-4`}>
              <div className="flex items-center space-x-2 mb-2">
                <Activity className={`w-4 h-4 ${statusColors.icon}`} />
                <span className={`text-xs font-medium ${statusColors.text}`}>Status</span>
              </div>
              <div className={`text-lg font-bold ${statusColors.text}`}>{response.statusCode}</div>
              <div className={`text-xs ${statusColors.text} opacity-75`}>
                {response.statusCode >= 200 && response.statusCode < 300 ? 'Success' :
                 response.statusCode >= 400 && response.statusCode < 500 ? 'Client Error' :
                 response.statusCode >= 500 ? 'Server Error' : 'Unknown'}
              </div>
            </div>
            
            <div className="bg-blue-50 border border-blue-200 rounded-lg p-4">
              <div className="flex items-center space-x-2 mb-2">
                <Clock className="w-4 h-4 text-blue-600" />
                <span className="text-xs font-medium text-blue-700">Response Time</span>
              </div>
              <div className="text-lg font-bold text-blue-700">{response.responseTime}ms</div>
              <div className="text-xs text-blue-700 opacity-75">
                {response.responseTime < 1000 ? 'Fast' : response.responseTime < 3000 ? 'Normal' : 'Slow'}
              </div>
            </div>
            
            <div className="bg-purple-50 border border-purple-200 rounded-lg p-4">
              <div className="flex items-center space-x-2 mb-2">
                <Wifi className="w-4 h-4 text-purple-600" />
                <span className="text-xs font-medium text-purple-700">Type</span>
              </div>
              <div className="text-lg font-bold text-purple-700">
                {response.isStreaming ? 'Stream' : 'Single'}
              </div>
              <div className="text-xs text-purple-700 opacity-75">
                {response.isStreaming ? 'Streaming' : 'Complete'}
              </div>
            </div>
            
            <div className="bg-gray-50 border border-gray-200 rounded-lg p-4">
              <div className="flex items-center space-x-2 mb-2">
                <Calendar className="w-4 h-4 text-gray-600" />
                <span className="text-xs font-medium text-gray-700">Completed</span>
              </div>
              <div className="text-sm font-bold text-gray-700">{completedAt.split(' ')[1] || 'N/A'}</div>
              <div className="text-xs text-gray-700 opacity-75">{completedAt.split(' ')[0] || ''}</div>
            </div>
          </div>

          {/* Token Usage */}
          {usage && (
            <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
              <div className="bg-indigo-50 border border-indigo-200 rounded-lg p-4">
                <div className="flex items-center space-x-2 mb-2">
                  <Brain className="w-4 h-4 text-indigo-600" />
                  <span className="text-xs font-medium text-indigo-700">Input Tokens</span>
                </div>
                <div className="text-lg font-bold text-indigo-700">
                  {usage.input.toLocaleString()}
                </div>
                <div className="text-xs text-indigo-700 opacity-75">Prompt</div>
              </div>
              
              <div className="bg-emerald-50 border border-emerald-200 rounded-lg p-4">
                <div className="flex items-center space-x-2 mb-2">
                  <MessageCircle className="w-4 h-4 text-emerald-600" />
                  <span className="text-xs font-medium text-emerald-700">Output Tokens</span>
                </div>
                <div className="text-lg font-bold text-emerald-700">
                  {usage.output.toLocaleString()}
                </div>
                <div className="text-xs text-emerald-700 opacity-75">Response</div>
              </div>
              
              <div className="bg-amber-50 border border-amber-200 rounded-lg p-4">
                <div className="flex items-center space-x-2 mb-2">
                  <Cpu className="w-4 h-4 text-amber-600" />
                  <span className="text-xs font-medium text-amber-700">Total Tokens</span>
                </div>
                <div className="text-lg font-bold text-amber-700">
                  {usage.total.toLocaleString()}
                </div>
                <div className="text-xs text-amber-700 opacity-75">Combined</div>
              </div>
              
              {usage.cached > 0 && (
                <div className="bg-green-50 border border-green-200 rounded-lg p-4">
                  <div className="flex items-center space-x-2 mb-2">
                    <Bot className="w-4 h-4 text-green-600" />
                    <span className="text-xs font-medium text-green-700">Cached Tokens</span>
                  </div>
                  <div className="text-lg font-bold text-green-700">
                    {usage.cached.toLocaleString()}
                  </div>
                  <div className="text-xs text-green-700 opacity-75">From Cache</div>
                </div>
              )}
            </div>
          )}

          {response.body && (
            <SemanticResponse body={response.body} endpoint={endpoint} />
          )}

          {/* Response Headers */}
          {response.headers && (
            <div className="bg-gray-50 border border-gray-200 rounded-xl overflow-hidden">
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
                <div className="px-4 pb-4">
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
            <div className="bg-gray-50 border border-gray-200 rounded-xl overflow-hidden">
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
                <div className="px-4 pb-4">
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
            <div className="bg-gray-50 border border-gray-200 rounded-xl overflow-hidden">
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
              <div className="bg-gray-50 border border-gray-200 rounded-xl overflow-hidden">
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
                  <div className="px-4 pb-4 space-y-3">
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
    <div className="bg-blue-50 border border-blue-200 rounded-xl p-4 space-y-3">
      <h5 className="text-sm font-semibold text-blue-900">Structured Output</h5>
      {items.map((item, index) => item.reasoning ? (
        <details key={index} className="bg-white border border-purple-200 rounded-lg p-3">
          <summary className="cursor-pointer text-sm font-medium text-purple-700">{item.title}</summary>
          <div className="mt-3"><MessageContent content={item.content} /></div>
        </details>
      ) : (
        <div key={index} className="bg-white border border-blue-200 rounded-lg p-3">
          <div className="text-xs font-semibold text-blue-700 mb-2">{item.title}</div>
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
    <div className="bg-gray-50 border border-gray-200 rounded-xl overflow-hidden">
      <div className="p-5">
        <div className="flex items-start justify-between mb-4">
          <div className="flex items-center space-x-3">
            <div className="w-10 h-10 bg-white rounded-lg flex items-center justify-center border border-gray-200 shadow-sm">
              <Wrench className="w-5 h-5 text-gray-600" />
            </div>
            <div>
              <h5 className="text-lg font-bold text-gray-900">{toolName}</h5>
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
            <div className="bg-white rounded-lg border border-gray-200 overflow-hidden">
              <div className="bg-gray-50 px-4 py-2 border-b border-gray-200 flex items-center justify-between">
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
