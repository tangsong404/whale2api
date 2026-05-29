'use strict';

const CDATA_PATTERN = /^<!\[CDATA\[([\s\S]*?)]]>$/i;
const XML_ATTR_PATTERN = /\b([a-z0-9_:-]+)\s*=\s*("([^"]*)"|'([^']*)')/gi;
const TOOL_MARKUP_NAMES = [
  { raw: 'tool_calls', canonical: 'tool_calls' },
  { raw: 'tool-calls', canonical: 'tool_calls', dsmlOnly: true },
  { raw: 'invoke', canonical: 'invoke' },
  { raw: 'parameter', canonical: 'parameter' },
];

const {
  toStringSafe,
} = require('./state');

function stripFencedCodeBlocks(text) {
  const t = typeof text === 'string' ? text : '';
  if (!t) {
    return '';
  }
  const lines = t.split('\n');
  const out = [];
  let inFence = false;
  let fenceChar = '';
  let fenceLen = 0;
  let inCDATA = false;
  let beforeFenceIdx = 0;

  for (let li = 0; li < lines.length; li += 1) {
    const line = lines[li];
    const lineWithNL = li < lines.length - 1 ? line + '\n' : line;

    // CDATA protection
    if (inCDATA || cdataStartsBeforeFence(line)) {
      out.push(lineWithNL);
      inCDATA = updateCDATAStateLine(inCDATA, line);
      continue;
    }

    const trimmed = line.replace(/^[ \t]+/, '');
    if (!inFence) {
      const fence = parseFenceOpenLine(trimmed);
      if (fence) {
        inFence = true;
        fenceChar = fence.ch;
        fenceLen = fence.count;
        beforeFenceIdx = out.length;
        continue;
      }
      out.push(lineWithNL);
      continue;
    }

    if (isFenceCloseLine(trimmed, fenceChar, fenceLen)) {
      inFence = false;
      fenceChar = '';
      fenceLen = 0;
    }
  }

  if (inFence) {
    // Unclosed fence: keep content before the fence started.
    if (beforeFenceIdx > 0) {
      return out.slice(0, beforeFenceIdx).join('');
    }
    return '';
  }
  return out.join('');
}

function parseFenceOpenLine(trimmed) {
  if (trimmed.length < 3) return null;
  const ch = trimmed[0];
  if (ch !== '`' && ch !== '~') return null;
  let count = 0;
  while (count < trimmed.length && trimmed[count] === ch) count++;
  if (count < 3) return null;
  return { ch, count };
}

function isFenceCloseLine(trimmed, fenceChar, fenceLen) {
  if (!fenceChar || !trimmed || trimmed[0] !== fenceChar) return false;
  let count = 0;
  while (count < trimmed.length && trimmed[count] === fenceChar) count++;
  if (count < fenceLen) return false;
  return trimmed.slice(count).trim() === '';
}

function cdataStartsBeforeFence(line) {
  const cdataIdx = line.toLowerCase().indexOf('<![cdata[');
  if (cdataIdx < 0) return false;
  const fenceIdx = Math.min(
    line.indexOf('```') >= 0 ? line.indexOf('```') : Infinity,
    line.indexOf('~~~') >= 0 ? line.indexOf('~~~') : Infinity,
  );
  return fenceIdx === Infinity || cdataIdx < fenceIdx;
}

function updateCDATAStateLine(inCDATA, line) {
  const lower = line.toLowerCase();
  let pos = 0;
  let state = inCDATA;
  while (pos < lower.length) {
    if (state) {
      const end = lower.indexOf(']]>', pos);
      if (end < 0) return true;
      pos = end + ']]>'.length;
      state = false;
      continue;
    }
    const start = lower.indexOf('<![cdata[', pos);
    if (start < 0) return false;
    pos = start + '<![cdata['.length;
    state = true;
  }
  return state;
}

function parseMarkupToolCalls(text) {
  const normalized = normalizeDSMLToolCallMarkup(toStringSafe(text));
  if (!normalized.ok) {
    return [];
  }
  const raw = normalized.text.trim();
  if (!raw) {
    return [];
  }
  const out = [];
  for (const wrapper of findXmlElementBlocks(raw, 'tool_calls')) {
    const body = toStringSafe(wrapper.body);
    for (const block of findXmlElementBlocks(body, 'invoke')) {
      const parsed = parseMarkupSingleToolCall(block);
      if (parsed) {
        out.push(parsed);
      }
    }
  }
  return out;
}

function normalizeDSMLToolCallMarkup(text) {
  const raw = toStringSafe(text);
  if (!raw) {
    return { text: '', ok: true };
  }
  const styles = containsToolMarkupSyntaxOutsideIgnored(raw);
  if (!styles.dsml) {
    return { text: raw, ok: true };
  }
  return {
    text: replaceDSMLToolMarkupOutsideIgnored(raw),
    ok: true,
  };
}

