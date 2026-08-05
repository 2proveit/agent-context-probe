import assert from 'node:assert/strict';
import test from 'node:test';
import { renderToStaticMarkup } from 'react-dom/server';
import { CodeViewer } from './CodeViewer';
import { ToolResult } from './ToolResult';

test('WebSearch output renders as escaped text instead of code', () => {
  const content = `${'# Search Results for "2026年全球最佳AI大模型排名"\n'}${'function calling is supported. '.repeat(10)}<img src=x onerror=alert(1)>`;
  const markup = renderToStaticMarkup(<ToolResult content={content} toolName="WebSearch" />);

  assert.match(markup, />Text</);
  assert.doesNotMatch(markup, />Code</);
  assert.match(markup, /# Search Results/);
  assert.match(markup, /&lt;img src=x onerror=alert\(1\)&gt;/);
  assert.doesNotMatch(markup, /<img src=x/);
});

test('code highlighting never rewrites generated class attributes', () => {
  const content = '# Search Results for "2026年全球最佳AI大模型排名"';
  const markup = renderToStaticMarkup(<CodeViewer code={content} />);

  assert.match(markup, /class="text-gray-500 italic"/);
  assert.doesNotMatch(markup, /text-gray-class/);
  assert.doesNotMatch(markup, /<span <span/);
});

test('CodeViewer renders lines that begin with unstyled text', () => {
  const content = 'plain text\n  indentedValue = 1;\n= assignment';
  const markup = renderToStaticMarkup(<CodeViewer code={content} />);

  assert.match(markup, /plain text/);
  assert.match(markup, /indentedValue/);
  assert.match(markup, /assignment/);
});
