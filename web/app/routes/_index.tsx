import type { MetaFunction } from "@remix-run/node";
import { useState, useEffect, useTransition } from "react";
import { 
  Activity, 
  RefreshCw, 
  Trash2, 
  List,
  FileText,
  X,
  ChevronRight,
  ChevronDown,
  Inbox,
  Wrench,
  Bot,
  User,
  Settings,
  Users,
  Target,
  Cpu,
  CheckCircle,
  ClipboardCheck,
  BarChart3,
  MessageSquare,
  Copy,
  Check,
  Lightbulb,
  Loader2,
  ArrowLeftRight
} from "lucide-react";

import RequestDetailContent from "../components/RequestDetailContent";
import {
  getAPIProtocol,
  getProtocolBadgeClasses,
  getUsage,
} from "../utils/models";

export const meta: MetaFunction = () => {
  return [
    { title: "API Request Monitor" },
    { name: "description", content: "Real-time API request visualization" },
  ];
};

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
    model?: string;
    messages?: Array<{
      role: string;
      content: any;
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
    temperature?: number;
    stream?: boolean;
  };
  response?: {
    statusCode: number;
    headers: Record<string, string[]>;
    body?: {
      usage?: {
        input_tokens?: number;
        output_tokens?: number;
        cache_creation_input_tokens?: number;
        cache_read_input_tokens?: number;
        service_tier?: string;
      };
      [key: string]: any;
    };
    bodyText?: string;
    responseTime: number;
    streamingChunks?: string[];
    isStreaming: boolean;
    completedAt: string;
    streamError?: string;
    truncated?: boolean;
  };
  promptGrade?: {
    score: number;
    criteria: Record<string, { score: number; feedback: string }>;
    feedback: string;
    improvedPrompt: string;
    gradingTimestamp: string;
  };
}

type TimeRange = "all" | "15m" | "1h" | "6h" | "24h" | "7d";

interface RequestFilters {
  timeRange: TimeRange;
  model: string;
  header: string;
}

const DEFAULT_FILTERS: RequestFilters = {
  timeRange: "all",
  model: "",
  header: "",
};

const TIME_RANGE_MILLISECONDS: Partial<Record<TimeRange, number>> = {
  "15m": 15 * 60 * 1000,
  "1h": 60 * 60 * 1000,
  "6h": 6 * 60 * 60 * 1000,
  "24h": 24 * 60 * 60 * 1000,
  "7d": 7 * 24 * 60 * 60 * 1000,
};

const getSinceTimestamp = (timeRange: TimeRange) => {
  const duration = TIME_RANGE_MILLISECONDS[timeRange];
  return duration ? new Date(Date.now() - duration).toISOString() : "";
};