function containsDSMLToolMarkup(text) {
  return containsToolMarkupSyntaxOutsideIgnored(text).dsml;
}

function containsCanonicalToolMarkup(text) {
  return containsToolMarkupSyntaxOutsideIgnored(text).canonical;
}

function containsToolCallWrapperSyntaxOutsideIgnored(text) {
  const raw = toStringSafe(text);
  const styles = { dsml: false, canonical: false };
  if (!raw) {
    return styles;
  }
  const lower = raw.toLowerCase();
  for (let i = 0; i < raw.length;) {
    const skipped = skipXmlIgnoredSection(lower, i);
    if (skipped.blocked) {
      return styles;
    }
    if (skipped.advanced) {
      i = skipped.next;
      continue;
    }
    const tag = scanToolMarkupTagAt(raw, i);
    if (tag) {
      if (tag.name !== 'tool_calls') {
        i = tag.end + 1;
        continue;
      }
      if (tag.dsmlLike) {
        styles.dsml = true;
      } else {
        styles.canonical = true;
      }
      if (styles.dsml && styles.canonical) {
        return styles;
      }
      i = tag.end + 1;
      continue;
    }
    i += 1;
  }
  return styles;
}
function containsToolMarkupSyntaxOutsideIgnored(text) {
  const raw = toStringSafe(text);
  const styles = { dsml: false, canonical: false };
  if (!raw) {
    return styles;
  }
  for (let i = 0; i < raw.length;) {
    const skipped = skipXmlIgnoredSection(raw.toLowerCase(), i);
    if (skipped.blocked) {
      return styles;
    }
    if (skipped.advanced) {
      i = skipped.next;
      continue;
    }
    const tag = scanToolMarkupTagAt(raw, i);
    if (tag) {
      if (tag.dsmlLike) {
        styles.dsml = true;
      } else {
        styles.canonical = true;
      }
      if (styles.dsml && styles.canonical) {
        return styles;
      }
      i = tag.end + 1;
      continue;
    }
    i += 1;
  }
  return styles;
}

function replaceDSMLToolMarkupOutsideIgnored(text) {
  const raw = toStringSafe(text);
  if (!raw) {
    return '';
  }
  const lower = raw.toLowerCase();
  let out = '';
  for (let i = 0; i < raw.length;) {
    const skipped = skipXmlIgnoredSection(lower, i);
    if (skipped.blocked) {
      out += raw.slice(i);
      break;
    }
    if (skipped.advanced) {
      out += raw.slice(i, skipped.next);
      i = skipped.next;
      continue;
    }
    const tag = scanToolMarkupTagAt(raw, i);
    if (tag) {
      if (tag.dsmlLike) {
        out += `<${tag.closing ? '/' : ''}${tag.name}${raw.slice(tag.nameEnd, tag.end + 1)}`;
        if (raw[tag.end] !== '>') {
          out += '>';
        }
      } else {
        out += raw.slice(tag.start, tag.end + 1);
      }
      i = tag.end + 1;
      continue;
    }
    out += raw[i];
    i += 1;
  }
  return out;
}

function parseMarkupSingleToolCall(block) {
  const attrs = parseTagAttributes(block.attrs);
  const name = toStringSafe(attrs.name).trim();
  if (!name) {
    return null;
  }
  const inner = toStringSafe(block.body).trim();

  if (inner) {
    try {
      const decoded = JSON.parse(inner);
      if (decoded && typeof decoded === 'object' && !Array.isArray(decoded)) {
        return {
          name,
          input: decoded.input && typeof decoded.input === 'object' && !Array.isArray(decoded.input)
            ? decoded.input
            : decoded.parameters && typeof decoded.parameters === 'object' && !Array.isArray(decoded.parameters)
              ? decoded.parameters
              : {},
        };
      }
    } catch (_err) {
      // Not JSON, continue with markup parsing.
    }
  }
  const input = {};
  for (const match of findXmlElementBlocks(inner, 'parameter')) {
    const parameterAttrs = parseTagAttributes(match.attrs);
    const paramName = toStringSafe(parameterAttrs.name).trim();
    if (!paramName) {
      continue;
    }
    appendMarkupValue(input, paramName, parseMarkupValue(match.body, paramName));
  }
  if (Object.keys(input).length === 0 && inner.trim() !== '') {
    return null;
  }
  return { name, input };
}

function findXmlElementBlocks(text, tag) {
  const source = toStringSafe(text);
  const name = toStringSafe(tag).toLowerCase();
  if (!source || !name) {
    return [];
  }
  const out = [];
  let pos = 0;
  while (pos < source.length) {
    const start = findXmlStartTagOutsideCDATA(source, name, pos);
    if (!start) {
      break;
    }
    const end = findMatchingXmlEndTagOutsideCDATA(source, name, start.bodyStart);
    if (!end) {
      pos = start.bodyStart;
      continue;
    }
    out.push({
      attrs: start.attrs,
      body: source.slice(start.bodyStart, end.closeStart),
      start: start.start,
      end: end.closeEnd,
    });
    pos = end.closeEnd;
  }
  return out;
}

