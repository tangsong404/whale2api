'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const chatStream = require('./chat-stream.js');
const deepseekConstants = require('../../internal/js/shared/deepseek-constants.js');
const { parseToolCallsDetailed, parseStandaloneToolCallsDetailed } = require('../../internal/js/helpers/stream-tool-sieve.js');

const { parseChunkForContent, estimateTokens } = chatStream.__test;

const compatRoot = path.resolve(__dirname, '../../tests/compat');

function readJSON(filePath) {
  return JSON.parse(fs.readFileSync(filePath, 'utf8'));
}

test('js shared constants derive client headers from shared json', () => {
  const shared = readJSON(path.resolve(__dirname, '../../internal/deepseek/protocol/constants_shared.json'));
  const client = shared.client;
  assert.equal(deepseekConstants.CLIENT_VERSION, client.version);
  assert.equal(deepseekConstants.BASE_HEADERS['x-client-version'], client.version);
  assert.equal(deepseekConstants.BASE_HEADERS['User-Agent'], `${client.name}/${client.version} Android/${client.android_api_level}`);
  assert.equal(deepseekConstants.BASE_HEADERS['Content-Type'], 'application/json');
});

test('js compat: sse fixtures', () => {
  const fixtureDir = path.join(compatRoot, 'fixtures', 'sse_chunks');
  const expectedDir = path.join(compatRoot, 'expected');
  const files = fs.readdirSync(fixtureDir).filter((f) => f.endsWith('.json')).sort();
  assert.ok(files.length > 0);

  for (const file of files) {
    const name = file.replace(/\.json$/i, '');
    const fixture = readJSON(path.join(fixtureDir, file));
    const expected = readJSON(path.join(expectedDir, `sse_${name}.json`));
    const got = parseChunkForContent(fixture.chunk, Boolean(fixture.thinking_enabled), fixture.current_type || 'text');
    assert.deepEqual(got.parts, expected.parts, `${name}: parts mismatch`);
    assert.equal(got.finished, expected.finished, `${name}: finished mismatch`);
    assert.equal(got.newType, expected.new_type, `${name}: newType mismatch`);
    assert.equal(Boolean(got.contentFilter), Boolean(expected.content_filter), `${name}: contentFilter mismatch`);
    assert.equal(got.errorMessage || '', expected.error_message || '', `${name}: errorMessage mismatch`);
  }
});

test('js compat: toolcall fixtures', () => {
  const fixtureDir = path.join(compatRoot, 'fixtures', 'toolcalls');
  const expectedDir = path.join(compatRoot, 'expected');
  const files = fs.readdirSync(fixtureDir).filter((f) => f.endsWith('.json')).sort();
  assert.ok(files.length > 0);

  for (const file of files) {
    const name = file.replace(/\.json$/i, '');
      const fixture = readJSON(path.join(fixtureDir, file));
      const expected = readJSON(path.join(expectedDir, `toolcalls_${name}.json`));
      const mode = typeof fixture.mode === 'string' ? fixture.mode.trim().toLowerCase() : '';
      const parser = mode === 'standalone' ? parseStandaloneToolCallsDetailed : parseToolCallsDetailed;
      const got = parser(fixture.text, fixture.tool_names || []);
      assert.deepEqual(got.calls, expected.calls, `${name}: calls mismatch`);
      assert.equal(got.sawToolCallSyntax, expected.sawToolCallSyntax, `${name}: sawToolCallSyntax mismatch`);
      assert.equal(got.rejectedByPolicy, expected.rejectedByPolicy, `${name}: rejectedByPolicy mismatch`);
      assert.deepEqual(got.rejectedToolNames, expected.rejectedToolNames, `${name}: rejectedToolNames mismatch`);
    }
  });

test('js compat: token fixtures', () => {
  const fixture = readJSON(path.join(compatRoot, 'fixtures', 'token_cases.json'));
  const expected = readJSON(path.join(compatRoot, 'expected', 'token_cases.json'));
  const expectedByName = new Map(expected.cases.map((c) => [c.name, c.tokens]));
  for (const c of fixture.cases) {
    assert.ok(expectedByName.has(c.name), `missing expected case: ${c.name}`);
    const got = estimateTokens(c.text);
    assert.equal(got, expectedByName.get(c.name), `${c.name}: tokens mismatch`);
  }
});
