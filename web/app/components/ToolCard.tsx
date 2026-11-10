import { useState } from "react";
import { Check, Copy, Settings, Wrench } from "lucide-react";
import { formatJSON, formatJSONForCopy } from "../utils/formatters";

interface ToolDefinition {
  name?: string;
  type?: string;
  description?: string;
  parameters?: unknown;
  input_schema?: unknown;
  function?: { name?: string; description?: string; parameters?: unknown };
  tools?: ToolDefinition[];
}

export default function ToolCard({
  tool,
  index,
  namespacePath = "",
}: {
  tool: ToolDefinition;
  index: number;
  namespacePath?: string;
}) {
  const [expanded, setExpanded] = useState(false);
  const [copiedSchema, setCopiedSchema] = useState(false);
  const localName =
    tool.name || tool.function?.name || tool.type || "Unknown Tool";
  const toolName = namespacePath ? `${namespacePath}.${localName}` : localName;
  const isNamespace = tool.type === "namespace";
  const children =
    isNamespace && Array.isArray(tool.tools)
      ? tool.tools.filter(
          (child) => child && typeof child === "object" && !Array.isArray(child)
        )
      : [];
  const description = tool.description || tool.function?.description || "";
  const schema = isNamespace
    ? undefined
    : (tool.input_schema ?? tool.parameters ?? tool.function?.parameters);

  const handleCopySchema = async () => {
    try {
      await navigator.clipboard.writeText(formatJSONForCopy(schema));
      setCopiedSchema(true);
      setTimeout(() => setCopiedSchema(false), 2000);
    } catch (error) {
      console.error("Failed to copy schema:", error);
    }
  };

  // Parse description to identify code blocks and format them
  const formatDescription = (description: string) => {
    // Split by code blocks (text between backticks)
    const parts = description.split(/(`[^`]+`)/g);

    return parts.map((part, i) => {
      if (part.startsWith("`") && part.endsWith("`")) {
        // Code inline
        const code = part.slice(1, -1);
        return (
          <code
            key={i}
            className="bg-gray-100 text-gray-800 px-1.5 py-0.5 rounded text-xs font-mono"
          >
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
    <div
      data-tool-definition-name={toolName}
      className="overflow-hidden rounded-lg border border-gray-200 bg-gray-50/60"
    >
      <div className="p-4">
        <div className="mb-3 flex items-start justify-between">
          <div className="flex items-center gap-3">
            <div className="flex h-8 w-8 items-center justify-center rounded-md border border-gray-200 bg-white">
              <Wrench className="h-4 w-4 text-gray-500" />
            </div>
            <div>
              <h5 className="break-all text-sm font-semibold text-gray-900">
                {toolName}
              </h5>
              <span className="text-xs text-gray-500">
                {isNamespace
                  ? `Namespace · ${children.length} tools`
                  : `Tool #${index + 1}`}
              </span>
            </div>
          </div>
        </div>

        <div className="prose prose-sm max-w-none">
          <div className="text-sm text-gray-700 leading-relaxed space-y-2">
            <div className="whitespace-pre-wrap">
              {formatDescription(displayDescription)}
              {isLongDescription && !expanded && "..."}
            </div>
            {isLongDescription && (
              <button
                onClick={() => setExpanded(!expanded)}
                className="text-blue-600 hover:text-blue-700 text-xs font-medium mt-2"
              >
                {expanded ? "Show less" : "Show more"}
              </button>
            )}
          </div>
        </div>

        {isNamespace && (
          <div className="mt-4 space-y-3 border-l-2 border-gray-200 pl-3">
            {children.length ? (
              children.map((child, childIndex) => (
                <ToolCard
                  key={childIndex}
                  tool={child}
                  index={childIndex}
                  namespacePath={toolName}
                />
              ))
            ) : (
              <p className="text-xs text-gray-500">
                No tool definitions captured in this namespace.
              </p>
            )}
          </div>
        )}

        {schema != null && (
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
                  {formatJSON(schema, 0)}
                </pre>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