function findXmlStartTagOutsideCDATA(text, tag, from) {
  const lower = text.toLowerCase();
  const target = `<${tag}`;
  for (let i = Math.max(0, from || 0); i < text.length;) {
    const skipped = skipXmlIgnoredSection(lower, i);
    if (skipped.blocked) {
      return null;
    }
    if (skipped.advanced) {
      i = skipped.next;
      continue;
    }
    if (lower.startsWith(target, i) && hasXmlTagBoundary(text, i + target.length)) {
      const tagEnd = findXmlTagEnd(text, i + target.length);
      if (tagEnd < 0) {
        return null;
      }
      return {
        start: i,
        bodyStart: tagEnd + 1,
        attrs: text.slice(i + target.length, tagEnd),
      };
    }
    i += 1;
  }
  return null;
}

function findMatchingXmlEndTagOutsideCDATA(text, tag, from) {
  const lower = text.toLowerCase();
  const openTarget = `<${tag}`;
  const closeTarget = `</${tag}`;
  let depth = 1;
  for (let i = Math.max(0, from || 0); i < text.length;) {
    const skipped = skipXmlIgnoredSection(lower, i);
    if (skipped.blocked) {
      return null;
    }
    if (skipped.advanced) {
      i = skipped.next;
      continue;
    }
    if (lower.startsWith(closeTarget, i) && hasXmlTagBoundary(text, i + closeTarget.length)) {
      const tagEnd = findXmlTagEnd(text, i + closeTarget.length);
      if (tagEnd < 0) {
        return null;
      }
      depth -= 1;
      if (depth === 0) {
        return { closeStart: i, closeEnd: tagEnd + 1 };
      }
      i = tagEnd + 1;
      continue;
    }
    if (lower.startsWith(openTarget, i) && hasXmlTagBoundary(text, i + openTarget.length)) {
      const tagEnd = findXmlTagEnd(text, i + openTarget.length);
      if (tagEnd < 0) {
        return null;
      }
      if (!isSelfClosingXmlTag(text.slice(i, tagEnd))) {
        depth += 1;
      }
      i = tagEnd + 1;
      continue;
    }
    i += 1;
  }
  return null;
}

function skipXmlIgnoredSection(lower, i) {
  if (lower.startsWith('<![cdata[', i)) {
    const end = lower.indexOf(']]>', i + '<![cdata['.length);
    if (end < 0) {
      return { advanced: false, blocked: true, next: i };
    }
    return { advanced: true, blocked: false, next: end + ']]>'.length };
  }
  if (lower.startsWith('<!--', i)) {
    const end = lower.indexOf('-->', i + '<!--'.length);
    if (end < 0) {
      return { advanced: false, blocked: true, next: i };
    }
    return { advanced: true, blocked: false, next: end + '-->'.length };
  }
  return { advanced: false, blocked: false, next: i };
}

function scanToolMarkupTagAt(text, start) {
  const raw = toStringSafe(text);
  if (!raw || start < 0 || start >= raw.length || raw[start] !== '<') {
    return null;
  }
  const lower = raw.toLowerCase();
  let i = start + 1;
  while (i < raw.length && raw[i] === '<') {
    i += 1;
  }
  const closing = raw[i] === '/';
  if (closing) {
    i += 1;
  }
  const prefix = consumeToolMarkupNamePrefix(raw, lower, i);
  i = prefix.next;
  const dsmlLike = prefix.dsmlLike;
  const { name, len } = matchToolMarkupName(lower, i, dsmlLike);
  if (!name) {
    return null;
  }
  const originalNameEnd = i + len;
  let nameEnd = originalNameEnd;
  while (nameEnd < raw.length && isToolMarkupPipe(raw[nameEnd])) {
    nameEnd += 1;
  }
  const hasTrailingPipe = nameEnd > originalNameEnd;
  if (!hasXmlTagBoundary(raw, nameEnd)) {
    return null;
  }
  let end = findXmlTagEnd(raw, nameEnd);
  if (end < 0) {
    if (!hasTrailingPipe) {
      return null;
    }
    end = nameEnd - 1;
  }
  if (hasTrailingPipe) {
    const nextLT = raw.indexOf('<', nameEnd);
    if (nextLT >= 0 && end >= nextLT) {
      end = nameEnd - 1;
    }
  }
  if (end < 0) {
    return null;
  }
  return {
    start,
    end,
    nameStart: i,
    nameEnd,
    name,
    closing,
    selfClosing: raw.slice(start, end + 1).trim().endsWith('/>'),
    dsmlLike,
    canonical: !dsmlLike,
  };
}

