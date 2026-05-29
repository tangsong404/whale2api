'use strict';

function buildUsage(prompt, thinking, output, outputTokens = 0, providedPromptTokens = 0) {
  const reasoningTokens = estimateTokens(thinking);
  const completionTokens = estimateTokens(output);

  const finalPromptTokens = Number.isFinite(providedPromptTokens) && providedPromptTokens > 0 ? Math.trunc(providedPromptTokens) : estimateTokens(prompt);

  const overriddenCompletionTokens = Number.isFinite(outputTokens) && outputTokens > 0 ? Math.trunc(outputTokens) : 0;
  const finalCompletionTokens = overriddenCompletionTokens > 0 ? overriddenCompletionTokens : reasoningTokens + completionTokens;
  return {
    prompt_tokens: finalPromptTokens,
    completion_tokens: finalCompletionTokens,
    total_tokens: finalPromptTokens + finalCompletionTokens,
    completion_tokens_details: {
      reasoning_tokens: reasoningTokens,
    },
  };
}

function estimateTokens(text) {
  const t = asTokenString(text);
  if (!t) {
    return 0;
  }
  let asciiChars = 0;
  let nonASCIIChars = 0;
  for (const ch of Array.from(t)) {
    if (ch.charCodeAt(0) < 128) {
      asciiChars += 1;
    } else {
      nonASCIIChars += 1;
    }
  }
  const n = Math.floor(asciiChars / 4) + Math.floor((nonASCIIChars * 10 + 7) / 13);
  return n < 1 ? 1 : n;
}

function asTokenString(v) {
  if (typeof v === 'string') {
    return v;
  }
  if (Array.isArray(v)) {
    return asTokenString(v[0]);
  }
  if (v == null) {
    return '';
  }
  return String(v);
}

module.exports = {
  buildUsage,
  estimateTokens,
};
