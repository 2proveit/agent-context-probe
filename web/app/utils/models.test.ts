import assert from "node:assert/strict";
import test from "node:test";
import { getUsage } from "./models";

test("normalizes OpenAI Chat Completions cached input usage", () => {
  const usage = getUsage({
    usage: {
      prompt_tokens: 100,
      completion_tokens: 20,
      total_tokens: 120,
      prompt_tokens_details: { cached_tokens: 40 },
    },
  });

  assert.equal(usage?.contextInput, 100);
  assert.equal(usage?.cached, 40);
  assert.equal(usage?.cacheAvailable, true);
  assert.equal(usage?.prefixCacheHitRate, 0.4);
});

test("normalizes OpenAI Responses cached input usage", () => {
  const usage = getUsage({
    usage: {
      input_tokens: 200,
      output_tokens: 25,
      input_tokens_details: { cached_tokens: 150 },
    },
  });

  assert.equal(usage?.contextInput, 200);
  assert.equal(usage?.total, 225);
  assert.equal(usage?.prefixCacheHitRate, 0.75);
});

test("includes Anthropic cache read and creation tokens in normalized context input", () => {
  const usage = getUsage({
    usage: {
      input_tokens: 10,
      output_tokens: 5,
      cache_read_input_tokens: 80,
      cache_creation_input_tokens: 10,
    },
  });

  assert.equal(usage?.input, 10);
  assert.equal(usage?.contextInput, 100);
  assert.equal(usage?.cached, 80);
  assert.equal(usage?.total, 105);
  assert.equal(usage?.prefixCacheHitRate, 0.8);
});

test("keeps unavailable cache metrics distinct from a measured zero", () => {
  const unavailable = getUsage({
    usage: { input_tokens: 50, output_tokens: 5 },
  });
  const measuredZero = getUsage({
    usage: {
      input_tokens: 50,
      output_tokens: 5,
      input_tokens_details: { cached_tokens: 0 },
    },
  });

  assert.equal(unavailable?.cacheAvailable, false);
  assert.equal(unavailable?.prefixCacheHitRate, null);
  assert.equal(measuredZero?.cacheAvailable, true);
  assert.equal(measuredZero?.prefixCacheHitRate, 0);
});