function findToolMarkupTagOutsideIgnored(text, from) {
  const raw = toStringSafe(text);
  const lower = raw.toLowerCase();
  for (let i = Math.max(0, from || 0); i < raw.length;) {
    const skipped = skipXmlIgnoredSection(lower, i);
    if (skipped.blocked) {
      return null;
    }
    if (skipped.advanced) {
      i = skipped.next;
      continue;
    }
    const tag = scanToolMarkupTagAt(raw, i);
    if (tag) {
      return tag;
    }
    i += 1;
  }
  return null;
}

function findMatchingToolMarkupClose(text, openTag) {
  const raw = toStringSafe(text);
  if (!raw || !openTag || !openTag.name || openTag.closing) {
    return null;
  }
  let depth = 1;
  for (let pos = openTag.end + 1; pos < raw.length;) {
    const tag = findToolMarkupTagOutsideIgnored(raw, pos);
    if (!tag) {
      return null;
    }
    if (tag.name !== openTag.name) {
      pos = tag.end + 1;
      continue;
    }
    if (tag.closing) {
      depth -= 1;
      if (depth === 0) {
        return tag;
      }
    } else if (!tag.selfClosing) {
      depth += 1;
    }
    pos = tag.end + 1;
  }
  return null;
}

function findPartialToolMarkupStart(text) {
  const raw = toStringSafe(text);
  const lastLT = raw.lastIndexOf('<');
  if (lastLT < 0) {
    return -1;
  }
  const start = includeDuplicateLeadingLessThan(raw, lastLT);
  const tail = raw.slice(start);
  if (tail.includes('>')) {
    return -1;
  }
  return isPartialToolMarkupTagPrefix(tail) ? start : -1;
}

function includeDuplicateLeadingLessThan(text, idx) {
  let out = idx;
  while (out > 0 && text[out - 1] === '<') {
    out -= 1;
  }
  return out;
}

function isToolMarkupPipe(ch) {
  return ch === '|' || ch === '｜';
}

function isPartialToolMarkupTagPrefix(text) {
  const raw = toStringSafe(text);
  if (!raw || raw[0] !== '<' || raw.includes('>')) {
    return false;
  }
  const lower = raw.toLowerCase();
  let i = 1;
  while (i < raw.length && raw[i] === '<') {
    i += 1;
  }
  if (i >= raw.length) {
    return true;
  }
  if (raw[i] === '/') {
    i += 1;
  }
  while (i <= raw.length) {
    if (i === raw.length) {
      return true;
    }
    if (hasToolMarkupNamePrefix(lower.slice(i))) {
      return true;
    }
    if ('dsml'.startsWith(lower.slice(i))) {
      return true;
    }
    const next = consumeToolMarkupNamePrefixOnce(raw, lower, i);
    if (!next.ok) {
      return false;
    }
    i = next.next;
  }
  return false;
}

function consumeToolMarkupNamePrefix(raw, lower, idx) {
  let next = idx;
  let dsmlLike = false;
  while (true) {
    const consumed = consumeToolMarkupNamePrefixOnce(raw, lower, next);
    if (!consumed.ok) {
      return { next, dsmlLike };
    }
    next = consumed.next;
    dsmlLike = true;
  }
}

function consumeToolMarkupNamePrefixOnce(raw, lower, idx) {
  if (idx < raw.length && isToolMarkupPipe(raw[idx])) {
    return { next: idx + 1, ok: true };
  }
  if (idx < raw.length && [' ', '\t', '\r', '\n'].includes(raw[idx])) {
    return { next: idx + 1, ok: true };
  }
  if (lower.startsWith('dsml', idx)) {
    let next = idx + 'dsml'.length;
    if (next < raw.length && raw[next] === '-') {
      next += 1;
    }
    return { next, ok: true };
  }
  return { next: idx, ok: false };
}

function hasToolMarkupNamePrefix(lowerTail) {
  for (const name of TOOL_MARKUP_NAMES) {
    if (lowerTail.startsWith(name.raw) || name.raw.startsWith(lowerTail)) {
      return true;
    }
  }
  return false;
}

function matchToolMarkupName(lower, start, dsmlLike) {
  for (const name of TOOL_MARKUP_NAMES) {
    if (name.dsmlOnly && !dsmlLike) {
      continue;
    }
    if (lower.startsWith(name.raw, start)) {
      return { name: name.canonical, len: name.raw.length };
    }
  }
  return { name: '', len: 0 };
}

function findXmlTagEnd(text, from) {
  let quote = '';
  for (let i = Math.max(0, from || 0); i < text.length; i += 1) {
    const ch = text[i];
    if (quote) {
      if (ch === quote) {
        quote = '';
      }
      continue;
    }
    if (ch === '"' || ch === "'") {
      quote = ch;
      continue;
    }
    if (ch === '>') {
      return i;
    }
  }
  return -1;
}