export default function Index() {
  const [requests, setRequests] = useState<Request[]>([]);
  const [totalRequests, setTotalRequests] = useState(0);
  const [selectedRequest, setSelectedRequest] = useState<Request | null>(null);
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [isFetching, setIsFetching] = useState(false);
  const [isPending, startTransition] = useTransition();
  const [requestsCurrentPage, setRequestsCurrentPage] = useState(1);
  const [hasMoreRequests, setHasMoreRequests] = useState(true);
  const [draftFilters, setDraftFilters] = useState<RequestFilters>(DEFAULT_FILTERS);
  const [appliedFilters, setAppliedFilters] = useState<RequestFilters>(DEFAULT_FILTERS);
  const [appliedSince, setAppliedSince] = useState("");
  const [modelOptions, setModelOptions] = useState<string[]>([]);
  const [copiedRequestId, setCopiedRequestId] = useState<string | null>(null);
  const itemsPerPage = 50;

  const loadRequests = async (loadMore = false, filters = appliedFilters) => {
    setIsFetching(true);
    const since = loadMore ? appliedSince : getSinceTimestamp(filters.timeRange);
    if (!loadMore) {
      setRequestsCurrentPage(1);
      setAppliedSince(since);
    }
    const pageToFetch = loadMore ? requestsCurrentPage + 1 : 1;
    try {
      const url = new URL('/api/requests', window.location.origin);
      url.searchParams.append("page", pageToFetch.toString());
      url.searchParams.append("limit", itemsPerPage.toString());
      if (filters.model.trim()) {
        url.searchParams.append("model", filters.model.trim());
      }
      if (filters.header.trim()) {
        url.searchParams.append("header", filters.header.trim());
      }
      if (since) {
        url.searchParams.append("since", since);
      }

      const response = await fetch(url.toString());
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      
      const data = await response.json();
      const requests = data.requests || [];
      const mappedRequests = requests.map((req: any, index: number) => ({
        ...req,
        id: req.requestId ? `${req.requestId}_${index}` : `request_${index}` 
      }));
      const total = typeof data.total === "number" ? data.total : mappedRequests.length;
      const discoveredModels = mappedRequests.flatMap((request: Request) => (
        [request.routedModel, request.body?.model, request.originalModel]
      )).filter((model: string | undefined): model is string => Boolean(model));
      
      startTransition(() => {
        if (loadMore) {
          setRequests(prev => [...prev, ...mappedRequests]);
        } else {
          setRequests(mappedRequests);
        }
        setTotalRequests(total);
        setModelOptions(previous => (
          Array.from(new Set([...previous, ...discoveredModels])).sort((left, right) => left.localeCompare(right))
        ));
        setRequestsCurrentPage(pageToFetch);
        setHasMoreRequests(pageToFetch * itemsPerPage < total);
      });
    } catch (error) {
      console.error('Failed to load requests:', error);
      startTransition(() => {
        setRequests([]);
        setTotalRequests(0);
      });
    } finally {
      setIsFetching(false);
    }
  };

  const clearRequests = async () => {
    try {
      const response = await fetch('/api/requests', {
        method: 'DELETE'
      });
      
      if (response.ok) {
        setRequests([]);
        setTotalRequests(0);
        setRequestsCurrentPage(1);
        setHasMoreRequests(true);
      }
    } catch (error) {
      console.error('Failed to clear requests:', error);
      setRequests([]);
    }
  };

  const applyFilters = () => {
    const normalizedFilters = {
      ...draftFilters,
      model: draftFilters.model.trim(),
      header: draftFilters.header.trim(),
    };
    setDraftFilters(normalizedFilters);
    setAppliedFilters(normalizedFilters);
    loadRequests(false, normalizedFilters);
  };

  const resetFilters = () => {
    setDraftFilters(DEFAULT_FILTERS);
    setAppliedFilters(DEFAULT_FILTERS);
    loadRequests(false, DEFAULT_FILTERS);
  };

  const hasActiveFilters = appliedFilters.timeRange !== "all"
    || appliedFilters.model !== ""
    || appliedFilters.header !== "";

  const getMethodColor = (method: string) => {
    const colors = {
      'GET': 'bg-green-50 text-green-700 border border-green-200',
      'POST': 'bg-blue-50 text-blue-700 border border-blue-200',
      'PUT': 'bg-yellow-50 text-yellow-700 border border-yellow-200',
      'DELETE': 'bg-red-50 text-red-700 border border-red-200'
    };
    return colors[method as keyof typeof colors] || 'bg-gray-50 text-gray-700 border border-gray-200';
  };

  const getRequestSummary = (request: Request) => {
    const parts = [];
    
    // Add token usage if available
    if (request.response?.body?.usage) {
      const usage = request.response.body.usage;
      const inputTokens = usage.input_tokens || 0;
      const outputTokens = usage.output_tokens || 0;
      const totalTokens = inputTokens + outputTokens;
      
      if (totalTokens > 0) {
        parts.push(`🪙 ${totalTokens.toLocaleString()} tokens`);
        
        if (usage.cache_read_input_tokens) {
          parts.push(`💾 ${usage.cache_read_input_tokens.toLocaleString()} cached`);
        }
      }
    }
    
    // Add response time if available
    if (request.response?.responseTime) {
      const seconds = (request.response.responseTime / 1000).toFixed(1);
      parts.push(`⏱️ ${seconds}s`);
    }
    
    // Add model if available (use routed model if different from original)
    const model = request.routedModel || request.body?.model;
    if (model) {
      const modelShort = model.includes('opus') ? 'Opus' :
                         model.includes('sonnet') ? 'Sonnet' :
                         model.includes('haiku') ? 'Haiku' : 
                         model.includes('gpt-4o') ? 'gpt-4o' :
                         model.includes('o3') ? 'o3' :
                         model.includes('o3-mini') ? 'o3-mini' : 'Model';
      parts.push(`🤖 ${modelShort}`);
      
      // Show routing info if model was routed
      if (request.routedModel && request.originalModel && request.routedModel !== request.originalModel) {
        parts.push(`→ routed`);
      }
    }
    
    return parts.length > 0 ? parts.join(' • ') : '📡 API request';
  };

  const showRequestDetails = (requestId: string) => {
    const request = requests.find(r => r.id === requestId);
    if (request) {
      setSelectedRequest(request);
      setIsModalOpen(true);
    }
  };

  const copyRequestId = async (requestId: string) => {
    try {
      await navigator.clipboard.writeText(requestId);
      setCopiedRequestId(requestId);
      setTimeout(() => {
        setCopiedRequestId(current => current === requestId ? null : current);
      }, 2000);
    } catch (error) {
      console.error('Failed to copy request ID:', error);
    }
  };

  const closeModal = () => {
    setIsModalOpen(false);
    setSelectedRequest(null);
  };

  const getToolStats = () => {
    let toolDefinitions = 0;
    let toolCalls = 0;
    
    requests.forEach(req => {
      if (req.body) {
        // Count tool definitions in system prompts
        if (req.body.system) {
          req.body.system.forEach(sys => {
            if (sys.text && sys.text.includes('<functions>')) {
              const functionMatches = [...sys.text.matchAll(/<function>([\s\S]*?)<\/function>/g)];
              toolDefinitions += functionMatches.length;
            }
          });
        }
        
        // Count actual tool calls in messages
        if (req.body.messages) {
          req.body.messages.forEach(msg => {
            if (msg.content && Array.isArray(msg.content)) {
              msg.content.forEach((contentPart: any) => {
                if (contentPart.type === 'tool_use') {
                  toolCalls++;
                }
                if (contentPart.type === 'text' && contentPart.text && contentPart.text.includes('<functions>')) {
                  const functionMatches = [...contentPart.text.matchAll(/<function>([\s\S]*?)<\/function>/g)];
                  toolDefinitions += functionMatches.length;
                }
              });
            }
          });
        }
      }
    });
    
    return `${toolCalls} calls / ${toolDefinitions} tools`;
  };

  const getPromptGradeStats = () => {
    let totalGrades = 0;
    let gradeCount = 0;
    
    requests.forEach(req => {
      if (req.promptGrade && req.promptGrade.score) {
        totalGrades += req.promptGrade.score;
        gradeCount++;
      }
    });
    
    if (gradeCount > 0) {
      const avgGrade = (totalGrades / gradeCount).toFixed(1);
      return `${avgGrade}/5`;
    }
    return '-/5';
  };

  const canGradeRequest = (request: Request) => {
    return request.body && 
           request.body.messages && 
           request.body.messages.some(msg => msg.role === 'user') &&
           request.endpoint.includes('/messages');
  };

  const gradeRequest = async (requestId: string) => {
    const request = requests.find(r => r.id === requestId);
    if (!request || !canGradeRequest(request)) return;

    try {
      const response = await fetch('/api/grade-prompt', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({
          messages: request.body!.messages,
          systemMessages: request.body!.system || [],
          requestId: request.timestamp
        })
      });

      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }

      const promptGrade = await response.json();
      
      // Update the request with the new grading
      const updatedRequests = requests.map(r => 
        r.id === requestId ? { ...r, promptGrade } : r
      );
      setRequests(updatedRequests);
      
    } catch (error) {
      console.error('Failed to grade prompt:', error);
    }
  };

  useEffect(() => {
    loadRequests();
  }, []);

  // Handle escape key to close modals
  useEffect(() => {
    const handleEscapeKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        if (isModalOpen) {
          closeModal();
        }
      }
    };

    window.addEventListener('keydown', handleEscapeKey);
    
    return () => {
      window.removeEventListener('keydown', handleEscapeKey);
    };
  }, [isModalOpen]);

  return (
    <div className="min-h-screen bg-gray-50">
      {/* Header */}
      <header className="sticky top-0 z-40 bg-white border-b border-gray-200">
        <div className="max-w-7xl mx-auto px-6 py-3 flex items-center justify-end">
          <div className="flex items-center space-x-2">
            <button
              onClick={() => loadRequests()}
              className="p-1.5 text-gray-600 hover:bg-gray-100 rounded transition-colors"
              title="Refresh"
            >
              <RefreshCw className="w-4 h-4" />
            </button>
            <button
              onClick={clearRequests}
              className="p-1.5 text-red-600 hover:bg-red-50 rounded transition-colors"
              title="Clear all requests"
            >
              <Trash2 className="w-4 h-4" />
            </button>
          </div>
        </div>
      </header>

      {/* Main Content */}
      <main className="max-w-7xl mx-auto px-6 py-8 space-y-8">
        {/* Stats Grid */}
        <div className="mb-6">
          <div className="bg-white border border-gray-200 rounded-lg p-4">
            <div className="flex flex-col gap-5">
              <div className="flex items-end justify-between">
                <div>
                  <p className="text-xs font-medium text-gray-500 uppercase tracking-wider">
                    {hasActiveFilters ? "Matching Requests" : "Total Requests"}
                  </p>
                  <p className="text-2xl font-semibold text-gray-900 mt-1">
                    {totalRequests}
                  </p>
                </div>
                {totalRequests > requests.length && (
                  <p className="text-xs text-gray-500">
                    {requests.length} loaded
                  </p>
                )}
              </div>
              <form
                className="grid grid-cols-1 gap-3 border-t border-gray-100 pt-4 md:grid-cols-[160px_minmax(180px,1fr)_minmax(240px,1.4fr)_auto]"
                onSubmit={(event) => {
                  event.preventDefault();
                  applyFilters();
                }}
              >
                <label className="block">
                  <span className="mb-1.5 block text-xs font-medium text-gray-600">Time range</span>
                  <select
                    value={draftFilters.timeRange}
                    onChange={(event) => setDraftFilters(previous => ({
                      ...previous,
                      timeRange: event.target.value as TimeRange,
                    }))}
                    className="h-9 w-full rounded-md border border-gray-300 bg-white px-3 text-sm text-gray-800 outline-none transition focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
                  >
                    <option value="all">Any time</option>
                    <option value="15m">Last 15 minutes</option>
                    <option value="1h">Last hour</option>
                    <option value="6h">Last 6 hours</option>
                    <option value="24h">Last 24 hours</option>
                    <option value="7d">Last 7 days</option>
                  </select>
                </label>
                <label className="block">
                  <span className="mb-1.5 block text-xs font-medium text-gray-600">Model</span>
                  <input
                    type="text"
                    list="request-model-options"
                    value={draftFilters.model}
                    onChange={(event) => setDraftFilters(previous => ({
                      ...previous,
                      model: event.target.value,
                    }))}
                    placeholder="e.g. deepseek"
                    className="h-9 w-full rounded-md border border-gray-300 px-3 text-sm text-gray-800 outline-none transition placeholder:text-gray-400 focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
                  />
                  <datalist id="request-model-options">
                    {modelOptions.map(model => (
                      <option key={model} value={model} />
                    ))}
                  </datalist>
                </label>
                <label className="block">
                  <span className="mb-1.5 block text-xs font-medium text-gray-600">Request header</span>
                  <input
                    type="text"
                    value={draftFilters.header}
                    onChange={(event) => setDraftFilters(previous => ({
                      ...previous,
                      header: event.target.value,
                    }))}
                    placeholder="Header name or value"
                    className="h-9 w-full rounded-md border border-gray-300 px-3 text-sm text-gray-800 outline-none transition placeholder:text-gray-400 focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
                  />
                </label>
                <div className="flex items-end gap-2">
                  <button
                    type="submit"
                    disabled={isFetching}
                    className="h-9 rounded-md bg-gray-900 px-4 text-xs font-medium text-white transition hover:bg-gray-700 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    Apply
                  </button>
                  <button
                    type="button"
                    onClick={resetFilters}
                    disabled={isFetching}
                    className="h-9 rounded-md border border-gray-300 px-3 text-xs font-medium text-gray-700 transition hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    Reset
                  </button>
                </div>
              </form>
            </div>
          </div>
        </div>

        {/* Request History */}
        <div className="bg-white border border-gray-200 rounded-lg overflow-hidden">
            <div className="bg-gray-50 px-4 py-3 border-b border-gray-200">
              <div className="flex items-center justify-between">
                <h2 className="text-sm font-semibold text-gray-900 uppercase tracking-wider">Request History</h2>
              </div>
            </div>
            <div className="divide-y divide-gray-200">
              {(isFetching && requestsCurrentPage === 1) || isPending ? (
                <div className="p-8 text-center">
                  <Loader2 className="w-6 h-6 mx-auto animate-spin text-gray-400" />
                  <p className="mt-2 text-xs text-gray-500">Loading requests...</p>
                </div>
              ) : requests.length === 0 ? (
                <div className="p-8 text-center text-gray-500">
                  <h3 className="text-sm font-medium text-gray-600 mb-1">
                    {hasActiveFilters ? "No requests match these filters" : "No requests found"}
                  </h3>
                  {hasActiveFilters ? (
                    <p className="text-xs text-gray-500">Try a wider time range or clear one of the filters.</p>
                  ) : (
                    <p className="text-xs text-gray-500">Make sure you have set <code className="font-mono bg-gray-100 px-1 py-0.5 rounded">ANTHROPIC_BASE_URL</code> to point at the proxy</p>
                  )}
                </div>
              ) : (
                <>
                  {requests.map(request => (
                    <div key={request.id} className="px-4 py-3 hover:bg-gray-50 transition-colors cursor-pointer border-b border-gray-100 last:border-b-0" onClick={() => showRequestDetails(request.id)}>
                      <div className="flex items-start justify-between">
                        <div className="flex-1 min-w-0 mr-4">
                          {/* Model and Status */}
                          <div className="flex items-center space-x-3 mb-1">
                            <h3 className="text-sm font-medium">
                              {request.routedModel || request.body?.model ? (
                                // Use routedModel if available, otherwise fall back to body.model
                                (() => {
                                  const model = request.routedModel || request.body?.model || '';
                                  if (model.includes('opus')) return <span className="text-purple-600 font-semibold">Opus</span>;
                                  if (model.includes('sonnet')) return <span className="text-indigo-600 font-semibold">Sonnet</span>;
                                  if (model.includes('haiku')) return <span className="text-teal-600 font-semibold">Haiku</span>;
                                  if (model.includes('gpt-4o')) return <span className="text-green-600 font-semibold">GPT-4o</span>;
                                  if (model.includes('gpt')) return <span className="text-green-600 font-semibold">GPT</span>;
                                  return <span className="text-gray-900">{model.split('-')[0]}</span>;
                                })()
                              ) : <span className="text-gray-900">API</span>}
                            </h3>
                            {request.routedModel && request.routedModel !== request.originalModel && (
                              <span className="text-xs px-1.5 py-0.5 bg-blue-100 text-blue-700 rounded font-medium flex items-center space-x-1">
                                <ArrowLeftRight className="w-3 h-3" />
                                <span>routed</span>
                              </span>
                            )}
                            {request.response?.statusCode && (
                              <span className={`text-xs font-medium px-1.5 py-0.5 rounded ${
                                request.response.statusCode >= 200 && request.response.statusCode < 300 
                                  ? 'bg-green-100 text-green-700' 
                                  : request.response.statusCode >= 300 && request.response.statusCode < 400
                                  ? 'bg-yellow-100 text-yellow-700'
                                  : 'bg-red-100 text-red-700'
                              }`}>
                                {request.response.statusCode}
                              </span>
                            )}
                            <span className={`text-xs px-1.5 py-0.5 rounded font-medium ${getProtocolBadgeClasses(request.endpoint)}`}>
                              {getAPIProtocol(request.endpoint)}
                            </span>
                          </div>
                          
                          {/* Endpoint */}
                          <div className="text-xs text-gray-600 font-mono mb-1">
                            {request.endpoint}
                          </div>
                          
                          {/* Metrics Row */}
                          <div className="flex items-center space-x-3 text-xs">
                            {getUsage(request.response?.body) && (
                              <>
                                <span className="font-mono text-gray-600">
                                  <span className="font-medium text-gray-900">{getUsage(request.response?.body)!.total.toLocaleString()}</span> tokens
                                </span>
                                {getUsage(request.response?.body)!.cached > 0 && (
                                  <span className="font-mono bg-green-50 text-green-700 px-1.5 py-0.5 rounded">
                                    {getUsage(request.response?.body)!.cached.toLocaleString()} cached
                                  </span>
                                )}
                              </>
                            )}
                            {request.response?.streamError && (
                              <span className="font-mono text-red-600">interrupted</span>
                            )}
                            
                            {request.response?.responseTime !== undefined && request.response?.responseTime !== null && (
                              <span className="font-mono text-gray-600">
                                <span className="font-medium text-gray-900">{(request.response.responseTime / 1000).toFixed(2)}</span>s
                              </span>
                            )}
                          </div>
                        </div>
                        <div className="flex flex-shrink-0 items-start space-x-3">
                          <button
                            type="button"
                            onClick={(event) => {
                              event.stopPropagation();
                              copyRequestId(request.requestId);
                            }}
                            className="inline-flex items-center space-x-1.5 rounded-md border border-gray-200 bg-white px-2.5 py-1.5 text-xs font-medium text-gray-600 shadow-sm transition-colors hover:border-blue-200 hover:bg-blue-50 hover:text-blue-700"
                            title={`Copy request ID: ${request.requestId}`}
                            aria-label={`Copy request ID ${request.requestId}`}
                          >
                            {copiedRequestId === request.requestId ? (
                              <Check className="h-3.5 w-3.5 text-green-600" />
                            ) : (
                              <Copy className="h-3.5 w-3.5" />
                            )}
                            <span>{copiedRequestId === request.requestId ? 'Copied' : 'Copy ID'}</span>
                          </button>
                          <div className="text-right">
                            <div className="text-xs text-gray-500">
                              {new Date(request.timestamp).toLocaleDateString()}
                            </div>
                            <div className="text-xs text-gray-400">
                              {new Date(request.timestamp).toLocaleTimeString()}
                            </div>
                          </div>
                        </div>
                      </div>
                    </div>
                  ))}
                  {hasMoreRequests && (
                    <div className="p-3 text-center border-t border-gray-100">
                      <button
                        onClick={() => loadRequests(true)}
                        disabled={isFetching}
                        className="px-3 py-1.5 text-xs font-medium text-gray-700 bg-gray-100 rounded hover:bg-gray-200 disabled:opacity-50 transition-colors"
                      >
                        {isFetching ? "Loading..." : "Load More"}
                      </button>
                    </div>
                  )}
                </>
              )}
            </div>
        </div>
      </main>

      {/* Request Detail Modal */}
      {isModalOpen && selectedRequest && (
        <div className="fixed inset-0 bg-gray-900/70 backdrop-blur-sm z-50 flex items-center justify-center p-6">
          <div className="bg-white rounded-xl max-w-6xl w-full max-h-[90vh] overflow-hidden shadow-2xl">
            <div className="bg-gray-50 px-6 py-4 border-b border-gray-200">
              <div className="flex items-center justify-between">
                <div className="flex items-center space-x-3">
                  <FileText className="w-5 h-5 text-blue-600" />
                  <h3 className="text-lg font-semibold text-gray-900">Request Details</h3>
                </div>
                <div className="flex items-center space-x-2">
                  <button
                    type="button"
                    onClick={() => copyRequestId(selectedRequest.requestId)}
                    className="inline-flex items-center space-x-1.5 rounded-lg border border-gray-200 bg-white px-3 py-1.5 text-xs font-medium text-gray-600 shadow-sm transition-colors hover:border-blue-200 hover:bg-blue-50 hover:text-blue-700"
                    title={`Copy request ID: ${selectedRequest.requestId}`}
                    aria-label={`Copy request ID ${selectedRequest.requestId}`}
                  >
                    {copiedRequestId === selectedRequest.requestId ? (
                      <Check className="h-4 w-4 text-green-600" />
                    ) : (
                      <Copy className="h-4 w-4" />
                    )}
                    <span>{copiedRequestId === selectedRequest.requestId ? 'Copied' : 'Copy ID'}</span>
                  </button>
                  <button
                    onClick={closeModal}
                    className="text-gray-400 hover:text-gray-600 transition-colors p-2 hover:bg-gray-100 rounded-lg"
                    aria-label="Close request details"
                  >
                    <X className="w-5 h-5" />
                  </button>
                </div>
              </div>
            </div>
            <div className="p-6 overflow-y-auto max-h-[calc(90vh-100px)]">
              <RequestDetailContent request={selectedRequest} onGrade={() => gradeRequest(selectedRequest.id)} />
            </div>
          </div>
        </div>
      )}

    </div>
  );
}
