'use strict';

function createToolSieveState() {
  return {
    pending: '',
    capture: '',
    capturing: false,
    codeFenceStack: [],
    codeFencePendingTicks: 0,
    codeFencePendingTildes: 0,
    codeFenceLineStart: true,
    pendingToolRaw: '',
    pendingToolCalls: [],
    disableDeltas: false,
    toolNameSent: false,
    toolName: '',
    toolArgsStart: -1,
    toolArgsSent: -1,
    toolArgsString: false,
    toolArgsDone: false,
  };
}

function resetIncrementalToolState(state) {
  state.disableDeltas = false;
  state.toolNameSent = false;
  state.toolName = '';
  state.toolArgsStart = -1;
  state.toolArgsSent = -1;
  state.toolArgsString = false;
  state.toolArgsDone = false;
}

function noteText(state, text) {
  if (!state || !hasMeaningfulText(text)) {
    return;
  }
  updateCodeFenceState(state, text);
}

function looksLikeToolExampleContext(text) {
  return insideCodeFence(text);
}

function insideCodeFence(text) {
  const t = typeof text === 'string' ? text : '';
  if (!t) {
    return false;
  }
  return simulateCodeFenceState([], 0, 0, true, t).stack.length > 0;
}

function insideCodeFenceWithState(state, text) {
  if (!state) {
    return insideCodeFence(text);
  }
  const simulated = simulateCodeFenceState(
    Array.isArray(state.codeFenceStack) ? state.codeFenceStack : [],
    Number.isInteger(state.codeFencePendingTicks) ? state.codeFencePendingTicks : 0,
    Number.isInteger(state.codeFencePendingTildes) ? state.codeFencePendingTildes : 0,
    state.codeFenceLineStart !== false,
    text,
  );
  return simulated.stack.length > 0;
}

function updateCodeFenceState(state, text) {
  if (!state) {
    return;
  }
  const next = simulateCodeFenceState(
    Array.isArray(state.codeFenceStack) ? state.codeFenceStack : [],
    Number.isInteger(state.codeFencePendingTicks) ? state.codeFencePendingTicks : 0,
    Number.isInteger(state.codeFencePendingTildes) ? state.codeFencePendingTildes : 0,
    state.codeFenceLineStart !== false,
    text,
  );
  state.codeFenceStack = next.stack;
  state.codeFencePendingTicks = next.pendingTicks;
  state.codeFencePendingTildes = next.pendingTildes;
  state.codeFenceLineStart = next.lineStart;
}

function simulateCodeFenceState(stack, pendingTicks, pendingTildes, lineStart, text) {
  const chunk = typeof text === 'string' ? text : '';
  const nextStack = Array.isArray(stack) ? [...stack] : [];
  let ticks = Number.isInteger(pendingTicks) ? pendingTicks : 0;
  let tildes = Number.isInteger(pendingTildes) ? pendingTildes : 0;
  let atLineStart = lineStart !== false;

  const flushPending = () => {
    if (ticks > 0) {
      if (atLineStart && ticks >= 3) {
        applyFenceMarker(nextStack, ticks); // positive = backtick
      }
      atLineStart = false;
      ticks = 0;
    }
    if (tildes > 0) {
      if (atLineStart && tildes >= 3) {
        applyFenceMarker(nextStack, -tildes); // negative = tilde
      }
      atLineStart = false;
      tildes = 0;
    }
  };

  for (let i = 0; i < chunk.length; i += 1) {
    const ch = chunk[i];
    if (ch === '`') {
      if (tildes > 0) {
        flushPending();
      }
      ticks += 1;
      continue;
    }
    if (ch === '~') {
      if (ticks > 0) {
        flushPending();
      }
      tildes += 1;
      continue;
    }
    flushPending();
    if (ch === '\n' || ch === '\r') {
      atLineStart = true;
      continue;
    }
    if ((ch === ' ' || ch === '\t') && atLineStart) {
      continue;
    }
    atLineStart = false;
  }
  return {
    stack: nextStack,
    pendingTicks: ticks,
    pendingTildes: tildes,
    lineStart: atLineStart,
  };
}

// Positive values = backtick fences, negative = tilde fences.
// Closing must match fence type.
function applyFenceMarker(stack, marker) {
  if (!Array.isArray(stack)) {
    return;
  }
  if (stack.length === 0) {
    stack.push(marker);
    return;
  }
  const top = stack[stack.length - 1];
  const sameType = (top > 0 && marker > 0) || (top < 0 && marker < 0);
  if (!sameType) {
    stack.push(marker);
    return;
  }
  const absMarker = Math.abs(marker);
  const absTop = Math.abs(top);
  if (absMarker >= absTop) {
    stack.pop();
    return;
  }
  stack.push(marker);
}

function hasMeaningfulText(text) {
  return toStringSafe(text) !== '';
}

function toStringSafe(v) {
  if (typeof v === 'string') {
    return v.trim();
  }
  if (Array.isArray(v)) {
    return toStringSafe(v[0]);
  }
  if (v == null) {
    return '';
  }
  return String(v).trim();
}

module.exports = {
  createToolSieveState,
  resetIncrementalToolState,
  noteText,
  looksLikeToolExampleContext,
  insideCodeFence,
  insideCodeFenceWithState,
  updateCodeFenceState,
  hasMeaningfulText,
  toStringSafe,
};