function hasXmlTagBoundary(text, idx) {
  if (idx >= text.length) {
    return true;
  }
  return [' ', '\t', '\n', '\r', '>', '/'].includes(text[idx]);
}

function isSelfClosingXmlTag(startTag) {
  return toStringSafe(startTag).trim().endsWith('/');
}

function parseMarkupInput(raw) {
  const s = toStringSafe(raw).trim();
  if (!s) {
    return {};
  }
  // Prioritize XML-style KV tags (e.g., <arg>val</arg>)
  const kv = unwrapItemOnlyMarkupValue(parseMarkupKVObject(s));
  if (Array.isArray(kv)) {
    return kv;
  }
  if (kv && typeof kv === 'object' && Object.keys(kv).length > 0) {
    return kv;
  }

  // Fallback to JSON parsing
  const parsed = parseToolCallInput(s);
  if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
    if (Object.keys(parsed).length > 0) {
      return parsed;
    }
  }

  return { _raw: extractRawTagValue(s) };
}

function parseMarkupKVObject(text) {
  const raw = toStringSafe(text).trim();
  if (!raw) {
    return {};
  }
  const out = {};
  for (const block of findGenericXmlElementBlocks(raw)) {
    const key = toStringSafe(block.localName).trim();
    if (!key) {
      continue;
    }
    const value = parseMarkupValue(block.body, key);
    if (value === undefined || value === null) {
      continue;
    }
    appendMarkupValue(out, key, value);
  }
  return out;
}

function findGenericXmlElementBlocks(text) {
  const source = toStringSafe(text);
  if (!source) {
    return [];
  }
  const out = [];
  let pos = 0;
  while (pos < source.length) {
    const start = findGenericXmlStartTagOutsideCDATA(source, pos);
    if (!start) {
      break;
    }
    if (start.selfClosing) {
      out.push({
        name: start.name,
        localName: start.localName,
        attrs: start.attrs,
        body: '',
        start: start.start,
        end: start.end + 1,
      });
      pos = start.end + 1;
      continue;
    }
    const end = findMatchingGenericXmlEndTagOutsideCDATA(source, start.name, start.bodyStart);
    if (!end) {
      pos = start.bodyStart;
      continue;
    }
    out.push({
      name: start.name,
      localName: start.localName,
      attrs: start.attrs,
      body: source.slice(start.bodyStart, end.closeStart),
      start: start.start,
      end: end.closeEnd,
    });
    pos = end.closeEnd;
  }
  return out;
}

function findGenericXmlStartTagOutsideCDATA(text, from) {
  const lower = text.toLowerCase();
  for (let i = Math.max(0, from || 0); i < text.length;) {
    const skipped = skipXmlIgnoredSection(lower, i);
    if (skipped.blocked) {
      return null;
    }
    if (skipped.advanced) {
      i = skipped.next;
      continue;
    }
    if (text[i] !== '<' || text[i + 1] === '/' || text[i + 1] === '!' || text[i + 1] === '?') {
      i += 1;
      continue;
    }
    const match = text.slice(i + 1).match(/^([A-Za-z_][A-Za-z0-9_.:-]*)/);
    if (!match) {
      i += 1;
      continue;
    }
    const name = match[1];
    const nameEnd = i + 1 + name.length;
    if (!hasXmlTagBoundary(text, nameEnd)) {
      i += 1;
      continue;
    }
    const tagEnd = findXmlTagEnd(text, nameEnd);
    if (tagEnd < 0) {
      return null;
    }
    return {
      start: i,
      end: tagEnd,
      bodyStart: tagEnd + 1,
      name,
      localName: name.includes(':') ? name.slice(name.lastIndexOf(':') + 1) : name,
      attrs: text.slice(nameEnd, tagEnd),
      selfClosing: isSelfClosingXmlTag(text.slice(i, tagEnd)),
    };
  }
  return null;
}

function findMatchingGenericXmlEndTagOutsideCDATA(text, name, from) {
  const lower = text.toLowerCase();
  const needle = toStringSafe(name).toLowerCase();
  if (!needle) {
    return null;
  }
  const openTarget = `<${needle}`;
  const closeTarget = `</${needle}`;
  let depth = 1;
  for (let i = Math.max(0, from || 0); i < text.length;) {
    const skipped = skipXmlIgnoredSection(lower, i);
    if (skipped.blocked) {
      return null;
    }
    if (skipped.advanced) {
      i = skipped.next;
      continue;
    }
    if (lower.startsWith(closeTarget, i) && hasXmlTagBoundary(text, i + closeTarget.length)) {
      const tagEnd = findXmlTagEnd(text, i + closeTarget.length);
      if (tagEnd < 0) {
        return null;
      }
      depth -= 1;
      if (depth === 0) {
        return { closeStart: i, closeEnd: tagEnd + 1 };
      }
      i = tagEnd + 1;
      continue;
    }
    if (lower.startsWith(openTarget, i) && hasXmlTagBoundary(text, i + openTarget.length)) {
      const tagEnd = findXmlTagEnd(text, i + openTarget.length);
      if (tagEnd < 0) {
        return null;
      }
      if (!isSelfClosingXmlTag(text.slice(i, tagEnd))) {
        depth += 1;
      }
      i = tagEnd + 1;
      continue;
    }
    i += 1;
  }
  return null;
}

