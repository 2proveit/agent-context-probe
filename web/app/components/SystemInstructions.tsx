import { ChevronDown, Cpu } from "lucide-react";
import type { SystemInstruction } from "../utils/systemInstructions";

export default function SystemInstructions({
  instructions,
  context,
  showEmpty = false,
}: {
  instructions: SystemInstruction[];
  context?: string;
  showEmpty?: boolean;
}) {
  if (!instructions.length && !showEmpty) return null;

  return (
    <details
      data-testid="system-instructions"
      className="group/instructions overflow-hidden rounded-lg border border-amber-200 bg-white shadow-sm"
    >
      <summary className="flex cursor-pointer list-none items-center justify-between gap-3 bg-amber-50/60 px-4 py-3 text-sm font-semibold text-gray-900 hover:bg-amber-50 [&::-webkit-details-marker]:hidden">
        <span className="flex flex-wrap items-center gap-2">
          <Cpu className="h-4 w-4 text-amber-600" />
          <span>System Instructions</span>
          <span className="rounded-full border border-amber-200 px-2 py-0.5 text-[11px] font-medium text-amber-700">
            {instructions.length} {instructions.length === 1 ? "item" : "items"}
          </span>
          {context ? (
            <span className="text-xs font-normal text-gray-500">{context}</span>
          ) : null}
        </span>
        <ChevronDown className="h-4 w-4 shrink-0 text-gray-400 transition-transform group-open/instructions:rotate-180" />
      </summary>
      <div className="max-h-[min(60dvh,640px)] space-y-3 overflow-y-auto border-t border-amber-200 p-4 scrollbar-custom">
        {instructions.length ? (
          instructions.map((instruction, index) => (
            <div
              key={index}
              className="rounded-lg border border-amber-200 bg-amber-50/40 p-4"
            >
              <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
                <span className="text-sm font-medium text-gray-800">
                  {instruction.role === "developer" ? "Developer" : "System"}{" "}
                  Message #{index + 1}
                </span>
                <span className="font-mono text-xs text-gray-500">
                  {instruction.source}
                </span>
                {instruction.cacheControl ? (
                  <span className="rounded-full border border-orange-200 bg-orange-100 px-2 py-1 text-xs text-orange-700">
                    Cache: {instruction.cacheControl}
                  </span>
                ) : null}
              </div>
              <pre className="whitespace-pre-wrap break-words rounded border border-gray-200 bg-white p-3 text-sm leading-6 text-gray-700 [overflow-wrap:anywhere]">
                {instruction.text}
              </pre>
            </div>
          ))
        ) : (
          <p className="text-sm text-gray-500">
            No system or developer instructions in this captured request.
          </p>
        )}
      </div>
    </details>
  );
}
