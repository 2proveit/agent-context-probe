import assert from 'node:assert/strict';
import test from 'node:test';
import { formatJSON, limitDisplayText, normalizeDisplayLimit } from './formatters';

test('raw display is unlimited when the configured limit is zero', () => {
  const value = { content: 'x'.repeat(1500) };
  const formatted = formatJSON(value, 0);

  assert.equal(formatted, JSON.stringify(value, null, 2));
  assert.doesNotMatch(formatted, /\.\.\.$/);
});

test('positive raw display limits truncate only the displayed value', () => {
  const raw = 'abcdefghij';

  assert.equal(limitDisplayText(raw, 5), 'abcde...');
  assert.equal(raw, 'abcdefghij');
});

test('invalid display limits fall back to unlimited', () => {
  assert.equal(normalizeDisplayLimit(undefined), 0);
  assert.equal(normalizeDisplayLimit(-1), 0);
  assert.equal(normalizeDisplayLimit(Number.NaN), 0);
  assert.equal(normalizeDisplayLimit(123.9), 123);
});