function parseMarkupValue(raw, paramName = '') {
  const cdata = extractStandaloneCDATA(raw);
  if (cdata.ok) {
    const literal = parseJSONLiteralValue(cdata.value);
    if (literal.ok) {
      const literalArray = coerceArrayValue(literal.value, paramName);
      if (literalArray.ok) {
        return literalArray.value;
      }
      return literal.value;
    }
    const structured = parseStructuredCDATAParameterValue(paramName, cdata.value);
    if (structured.ok) {
      return structured.value;
    }
    const looseArray = parseLooseJSONArrayValue(cdata.value, paramName);
    return looseArray.ok ? looseArray.value : cdata.value;
  }
  const s = toStringSafe(extractRawTagValue(raw)).trim();
  if (!s) {
    return '';
  }

  if (s.includes('<') && s.includes('>')) {
    const nested = unwrapItemOnlyMarkupValue(parseMarkupInput(s));
    if (Array.isArray(nested)) {
      return nested;
    }
    if (nested && typeof nested === 'object') {
      const nestedArray = coerceArrayValue(nested, paramName);
      if (nestedArray.ok) {
        return nestedArray.value;
      }
      if (isOnlyRawValue(nested)) {
        const rawValue = toStringSafe(nested._raw);
        const looseArray = parseLooseJSONArrayValue(rawValue, paramName);
        return looseArray.ok ? looseArray.value : rawValue;
      }
      return nested;
    }
  }

  const literal = parseJSONLiteralValue(s);
  if (literal.ok) {
    const literalArray = coerceArrayValue(literal.value, paramName);
    if (literalArray.ok) {
      return literalArray.value;
    }
    return literal.value;
  }
  const looseArray = parseLooseJSONArrayValue(s, paramName);
  if (looseArray.ok) {
    return looseArray.value;
  }
  return s;
}

function parseStructuredCDATAParameterValue(paramName, raw) {
  if (preservesCDATAStringParameter(paramName)) {
    return { ok: false, value: null };
  }
  const normalized = normalizeCDATAForStructuredParse(raw);
  if (!normalized.includes('<') || !normalized.includes('>')) {
    return { ok: false, value: null };
  }
  if (!cdataFragmentLooksExplicitlyStructured(normalized)) {
    return { ok: false, value: null };
  }
  const parsed = parseMarkupInput(normalized);
  if (Array.isArray(parsed)) {
    return { ok: true, value: parsed };
  }
  if (parsed && typeof parsed === 'object' && !isOnlyRawValue(parsed) && Object.keys(parsed).length > 0) {
    return { ok: true, value: parsed };
  }
  return { ok: false, value: null };
}

function normalizeCDATAForStructuredParse(raw) {
  return unescapeHtml(toStringSafe(raw).replace(/<br\s*\/?>/gi, '\n').trim());
}

function cdataFragmentLooksExplicitlyStructured(raw) {
  const blocks = findGenericXmlElementBlocks(raw);
  if (blocks.length === 0) {
    return false;
  }
  if (blocks.length > 1) {
    return true;
  }
  const block = blocks[0];
  if (toStringSafe(block.localName).trim().toLowerCase() === 'item') {
    return true;
  }
  return findGenericXmlElementBlocks(block.body).length > 0;
}

function preservesCDATAStringParameter(name) {
  return new Set([
    'content',
    'file_content',
    'text',
    'prompt',
    'query',
    'command',
    'cmd',
    'script',
    'code',
    'old_string',
    'new_string',
    'pattern',
    'path',
    'file_path',
  ]).has(toStringSafe(name).trim().toLowerCase());
}

function unwrapItemOnlyMarkupValue(value) {
  if (Array.isArray(value)) {
    return value.map(unwrapItemOnlyMarkupValue);
  }
  if (!value || typeof value !== 'object') {
    return value;
  }
  const keys = Object.keys(value);
  if (keys.length === 1 && keys[0] === 'item') {
    const items = unwrapItemOnlyMarkupValue(value.item);
    return Array.isArray(items) ? items : [items];
  }
  const out = {};
  for (const key of keys) {
    out[key] = unwrapItemOnlyMarkupValue(value[key]);
  }
  return out;
}

function extractRawTagValue(inner) {
  const s = toStringSafe(inner).trim();
  if (!s) {
    return '';
  }

  // 1. Check for CDATA
  const cdata = extractStandaloneCDATA(s);
  if (cdata.ok) {
    return cdata.value;
  }

  // 2. Fallback to unescaping standard HTML entities
  // Note: we avoid broad tag stripping here to preserve user content (like < symbols in code)
  return unescapeHtml(inner);
}

