import { useState } from 'react';
import { Wrench, ChevronDown, ChevronRight, Copy, Check } from 'lucide-react';
import { formatValue, formatJSON, isComplexObject } from '../utils/formatters';
import { CodeDiff } from './CodeDiff';
import { TodoList } from './TodoList';

interface ToolUseProps {
  name: string;
  id: string;
  input?: Record<string, any>;
  text?: string;
  title?: string;
  statusText?: string;
}

export function ToolUse({
  name,
  id,
  input = {},
  text,
  title = 'Tool Execution',
  statusText = 'Tool execution initiated',
}: ToolUseProps) {
  const [detailsExpanded, setDetailsExpanded] = useState(false);
  const [showAllParameters, setShowAllParameters] = useState(false);
  const [expandedParameters, setExpandedParameters] = useState<Record<string, boolean>>({});
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(formatJSON({ name, id, input }));
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch (error) {
      console.error('Failed to copy to clipboard:', error);
    }
  };

  const renderParameterValue = (key: string, value: any) => {
    if (typeof value === 'string') {
      if (value.length > 200 || value.includes('\n')) {
        const expanded = Boolean(expandedParameters[key]);
        return (
          <div>
            <button 
              className="mb-2 text-xs text-blue-600 transition-colors hover:text-blue-800 hover:underline"
              onClick={() => setExpandedParameters(current => ({ ...current, [key]: !current[key] }))}
            >
              {expanded ? 'Hide' : 'Show'} large parameter
            </button>
            {expanded && (
              <pre className="max-h-64 overflow-auto rounded-lg border border-gray-200 bg-gray-50 p-3 font-mono text-xs">
                {value}
              </pre>
            )}
          </div>
        );
      }
      return <span className="text-gray-700 text-sm break-all font-mono">{value}</span>;
    }
    
    if (isComplexObject(value)) {
      return (
        <details className="cursor-pointer">
          <summary className="text-xs text-blue-600 transition-colors hover:text-blue-800 hover:underline">
            Show object ({Object.keys(value).length} properties)
          </summary>
          <pre className="mt-2 bg-gray-50 border border-gray-200 p-3 rounded-lg text-xs overflow-auto font-mono">
            {formatJSON(value)}
          </pre>
        </details>
      );
    }

    return <span className="text-gray-700 text-sm font-mono">{formatValue(value)}</span>;
  };

  const summarizeValue = (value: any) => {
    if (typeof value === 'string') return value.replace(/\s+/g, ' ').slice(0, 80);
    if (value === null || value === undefined) return String(value);
    if (typeof value === 'object') return Array.isArray(value) ? `${value.length} items` : `${Object.keys(value).length} fields`;
    return String(value);
  };

  const parameterEntries = Object.entries(input);
  const argumentSummary = parameterEntries.length > 0
    ? parameterEntries.slice(0, 2).map(([key, value]) => `${key}: ${summarizeValue(value)}`).join(', ')
    : 'No parameters';

  return (
    <div
      data-tool-use-id={id}
      data-expanded={detailsExpanded ? 'true' : 'false'}
      className={`overflow-hidden rounded-lg border transition ${
        detailsExpanded
          ? 'border-gray-200 bg-white shadow-sm'
          : 'border-transparent bg-white/50 hover:border-gray-200 hover:bg-gray-50'
      }`}
    >
      <button
        type="button"
        onClick={() => setDetailsExpanded(current => !current)}
        aria-expanded={detailsExpanded}
        className="group flex w-full items-center gap-3 px-3 py-2.5 text-left"
      >
        <span className="flex h-7 w-7 shrink-0 items-center justify-center text-gray-500">
          <Wrench className="h-4 w-4" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex min-w-0 items-baseline gap-2">
            <span className="shrink-0 text-sm font-medium text-gray-900">{name}</span>
            <span className="truncate text-sm text-gray-500">— {argumentSummary}</span>
          </span>
        </span>
        <span className="hidden shrink-0 items-center gap-3 text-xs text-gray-400 sm:flex">
          <span>{title}</span>
          <span className="h-2 w-2 rounded-full bg-gray-300" />
          <ChevronDown className={`h-4 w-4 transition-transform ${detailsExpanded ? 'rotate-180' : ''}`} />
        </span>
      </button>

      {detailsExpanded && (
        <div className="border-t border-gray-200 px-4 pb-4 pt-3 sm:ml-11 sm:mr-3">
          <div className="mb-3 flex min-w-0 items-center justify-between gap-3">
            <span className="truncate font-mono text-xs text-gray-400">{id}</span>
            <button
              type="button"
              onClick={handleCopy}
              className="inline-flex shrink-0 items-center gap-1.5 rounded-md border border-gray-200 bg-white px-2.5 py-1.5 text-xs font-medium text-gray-600 transition-colors hover:bg-gray-50 hover:text-gray-900"
              title="Copy tool call details"
            >
              {copied ? <Check className="h-3.5 w-3.5 text-green-600" /> : <Copy className="h-3.5 w-3.5" />}
              <span>{copied ? 'Copied' : 'Copy'}</span>
            </button>
          </div>

          {name === 'Edit' && input.old_string && input.new_string && (
            <div className="mb-4">
              <div className="mb-2 text-xs font-medium uppercase tracking-wide text-gray-500">Code changes</div>
              <CodeDiff
                oldCode={input.old_string as string}
                newCode={input.new_string as string}
                fileName={input.file_path as string}
              />
            </div>
          )}

          {name === 'Read' && input.file_path && (
            <div className="mb-4 text-xs text-gray-600">
              Reading: <span className="break-all font-mono">{input.file_path}</span>
            </div>
          )}

          {name === 'TodoWrite' && input.todos && Array.isArray(input.todos) && (
            <div className="mb-4">
              <div className="mb-2 text-xs font-medium uppercase tracking-wide text-gray-500">Task management</div>
              <TodoList todos={input.todos} />
            </div>
          )}

          {parameterEntries.length > 0 && name !== 'Edit' && name !== 'TodoWrite' && (
            <div>
              <div className="mb-2 flex items-center justify-between gap-3">
                <div className="flex items-center gap-2 text-xs font-medium uppercase tracking-wide text-gray-500">
                  <span>Parameters</span>
                  <span className="rounded-full border border-gray-200 bg-gray-50 px-2 py-0.5 text-[11px] normal-case tracking-normal text-gray-600">
                    {parameterEntries.length}
                  </span>
                </div>
                {parameterEntries.length > 2 && (
                  <button
                    type="button"
                    onClick={() => setShowAllParameters(current => !current)}
                    className="flex items-center gap-1 text-xs text-gray-500 transition-colors hover:text-gray-900"
                  >
                    {showAllParameters ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                    <span>{showAllParameters ? 'Show less' : 'Show all'}</span>
                  </button>
                )}
              </div>
              <div className="overflow-hidden rounded-lg border border-gray-200 bg-gray-50">
                {(showAllParameters ? parameterEntries : parameterEntries.slice(0, 2)).map(([key, value]) => (
                  <div key={key} className="flex items-start gap-3 border-b border-gray-200 px-3 py-2.5 last:border-b-0">
                    <span className="shrink-0 pt-0.5 font-mono text-xs font-medium text-gray-500">{key}</span>
                    <div className="min-w-0 flex-1">{renderParameterValue(key, value)}</div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {text && (
            <div className="mt-3 rounded-lg border border-gray-200 bg-gray-50 p-3">
              <div className="mb-1 text-xs font-medium text-gray-500">Additional information</div>
              <div className="text-sm text-gray-700">{text}</div>
            </div>
          )}

          <div className="mt-3 flex items-center gap-2 text-xs text-gray-400">
            <span className="h-2 w-2 rounded-full bg-gray-300" />
            <span>{statusText}</span>
          </div>
        </div>
      )}
    </div>
  );
}
