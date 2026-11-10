import type { RequestRecord } from "../types";

export interface SystemInstruction {
  role: "system" | "developer";
  source: string;
  text: string;
  cacheControl?: string;
}

function instructionText(content: unknown): string {
  if (typeof content === "string") return content;
  if (Array.isArray(content)) {
    return content.map(instructionText).filter(Boolean).join("\n\n");
  }
  if (content && typeof content === "object" && "text" in content) {
    return typeof content.text === "string" ? content.text : "";
  }
  return "";
}

/** Read captured instructions without changing message roles or request history. */
export function getSystemInstructions(
  body: RequestRecord["body"]
): SystemInstruction[] {
  const instructions: SystemInstruction[] = [];
  const add = (
    role: SystemInstruction["role"],
    source: string,
    content: unknown,
    cacheControl?: string
  ) => {
    const text = instructionText(content);
    if (text.trim()) instructions.push({ role, source, text, cacheControl });
  };

  if (Array.isArray(body?.system)) {
    body.system.forEach((block, index) => {
      add("system", `system[${index}]`, block, block?.cache_control?.type);
    });
  } else {
    add("system", "system", body?.system);
  }
  add("system", "instructions", body?.instructions);

  for (const source of ["messages", "input"] as const) {
    const messages = body?.[source];
    if (!Array.isArray(messages)) continue;
    messages.forEach((message, index) => {
      if (message?.role === "system" || message?.role === "developer") {
        add(message.role, `${source}[${index}]`, message.content);
      }
    });
  }
  return instructions;
}
