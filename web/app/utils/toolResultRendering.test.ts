import assert from 'node:assert/strict';
import test from 'node:test';
import { isCodeToolResult, tokenizeCodeLine } from './toolResultRendering';

test('WebSearch prose containing code-related words remains text', () => {
  const content = `${'# Search Results for "2026年全球最佳AI大模型排名"\n\n'}${'function calling is supported. '.repeat(10)}`;

  assert.equal(isCodeToolResult({ content, toolName: 'WebSearch' }), false);
});

test('explicit file-reading results and numbered source output remain code', () => {
  assert.equal(isCodeToolResult({ content: 'plain content', toolName: 'Read' }), true);
  assert.equal(isCodeToolResult({ content: 'plain content', toolName: 'mcp__files__read_file' }), true);
  assert.equal(isCodeToolResult({ content: '1→const value = 1;', toolName: 'Bash' }), true);
  assert.equal(isCodeToolResult({ content: 'plain content', fileName: 'example.ts' }), true);
});

test('tokenization preserves source text without generating nested HTML', () => {
  const line = '# Search Results for "2026年全球最佳AI大模型排名"';
  const tokens = tokenizeCodeLine(line);

  assert.equal(tokens.map(token => token.text).join(''), line);
  assert.deepEqual(tokens, [{ text: line, className: 'text-gray-500 italic' }]);
  assert.equal(tokens.some(token => token.text.includes('<span')), false);
});

test('tokenization classifies code without changing its text', () => {
  const line = 'const answer = buildResult(500, true);';
  const tokens = tokenizeCodeLine(line);

  assert.equal(tokens.map(token => token.text).join(''), line);
  assert.deepEqual(
    tokens.filter(token => token.className).map(token => [token.text, token.className]),
    [
      ['const', 'text-blue-700'],
      ['buildResult', 'text-amber-700'],
      ['500', 'text-violet-700'],
      ['true', 'text-orange-700'],
    ],
  );
});

test('tokenization accepts lines whose first token is unstyled', () => {
  const lines = [
    'plain text',
    '  indentedValue = 1;',
    '= assignment',
  ];

  for (const line of lines) {
    const tokens = tokenizeCodeLine(line);
    assert.equal(tokens.map(token => token.text).join(''), line);
  }
});
