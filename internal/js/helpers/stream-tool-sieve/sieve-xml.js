'use strict';
const { parseToolCalls } = require('./parse');
const {
  findToolMarkupTagOutsideIgnored,
  findMatchingToolMarkupClose,
  findPartialToolMarkupStart,
} = require('./parse_payload');

function consumeXMLToolCapture(captured, toolNames, trimWrappingJSONFence) {
  let anyOpenFound = false;
  let best = null;
  let rejected = null;

  // Scan every recognized wrapper occurrence. Prose can mention a wrapper tag
  // before the actual tool block, including the same variant as the real block.
  for (let searchFrom = 0; searchFrom < captured.length;) {
    const openTag = findFirstToolTag(captured, searchFrom, 'tool_calls', false);
    if (!openTag) {
      break;
    }
    const closeTag = findMatchingToolMarkupClose(captured, openTag);
    if (!closeTag) {
      anyOpenFound = true;
      searchFrom = openTag.end + 1;
      continue;
    }
    const xmlBlock = captured.slice(openTag.start, closeTag.end + 1);
    const prefixPart = captured.slice(0, openTag.start);
    const suffixPart = captured.slice(closeTag.end + 1);
    const parsed = parseToolCalls(xmlBlock, toolNames);
    if (Array.isArray(parsed) && parsed.length > 0) {
      const trimmedFence = trimWrappingJSONFence(prefixPart, suffixPart);
      if (!best || openTag.start < best.start) {
        best = {
          start: openTag.start,
          prefix: trimmedFence.prefix,
          calls: parsed,
          suffix: trimmedFence.suffix,
        };
      }
      break;
    }
    if (!rejected || openTag.start < rejected.start) {
      rejected = {
        start: openTag.start,
        prefix: prefixPart + xmlBlock,
        suffix: suffixPart,
      };
    }
    searchFrom = openTag.end + 1;
  }
  if (best) {
    return { ready: true, prefix: best.prefix, calls: best.calls, suffix: best.suffix };
  }
  if (anyOpenFound) {
    // At least one opening tag was found but none had a matching close tag.
    return { ready: false, prefix: '', calls: [], suffix: '' };
  }
  if (rejected) {
    // If this block failed to become a tool call, pass it through as text.
    return { ready: true, prefix: rejected.prefix, calls: [], suffix: rejected.suffix };
  }
  const invokeTag = findFirstToolTag(captured, 0, 'invoke', false);
  if (invokeTag) {
    const wrapperOpen = findFirstToolTag(captured, 0, 'tool_calls', false);
    if (!wrapperOpen || wrapperOpen.start > invokeTag.start) {
      const closeTag = findFirstToolTag(captured, invokeTag.start + 1, 'tool_calls', true);
      if (closeTag && closeTag.start > invokeTag.start) {
        const xmlBlock = '<tool_calls>' + captured.slice(invokeTag.start, closeTag.end + 1);
        const prefixPart = captured.slice(0, invokeTag.start);
        const suffixPart = captured.slice(closeTag.end + 1);
        const parsed = parseToolCalls(xmlBlock, toolNames);
        if (Array.isArray(parsed) && parsed.length > 0) {
          const trimmedFence = trimWrappingJSONFence(prefixPart, suffixPart);
          return {
            ready: true,
            prefix: trimmedFence.prefix,
            calls: parsed,
            suffix: trimmedFence.suffix,
          };
        }
        return { ready: true, prefix: prefixPart + captured.slice(invokeTag.start, closeTag.end + 1), calls: [], suffix: suffixPart };
      }
    }
  }
  return { ready: false, prefix: '', calls: [], suffix: '' };
}

function hasOpenXMLToolTag(captured) {
  for (let pos = 0; pos < captured.length;) {
    const tag = findFirstToolTag(captured, pos, 'tool_calls', false);
    if (!tag) {
      return false;
    }
    if (!findMatchingToolMarkupClose(captured, tag)) {
      return true;
    }
    pos = tag.end + 1;
  }
  return false;
}

function findFirstToolTag(text, from, name, closing) {
  for (let pos = Math.max(0, from || 0); pos < text.length;) {
    const tag = findToolMarkupTagOutsideIgnored(text, pos);
    if (!tag) {
      return null;
    }
    if (tag.name === name && tag.closing === closing) {
      return tag;
    }
    pos = tag.end + 1;
  }
  return null;
}

module.exports = {
  consumeXMLToolCapture,
  hasOpenXMLToolTag,
  findPartialXMLToolTagStart: findPartialToolMarkupStart,
};