function unescapeHtml(safe) {
  if (!safe) return '';
  return safe.replace(/&amp;/g, '&')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"')
    .replace(/&#039;/g, "'")
    .replace(/&#x27;/g, "'");
}

function extractStandaloneCDATA(inner) {
  const s = toStringSafe(inner).trim();
  const cdataMatch = s.match(CDATA_PATTERN);
  if (cdataMatch && cdataMatch[1] !== undefined) {
    return { ok: true, value: cdataMatch[1] };
  }
  if (s.toLowerCase().startsWith('<![cdata[')) {
    return { ok: true, value: s.slice('<![CDATA['.length) };
  }
  return { ok: false, value: '' };
}

function parseJSONLiteralValue(raw) {
  const s = toStringSafe(raw).trim();
  if (!s) {
    return { ok: false, value: null };
  }
  if (!['{', '[', '"', '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 't', 'f', 'n'].includes(s[0])) {
    return { ok: false, value: null };
  }
  try {
    return { ok: true, value: JSON.parse(s) };
  } catch (_err) {
    return { ok: false, value: null };
  }
}

function parseLooseJSONArrayValue(raw, paramName = '') {
  if (preservesCDATAStringParameter(paramName)) {
    return { ok: false, value: null };
  }
  const s = toStringSafe(raw).trim();
  if (!s) {
    return { ok: false, value: null };
  }
  const candidate = parseLooseJSONArrayCandidate(s, paramName);
  if (candidate.ok) {
    return candidate;
  }

  const segments = splitTopLevelJSONValues(s);
  if (segments.length < 2) {
    return { ok: false, value: null };
  }

  const out = [];
  for (const segment of segments) {
    const parsed = parseLooseArrayElementValue(segment);
    if (!parsed.ok) {
      return { ok: false, value: null };
    }
    out.push(parsed.value);
  }
  return { ok: true, value: out };
}

function parseLooseJSONArrayCandidate(raw, paramName = '') {
  const parsed = parseLooseArrayElementValue(raw);
  if (!parsed.ok) {
    return { ok: false, value: null };
  }
  return coerceArrayValue(parsed.value, paramName);
}

function parseLooseArrayElementValue(raw) {
  const s = toStringSafe(raw).trim();
  if (!s) {
    return { ok: false, value: null };
  }

  const literal = parseJSONLiteralValue(s);
  if (literal.ok) {
    return literal;
  }

  const repairedBackslashes = repairInvalidJSONBackslashes(s);
  if (repairedBackslashes !== s) {
    try {
      const parsed = JSON.parse(repairedBackslashes);
      return { ok: true, value: parsed };
    } catch (_err) {
      // Fall through.
    }
  }

  const repairedLoose = repairLooseJSON(s);
  if (repairedLoose !== s) {
    try {
      const parsed = JSON.parse(repairedLoose);
      return { ok: true, value: parsed };
    } catch (_err) {
      // Fall through.
    }
  }

  if (s.includes('<') && s.includes('>')) {
    const parsed = parseMarkupInput(s);
    if (Array.isArray(parsed)) {
      return { ok: true, value: parsed };
    }
    if (parsed && typeof parsed === 'object') {
      return { ok: true, value: parsed };
    }
  }

  return { ok: false, value: null };
}

function coerceArrayValue(value, paramName = '') {
  if (Array.isArray(value)) {
    return { ok: true, value };
  }
  if (!value || typeof value !== 'object') {
    return { ok: false, value: null };
  }

  const keys = Object.keys(value);
  if (keys.length !== 1) {
    return { ok: false, value: null };
  }

  if (Object.prototype.hasOwnProperty.call(value, 'item')) {
    const items = value.item;
    const nested = coerceArrayValue(items, '');
    return nested.ok ? nested : { ok: true, value: [items] };
  }

  if (paramName && Object.prototype.hasOwnProperty.call(value, paramName)) {
    const nested = coerceArrayValue(value[paramName], '');
    if (nested.ok) {
      return nested;
    }
  }

  return { ok: false, value: null };
}

function splitTopLevelJSONValues(raw) {
  const s = toStringSafe(raw).trim();
  if (!s) {
    return [];
  }

  const values = [];
  let start = 0;
  let depth = 0;
  let inString = false;
  let escaped = false;

  for (let i = 0; i < s.length; i += 1) {
    const ch = s[i];
    if (inString) {
      if (escaped) {
        escaped = false;
        continue;
      }
      if (ch === '\\') {
        escaped = true;
        continue;
      }
      if (ch === '"') {
        inString = false;
      }
      continue;
    }
    if (ch === '"') {
      inString = true;
      continue;
    }
    if (ch === '{' || ch === '[') {
      depth += 1;
      continue;
    }
    if (ch === '}' || ch === ']') {
      if (depth > 0) {
        depth -= 1;
      }
      continue;
    }
    if (ch === ',' && depth === 0) {
      const segment = s.slice(start, i).trim();
      if (!segment) {
        return [];
      }
      values.push(segment);
      start = i + 1;
    }
  }

  const last = s.slice(start).trim();
  if (!last) {
    return [];
  }
  values.push(last);
  return values.length > 1 ? values : [];
}

function repairInvalidJSONBackslashes(s) {
  if (!s || !s.includes('\\')) {
    return s;
  }

  let out = '';
  for (let i = 0; i < s.length; i += 1) {
    const ch = s[i];
    if (ch !== '\\') {
      out += ch;
      continue;
    }
    if (i + 1 < s.length) {
      const next = s[i + 1];
      if ('"\\/bfnrt'.includes(next)) {
        out += `\\${next}`;
        i += 1;
        continue;
      }
      if (next === 'u' && i + 5 < s.length) {
        let isHex = true;
        for (let j = 1; j <= 4; j += 1) {
          const r = s[i + 1 + j];
          if (!/[0-9a-fA-F]/.test(r)) {
            isHex = false;
            break;
          }
        }
        if (isHex) {
          out += `\\u${s.slice(i + 2, i + 6)}`;
          i += 5;
          continue;
        }
      }
    }
    out += '\\\\';
  }
  return out;
}

function repairLooseJSON(s) {
  const raw = toStringSafe(s).trim();
  if (!raw) {
    return raw;
  }
  let out = raw.replace(/([{,]\s*)([a-zA-Z_][a-zA-Z0-9_]*)\s*:/g, '$1"$2":');
  out = out.replace(/(:\s*)(\{(?:[^{}]|\{[^{}]*\})*\}(?:\s*,\s*\{(?:[^{}]|\{[^{}]*\})*\})+)/g, '$1[$2]');
  return out;
}

function sanitizeLooseCDATA(text) {
  const raw = toStringSafe(text);
  if (!raw) {
    return '';
  }
  const lower = raw.toLowerCase();
  const openMarker = '<![cdata[';
  const closeMarker = ']]>';

  let out = '';
  let pos = 0;
  let changed = false;
  while (pos < raw.length) {
    const startRel = lower.indexOf(openMarker, pos);
    if (startRel < 0) {
      out += raw.slice(pos);
      break;
    }
    const start = startRel;
    const contentStart = start + openMarker.length;
    out += raw.slice(pos, start);

    const endRel = lower.indexOf(closeMarker, contentStart);
    if (endRel >= 0) {
      const end = endRel + closeMarker.length;
      out += raw.slice(start, end);
      pos = end;
      continue;
    }

    changed = true;
    out += raw.slice(contentStart);
    pos = raw.length;
  }

  return changed ? out : raw;
}

function parseTagAttributes(raw) {
  const source = toStringSafe(raw);
  const out = {};
  if (!source) {
    return out;
  }
  for (const match of source.matchAll(XML_ATTR_PATTERN)) {
    const key = toStringSafe(match[1]).trim().toLowerCase();
    if (!key) {
      continue;
    }
    out[key] = match[3] || match[4] || '';
  }
  return out;
}

function parseToolCallInput(v) {
  if (v == null) {
    return {};
  }
  if (typeof v === 'string') {
    const raw = toStringSafe(v);
    if (!raw) {
      return {};
    }
    try {
      const parsed = JSON.parse(raw);
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        return parsed;
      }
      return { _raw: raw };
    } catch (_err) {
      return { _raw: raw };
    }
  }
  if (typeof v === 'object' && !Array.isArray(v)) {
    return v;
  }
  try {
    const parsed = JSON.parse(JSON.stringify(v));
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed;
    }
  } catch (_err) {
    return {};
  }
  return {};
}

function appendMarkupValue(out, key, value) {
  if (Object.prototype.hasOwnProperty.call(out, key)) {
    const current = out[key];
    if (Array.isArray(current)) {
      current.push(value);
      return;
    }
    out[key] = [current, value];
    return;
  }
  out[key] = value;
}

function isOnlyRawValue(obj) {
  if (!obj || typeof obj !== 'object' || Array.isArray(obj)) {
    return false;
  }
  const keys = Object.keys(obj);
  return keys.length === 1 && keys[0] === '_raw';
}

module.exports = {
  stripFencedCodeBlocks,
  parseMarkupToolCalls,
  normalizeDSMLToolCallMarkup,
  containsToolMarkupSyntaxOutsideIgnored,
  containsToolCallWrapperSyntaxOutsideIgnored,
  findToolMarkupTagOutsideIgnored,
  findMatchingToolMarkupClose,
  findPartialToolMarkupStart,
  sanitizeLooseCDATA,
};
