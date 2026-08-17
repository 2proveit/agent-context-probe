import type { MetaFunction } from "@remix-run/node";
import { useState, useEffect, useTransition } from "react";
import { 
  RefreshCw, 
  Trash2, 
  List,
  MessagesSquare,
  FileText,
  X,
  Copy,
  Check,
  Loader2,
  ArrowLeftRight
} from "lucide-react";

import RequestDetailContent from "../components/RequestDetailContent";
import SessionView from "../components/SessionView";
import type { RequestRecord, SessionDetail, SessionSummary } from "../types";
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

type TimeRange = "all" | "15m" | "1h" | "6h" | "24h" | "7d";
type ViewMode = "sessions" | "requests";

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
  const [viewMode, setViewMode] = useState<ViewMode>("sessions");
  const [requests, setRequests] = useState<RequestRecord[]>([]);
  const [totalRequests, setTotalRequests] = useState(0);
  const [sessions, setSessions] = useState<SessionSummary[]>([]);
  const [totalSessions, setTotalSessions] = useState(0);
  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(null);
  const [sessionDetail, setSessionDetail] = useState<SessionDetail | null>(null);
  const [isFetchingSessions, setIsFetchingSessions] = useState(false);
  const [isFetchingSessionDetail, setIsFetchingSessionDetail] = useState(false);
  const [selectedRequest, setSelectedRequest] = useState<RequestRecord | null>(null);
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

  const loadSessionDetail = async (sessionId: string) => {
    setSelectedSessionId(sessionId);
    setIsFetchingSessionDetail(true);
    try {
      const url = new URL('/api/sessions', window.location.origin);
      url.searchParams.set('session', sessionId);
      const response = await fetch(url.toString());
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      const data = await response.json();
      const normalize = (detail: SessionDetail): SessionDetail => ({
        ...detail,
        requests: (detail.requests ?? []).map(request => ({
          ...request,
          id: request.id || request.requestId,
        })),
        children: detail.children?.map(normalize),
      });
      setSessionDetail(data.session ? normalize(data.session) : null);
    } catch (error) {
      console.error('Failed to load session details:', error);
      setSessionDetail(null);
    } finally {
      setIsFetchingSessionDetail(false);
    }
  };

  const loadSessions = async (filters = appliedFilters) => {
    setIsFetchingSessions(true);
    const since = getSinceTimestamp(filters.timeRange);
    try {
      const url = new URL('/api/sessions', window.location.origin);
      url.searchParams.set('page', '1');
      url.searchParams.set('limit', '100');
      if (filters.model.trim()) url.searchParams.set('model', filters.model.trim());
      if (filters.header.trim()) url.searchParams.set('header', filters.header.trim());
      if (since) url.searchParams.set('since', since);
      const response = await fetch(url.toString());
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      const data = await response.json();
      const nextSessions: SessionSummary[] = data.sessions ?? [];
      setSessions(nextSessions);
      setTotalSessions(typeof data.total === 'number' ? data.total : nextSessions.length);
      setModelOptions(previous => Array.from(new Set([
        ...previous,
        ...nextSessions.flatMap(session => [
          session.model,
          ...(session.children ?? []).map(child => child.model),
        ]).filter((model): model is string => Boolean(model)),
      ])).sort((left, right) => left.localeCompare(right)));

      const nextSelected = nextSessions.some(session => session.sessionId === selectedSessionId)
        ? selectedSessionId
        : nextSessions[0]?.sessionId ?? null;
      setSelectedSessionId(nextSelected);
      if (nextSelected) await loadSessionDetail(nextSelected);
      else setSessionDetail(null);
    } catch (error) {
      console.error('Failed to load sessions:', error);
      setSessions([]);
      setTotalSessions(0);
      setSessionDetail(null);
    } finally {
      setIsFetchingSessions(false);
    }
  };

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
      const rawRequests: Array<Omit<RequestRecord, 'id'>> = data.requests || [];
      const mappedRequests = rawRequests.map((req, index: number) => ({
        ...req,
        id: req.requestId ? `${req.requestId}_${index}` : `request_${index}` 
      }));
      const total = typeof data.total === "number" ? data.total : mappedRequests.length;
      const discoveredModels = mappedRequests.flatMap((request: RequestRecord) => (
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
        setSessions([]);
        setTotalSessions(0);
        setSelectedSessionId(null);
        setSessionDetail(null);
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
    if (viewMode === "sessions") loadSessions(normalizedFilters);
    else loadRequests(false, normalizedFilters);
  };

  const resetFilters = () => {
    setDraftFilters(DEFAULT_FILTERS);
    setAppliedFilters(DEFAULT_FILTERS);
    if (viewMode === "sessions") loadSessions(DEFAULT_FILTERS);
    else loadRequests(false, DEFAULT_FILTERS);
  };

  const hasActiveFilters = appliedFilters.timeRange !== "all"
    || appliedFilters.model !== ""
    || appliedFilters.header !== "";

  const showRequestDetails = (requestId: string) => {
    const request = requests.find(r => r.id === requestId);
    if (request) {
      setSelectedRequest(request);
      setIsModalOpen(true);
    }
  };

  const showSessionRequestDetails = (request: RequestRecord) => {
    setSelectedRequest({ ...request, id: request.id || request.requestId });
    setIsModalOpen(true);
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

  const canGradeRequest = (request: RequestRecord) => {
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
    loadSessions(DEFAULT_FILTERS);
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
        <div className="flex w-full items-center justify-between gap-4 px-4 py-3 sm:px-6">
          <div className="flex items-center gap-4">
            <div className="hidden sm:block">
              <p className="text-sm font-semibold text-gray-900">Agent Context Probe</p>
              <p className="text-[11px] text-gray-400">LLM session inspector</p>
            </div>
            <div className="flex rounded-lg border border-gray-200 bg-gray-50 p-1" role="tablist" aria-label="Monitor view">
              <button
                type="button"
                role="tab"
                data-testid="sessions-tab"
                aria-selected={viewMode === 'sessions'}
                onClick={() => {
                  setViewMode('sessions');
                  if (!sessions.length) loadSessions(appliedFilters);
                }}
                className={`inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition ${viewMode === 'sessions' ? 'bg-white text-gray-900 shadow-sm' : 'text-gray-500 hover:text-gray-800'}`}
              >
                <MessagesSquare className="h-3.5 w-3.5" /> Sessions
              </button>
              <button
                type="button"
                role="tab"
                data-testid="requests-tab"
                aria-selected={viewMode === 'requests'}
                onClick={() => {
                  setViewMode('requests');
                  if (!requests.length) loadRequests(false, appliedFilters);
                }}
                className={`inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition ${viewMode === 'requests' ? 'bg-white text-gray-900 shadow-sm' : 'text-gray-500 hover:text-gray-800'}`}
              >
                <List className="h-3.5 w-3.5" /> Model Calls
              </button>
            </div>
          </div>
          <div className="flex items-center space-x-2">
            <button
              onClick={() => viewMode === 'sessions' ? loadSessions(appliedFilters) : loadRequests()}
              className="p-1.5 text-gray-600 hover:bg-gray-100 rounded transition-colors"
              title="Refresh"
            >
              <RefreshCw className="w-4 h-4" />
            </button>
            <button
              onClick={clearRequests}
              className="p-1.5 text-red-600 hover:bg-red-50 rounded transition-colors"
              title="Clear all model calls"
            >
              <Trash2 className="w-4 h-4" />
            </button>
          </div>
        </div>
      </header>

      {/* Main Content */}
      <main className="w-full space-y-8 px-4 py-6 sm:px-6 lg:py-8">
        {/* Stats Grid */}
        <div className="mb-6">
          <div className="bg-white border border-gray-200 rounded-lg p-4">
            <div className="flex flex-col gap-5">
              <div className="flex items-end justify-between">
                <div>
                  <p className="text-xs font-medium text-gray-500 uppercase tracking-wider">
                    {hasActiveFilters
                      ? `Matching ${viewMode === 'sessions' ? 'Sessions' : 'Model Calls'}`
                      : `Total ${viewMode === 'sessions' ? 'Sessions' : 'Model Calls'}`}
                  </p>
                  <p className="text-2xl font-semibold text-gray-900 mt-1">
                    {viewMode === 'sessions' ? totalSessions : totalRequests}
                  </p>
                </div>
                {(viewMode === 'sessions' ? totalSessions > sessions.length : totalRequests > requests.length) && (
                  <p className="text-xs text-gray-500">
                    {viewMode === 'sessions' ? sessions.length : requests.length} loaded
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
                    disabled={isFetching || isFetchingSessions}
                    className="h-9 rounded-md bg-gray-900 px-4 text-xs font-medium text-white transition hover:bg-gray-700 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    Apply
                  </button>
                  <button
                    type="button"
                    onClick={resetFilters}
                    disabled={isFetching || isFetchingSessions}
                    className="h-9 rounded-md border border-gray-300 px-3 text-xs font-medium text-gray-700 transition hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    Reset
                  </button>
                </div>
              </form>
            </div>
          </div>
        </div>

        {viewMode === 'sessions' ? (
          <SessionView
            sessions={sessions}
            total={totalSessions}
            selectedSessionId={selectedSessionId}
            detail={sessionDetail}
            isLoadingList={isFetchingSessions}
            isLoadingDetail={isFetchingSessionDetail}
            onSelectSession={loadSessionDetail}
            onOpenRequest={showSessionRequestDetails}
          />
        ) : (
          /* Request History */
          <div className="overflow-hidden rounded-xl border border-gray-200 bg-white shadow-sm">
            <div className="bg-gray-50/70">
              <div className="mx-auto flex h-16 w-full max-w-4xl items-center px-4">
                <div>
                  <h2 className="text-sm font-semibold uppercase tracking-wider text-gray-900">Model Calls</h2>
                  <p className="mt-0.5 text-xs text-gray-400">{totalRequests} captured model {totalRequests === 1 ? 'call' : 'calls'}</p>
                </div>
              </div>
            </div>
            <div className="py-2">
              {(isFetching && requestsCurrentPage === 1) || isPending ? (
                <div className="p-8 text-center">
                  <Loader2 className="w-6 h-6 mx-auto animate-spin text-gray-400" />
                  <p className="mt-2 text-xs text-gray-500">Loading model calls...</p>
                </div>
              ) : requests.length === 0 ? (
                <div className="p-8 text-center text-gray-500">
                  <h3 className="text-sm font-medium text-gray-600 mb-1">
                    {hasActiveFilters ? "No model calls match these filters" : "No model calls found"}
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
                    <div
                      key={request.id}
                      data-testid="request-row"
                      className="cursor-pointer py-1"
                      onClick={() => showRequestDetails(request.id)}
                    >
                      <div
                        data-testid="request-row-content"
                        className="mx-auto grid w-full max-w-4xl grid-cols-1 gap-3 rounded-lg px-4 py-3 transition-colors hover:bg-gray-50/80 sm:grid-cols-[minmax(0,36rem)_auto] sm:items-start sm:gap-6"
                      >
                        <div data-testid="request-summary" className="min-w-0">
                          {/* Model and Status */}
                          <div className="mb-1.5 flex flex-wrap items-center gap-2">
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
                          <div className="mb-1.5 truncate font-mono text-xs text-gray-500">
                            {request.endpoint}
                          </div>
                          
                          {/* Metrics Row */}
                          <div className="flex flex-wrap items-center gap-3 text-xs">
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
                        <div data-testid="request-actions" className="flex flex-shrink-0 items-start space-x-3 sm:justify-self-start">
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
                    <div className="p-3 text-center">
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
        )}
      </main>

      {/* Request Detail Modal */}
      {isModalOpen && selectedRequest && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-gray-900/55 p-3 backdrop-blur-[2px] sm:p-6">
          <div
            data-testid="request-detail-modal"
            className="flex h-[calc(100dvh-1.5rem)] w-[calc(100vw-1.5rem)] flex-col overflow-hidden rounded-xl border border-gray-200 bg-white shadow-2xl sm:h-[min(88dvh,960px)] sm:w-[min(92vw,1600px)]"
          >
            <div className="shrink-0 border-b border-gray-200 bg-white px-4 py-3 sm:px-5 sm:py-4">
              <div className="flex items-center justify-between">
                <div className="flex min-w-0 items-center gap-3">
                  <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-gray-900 text-white">
                    <FileText className="h-4 w-4" />
                  </div>
                  <div className="min-w-0">
                    <h3 className="text-base font-semibold text-gray-900">Request Details</h3>
                    <p className="truncate font-mono text-[11px] text-gray-400">{selectedRequest.requestId}</p>
                  </div>
                </div>
                <div className="flex items-center space-x-2">
                  <button
                    type="button"
                    onClick={() => copyRequestId(selectedRequest.requestId)}
                    className="inline-flex items-center gap-1.5 rounded-md border border-gray-200 bg-white px-2.5 py-1.5 text-xs font-medium text-gray-600 transition-colors hover:border-blue-200 hover:bg-blue-50 hover:text-blue-700"
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
                    className="rounded-md p-1.5 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600"
                    aria-label="Close request details"
                  >
                    <X className="w-5 h-5" />
                  </button>
                </div>
              </div>
            </div>
            <div
              data-testid="request-detail-scroll"
              className="min-h-0 flex-1 overflow-y-auto bg-gray-50/70 p-3 scrollbar-custom sm:p-5"
            >
              <RequestDetailContent request={selectedRequest} onGrade={() => gradeRequest(selectedRequest.id)} />
            </div>
          </div>
        </div>
      )}

    </div>
  );
}
