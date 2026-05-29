'use strict';

const MIN_DELTA_FLUSH_CHARS = 16;
const MAX_DELTA_FLUSH_WAIT_MS = 20;

function createChatCompletionEmitter({ res, sessionID, created, model, isClosed }) {
  let firstChunkSent = false;

  const sendFrame = (obj) => {
    if (isClosed() || res.writableEnded || res.destroyed) {
      return;
    }
    res.write(`data: ${JSON.stringify(obj)}\n\n`);
    if (typeof res.flush === 'function') {
      res.flush();
    }
  };

  const sendDeltaFrame = (delta) => {
    const payloadDelta = { ...delta };
    if (!firstChunkSent) {
      payloadDelta.role = 'assistant';
      firstChunkSent = true;
    }
    sendFrame({
      id: sessionID,
      object: 'chat.completion.chunk',
      created,
      model,
      choices: [{ delta: payloadDelta, index: 0 }],
    });
  };

  return {
    sendFrame,
    sendDeltaFrame,
  };
}

function createDeltaCoalescer({ sendDeltaFrame, minFlushChars = MIN_DELTA_FLUSH_CHARS, maxFlushWaitMS = MAX_DELTA_FLUSH_WAIT_MS }) {
  let pendingField = '';
  let pendingText = '';
  let flushTimer = null;

  const clearFlushTimer = () => {
    if (flushTimer) {
      clearTimeout(flushTimer);
      flushTimer = null;
    }
  };

  const flush = () => {
    clearFlushTimer();
    if (!pendingField || !pendingText) {
      return;
    }
    const delta = { [pendingField]: pendingText };
    pendingField = '';
    pendingText = '';
    sendDeltaFrame(delta);
  };

  const scheduleFlush = () => {
    if (flushTimer || maxFlushWaitMS <= 0) {
      return;
    }
    flushTimer = setTimeout(flush, maxFlushWaitMS);
    if (typeof flushTimer.unref === 'function') {
      flushTimer.unref();
    }
  };

  const append = (field, text) => {
    if (!field || !text) {
      return;
    }
    if (pendingField && pendingField !== field) {
      flush();
    }
    pendingField = field;
    pendingText += text;
    if ([...pendingText].length >= minFlushChars) {
      flush();
      return;
    }
    scheduleFlush();
  };

  return {
    append,
    flush,
  };
}

module.exports = {
  createChatCompletionEmitter,
  createDeltaCoalescer,
};
