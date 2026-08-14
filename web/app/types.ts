/* eslint-disable @typescript-eslint/no-explicit-any -- API payloads are protocol-dependent JSON. */

export interface RequestRecord {
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
      name?: string;
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

export interface SessionSummary {
  sessionId: string;
  parentSessionId?: string;
  purpose?: string;
  kind: 'root' | 'subagent' | 'memory-maintenance' | string;
  title: string;
  model?: string;
  agentName?: string;
  taskCallId?: string;
  taskDescription?: string;
  status: 'completed' | 'error' | 'interrupted' | 'pending' | 'awaiting-tool' | 'awaiting-result' | 'captured' | string;
  resultMessage?: string;
  requestCount: number;
  toolCallCount: number;
  inputTokens: number;
  outputTokens: number;
  responseTimeMs: number;
  firstTimestamp: string;
  lastTimestamp: string;
  children?: SessionSummary[];
}

export interface SessionDetail {
  summary: SessionSummary;
  requests: RequestRecord[];
  children?: SessionDetail[];
}
