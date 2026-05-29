'use strict';

const MIN_CONTINUATION_SNAPSHOT_LEN = 32;

function trimContinuationOverlap(existing, incoming) {
  if (!incoming) {
    return '';
  }
  if (!existing) {
    return incoming;
  }
  if (incoming.length >= MIN_CONTINUATION_SNAPSHOT_LEN && incoming.startsWith(existing)) {
    return incoming.slice(existing.length);
  }
  if (incoming.length >= MIN_CONTINUATION_SNAPSHOT_LEN && existing.startsWith(incoming)) {
    return '';
  }
  return incoming;
}

module.exports = {
  trimContinuationOverlap,
};
