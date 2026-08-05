export interface CodeToken {
  text: string;
  className?: string;
}

const CODE_FILE_EXTENSION = /\.(js|jsx|ts|tsx|py|rb|go|rs|java|cpp|c|h|hpp|cs|php|swift|kt|scala|r|sh|bash|zsh|fish|ps1|sql|html|htm|css|scss|sass|less|json|yaml|yml|toml|md|mdx|xml|tex)$/i;
const READ_TOOL_NAME = /(?:^|[_:.])read(?:_?file)?$/i;
const KEYWORDS = new Set([
  'function', 'const', 'let', 'var', 'if', 'else', 'for', 'while', 'return',
  'class', 'import', 'export', 'from', 'async', 'await', 'def', 'elif',
  'except', 'finally', 'lambda', 'with', 'as', 'raise', 'del', 'global',
  'nonlocal', 'assert', 'break', 'continue', 'try', 'catch', 'throw', 'new',
  'this', 'super', 'extends', 'implements', 'interface', 'abstract', 'static',
  'public', 'private', 'protected', 'void', 'int', 'string', 'boolean', 'float',
  'double', 'char', 'long', 'short', 'byte', 'enum', 'struct', 'typedef',
  'union', 'namespace', 'using', 'package', 'goto', 'switch', 'case', 'default',
]);
const BOOLEAN_OR_NULL = new Set([
  'true', 'false', 'null', 'undefined', 'nil', 'None', 'True', 'False',
]);

interface CodeResultOptions {
  content: string;
  toolName?: string;
  fileName?: string;
}

export function isCodeToolResult({ content, toolName, fileName }: CodeResultOptions): boolean {
  if (/^\s*\d+→/m.test(content)) return true;
  if (fileName && CODE_FILE_EXTENSION.test(fileName)) return true;

  const normalizedToolName = toolName?.replace(/__+/g, '_');
  return normalizedToolName ? READ_TOOL_NAME.test(normalizedToolName) : false;
}

function appendToken(tokens: CodeToken[], text: string, className?: string) {
  if (!text) return;
  const previous = tokens[tokens.length - 1];
  if (previous && previous.className === className) {
    previous.text += text;
    return;
  }
  tokens.push({ text, className });
}

export function tokenizeCodeLine(line: string): CodeToken[] {
  const tokens: CodeToken[] = [];
  let index = 0;

  while (index < line.length) {
    if (line.startsWith('//', index) || line[index] === '#') {
      appendToken(tokens, line.slice(index), 'text-gray-500 italic');
      break;
    }

    if (line.startsWith('/*', index)) {
      const commentEnd = line.indexOf('*/', index + 2);
      const end = commentEnd === -1 ? line.length : commentEnd + 2;
      appendToken(tokens, line.slice(index, end), 'text-gray-500 italic');
      index = end;
      continue;
    }

    const quote = line[index];
    if (quote === '"' || quote === "'" || quote === '`') {
      let end = index + 1;
      while (end < line.length) {
        if (line[end] === '\\') {
          end += 2;
          continue;
        }
        if (line[end] === quote) {
          end += 1;
          break;
        }
        end += 1;
      }
      appendToken(tokens, line.slice(index, end), 'text-green-400');
      index = end;
      continue;
    }

    const remaining = line.slice(index);
    const numberMatch = remaining.match(/^\d+(?:\.\d+)?\b/);
    if (numberMatch) {
      appendToken(tokens, numberMatch[0], 'text-purple-400');
      index += numberMatch[0].length;
      continue;
    }

    const identifierMatch = remaining.match(/^[A-Za-z_$][\w$]*/);
    if (identifierMatch) {
      const identifier = identifierMatch[0];
      const followingText = remaining.slice(identifier.length);
      let className: string | undefined;

      if (KEYWORDS.has(identifier)) {
        className = 'text-blue-400';
      } else if (BOOLEAN_OR_NULL.has(identifier)) {
        className = 'text-orange-400';
      } else if (/^\s*\(/.test(followingText)) {
        className = 'text-yellow-400';
      } else if (/^[A-Z][a-zA-Z0-9]*$/.test(identifier)) {
        className = 'text-cyan-400';
      }

      appendToken(tokens, identifier, className);
      index += identifier.length;
      continue;
    }

    appendToken(tokens, line[index]);
    index += 1;
  }

  return tokens;
}
