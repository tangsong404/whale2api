'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const {
  extractToolNames,
  createToolSieveState,
  processToolSieveChunk,
  flushToolSieve,
  parseToolCalls,
  parseToolCallsDetailed,
  parseStandaloneToolCalls,
  formatOpenAIStreamToolCalls,
} = require('../../internal/js/helpers/stream-tool-sieve.js');

function runSieve(chunks, toolNames) {
  const state = createToolSieveState();
  const events = [];
  for (const chunk of chunks) {
    events.push(...processToolSieveChunk(state, chunk, toolNames));
  }
  events.push(...flushToolSieve(state, toolNames));
  return events;
}

function collectText(events) {
  return events
    .filter((evt) => evt.type === 'text' && evt.text)
    .map((evt) => evt.text)
    .join('');
}

test('extractToolNames keeps only declared tool names (Go parity)', () => {
  const names = extractToolNames([
    { function: { description: 'no name tool' } },
    { function: { name: ' read_file ' } },
    { function: { name: 'read_file' } },
    {},
  ]);
  assert.deepEqual(names, ['read_file']);
});

test('parseToolCalls parses XML markup tool call', () => {
  const payload = '<tool_calls><invoke name="read_file"><parameter name="path">README.MD</parameter></invoke></tool_calls>';
  const calls = parseToolCalls(payload, ['read_file']);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].name, 'read_file');
  assert.deepEqual(calls[0].input, { path: 'README.MD' });
});

test('parseToolCalls parses DSML shell as XML-compatible tool call', () => {
  const payload = '<|DSML|tool_calls><|DSML|invoke name="read_file"><|DSML|parameter name="path">README.MD</|DSML|parameter></|DSML|invoke></|DSML|tool_calls>';
  const calls = parseToolCalls(payload, ['read_file']);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].name, 'read_file');
  assert.deepEqual(calls[0].input, { path: 'README.MD' });
});

test('parseToolCalls parses hyphenated DSML shell with here-doc CDATA', () => {
  const payload = `<dsml-tool-calls>
<dsml-invoke name="Bash">
<dsml-parameter name="command"><![CDATA[git commit -m "$(cat <<'EOF'
docs: add missing directory entries and package descriptions to architecture docs
Fill gaps identified in architecture audit: add artifacts/ and static/ to
directory tree, and document 7 auxiliary internal/ packages (textclean,
claudeconv, compat, rawsample, devcapture, util, version) in Section 3.

Co-Authored-By: Claude Opus 4.7 noreply@anthropic.com
EOF
)"]]></dsml-parameter>
<dsml-parameter name="description"><![CDATA[Create commit with architecture doc updates]]></dsml-parameter>
</dsml-invoke>
</dsml-tool-calls>`;
  const calls = parseToolCalls(payload, ['Bash']);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].name, 'Bash');
  assert.equal(calls[0].input.description, 'Create commit with architecture doc updates');
  assert.equal(calls[0].input.command.includes('git commit -m "$(cat <<\'EOF\''), true);
  assert.equal(calls[0].input.command.includes('Co-Authored-By: Claude Opus 4.7'), true);
});

test('parseToolCalls ignores bare hyphenated tool_calls lookalike', () => {
  const payload = '<tool-calls><invoke name="Bash"><parameter name="command">pwd</parameter></invoke></tool-calls>';
  const calls = parseToolCalls(payload, ['Bash']);
  assert.equal(calls.length, 0);
});

test('parseToolCalls tolerates DSML trailing pipe tag terminator', () => {
  const payload = [
    '<|DSML|tool_calls| ',
    '  <|DSML|invoke name="terminal">',
    '    <|DSML|parameter name="command"><![CDATA[find "/home" -type d]]></|DSML|parameter>',
    '    <|DSML|parameter name="timeout"><![CDATA[10]]></|DSML|parameter>',
    '  </|DSML|invoke>',
    '</|DSML|tool_calls>',
  ].join('\n');
  const calls = parseToolCalls(payload, ['terminal']);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].name, 'terminal');
  assert.deepEqual(calls[0].input, { command: 'find "/home" -type d', timeout: 10 });
});

test('parseToolCalls tolerates extra leading less-than before DSML tags', () => {
  const payload = [
    '<<|DSML|tool_calls>',
    '  <<|DSML|invoke name="Bash">',
    '    <<|DSML|parameter name="command"><![CDATA[pwd]]></|DSML|parameter>',
    '  </|DSML|invoke>',
    '</|DSML|tool_calls>',
  ].join('\n');
  const calls = parseToolCalls(payload, ['Bash']);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].name, 'Bash');
  assert.deepEqual(calls[0].input, { command: 'pwd' });
});

test('parseToolCalls tolerates repeated DSML prefix noise', () => {
  const payload = [
    '<<DSML|DSML|tool_calls>',
    '  <<DSML|DSML|invoke name="Bash">',
    '    <<DSML|DSML|parameter name="command"><![CDATA[git status]]></DSML|DSML|parameter>',
    '  </DSML|DSML|invoke>',
    '</DSML|DSML|tool_calls>',
  ].join('\n');
  const calls = parseToolCalls(payload, ['Bash']);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].name, 'Bash');
  assert.deepEqual(calls[0].input, { command: 'git status' });
});

test('parseToolCalls tolerates DSML space-separator typo', () => {
  const payload = '<|DSML tool_calls><|DSML invoke name="Read"><|DSML parameter name="file_path"><![CDATA[/tmp/input.txt]]></|DSML parameter></|DSML invoke></|DSML tool_calls>';
  const calls = parseToolCalls(payload, ['Read']);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].name, 'Read');
  assert.deepEqual(calls[0].input, { file_path: '/tmp/input.txt' });
});

test('parseToolCalls ignores DSML space lookalike tag names', () => {
  const payload = '<|DSML tool_calls_extra><|DSML invoke name="Read"><|DSML parameter name="file_path">/tmp/input.txt</|DSML parameter></|DSML invoke></|DSML tool_calls_extra>';
  const calls = parseToolCalls(payload, ['Read']);
  assert.equal(calls.length, 0);
});

test('parseToolCalls tolerates collapsed DSML tag names', () => {
  const todos = [
    '[x] 检查 toolcalls_format.go 格式化逻辑',
    '[x] 检查 toolcalls_parse.go 解析逻辑',
    '[x] 检查 toolcalls_xml.go 和 toolcalls_dsml.go',
    '[x] 检查 toolcalls_markup.go 和 toolcalls_json_repair.go',
    '[x] 检查 prompt/tool_calls.go 注入逻辑',
    '[x] 检查 toolstream 流式解析',
    '[x] 查看测试文件确认预期行为',
    '[x] 给出调查结论',
  ].join('\n');
  const payload = `<DSMLtool_calls><DSMLinvoke name="update_todo_list"><DSMLparameter name="todos"><![CDATA[${todos}]]></DSMLparameter></DSMLinvoke></DSMLtool_calls>`;
  const calls = parseToolCalls(payload, ['update_todo_list']);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].name, 'update_todo_list');
  assert.equal(calls[0].input.todos, todos);
});

test('parseToolCalls ignores collapsed DSML lookalike tag names', () => {
  const payload = '<DSMLtool_calls_extra><DSMLinvoke name="update_todo_list"><DSMLparameter name="todos">x</DSMLparameter></DSMLinvoke></DSMLtool_calls_extra>';
  const calls = parseToolCalls(payload, ['update_todo_list']);
  assert.equal(calls.length, 0);
});

test('parseToolCalls keeps canonical XML examples inside DSML CDATA', () => {
  const content = '<tool_calls><invoke name="demo"><parameter name="value">x</parameter></invoke></tool_calls>';
  const payload = `<|DSML|tool_calls><|DSML|invoke name="write_file"><|DSML|parameter name="path">notes.md</|DSML|parameter><|DSML|parameter name="content"><![CDATA[${content}]]></|DSML|parameter></|DSML|invoke></|DSML|tool_calls>`;
  const calls = parseToolCalls(payload, ['write_file']);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].name, 'write_file');
  assert.deepEqual(calls[0].input, { path: 'notes.md', content });
});

test('parseToolCalls preserves simple inline markup inside CDATA as text', () => {
  const payload = '<tool_calls><invoke name="Write"><parameter name="description"><![CDATA[<b>urgent</b>]]></parameter></invoke></tool_calls>';
  const calls = parseToolCalls(payload, ['Write']);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].input.description, '<b>urgent</b>');
});

test('parseToolCalls recovers when CDATA never closes inside a valid wrapper', () => {
  const payload = '<tool_calls><invoke name="Write"><parameter name="content"><![CDATA[hello world</parameter></invoke></tool_calls>';
  const calls = parseToolCalls(payload, ['Write']);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].name, 'Write');
  assert.equal(calls[0].input.content, 'hello world');
});

test('parseToolCalls supports JSON scalar parameters', () => {
  const payload = '<tool_calls><invoke name="configure"><parameter name="count">123</parameter><parameter name="max_tokens"><![CDATA[256]]></parameter><parameter name="enabled">true</parameter></invoke></tool_calls>';
  const calls = parseToolCalls(payload, ['configure']);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].name, 'configure');
  assert.equal(calls[0].input.count, 123);
  assert.equal(calls[0].input.max_tokens, 256);
  assert.equal(calls[0].input.enabled, true);
});

test('parseToolCalls treats item-only parameter body as array', () => {
  const payload = [
    '<|DSML|tool_calls>',
    '<|DSML|invoke name="AskUserQuestion">',
    '<|DSML|parameter name="questions">',
    '<item>',
    '<question><![CDATA[What would you like to do next?]]></question>',
    '<header><![CDATA[Next step]]></header>',
    '<options>',
    '<item><label><![CDATA[Run tests]]></label><description><![CDATA[Run the test suite]]></description></item>',
    '<item><label><![CDATA[Other task]]></label><description><![CDATA[Something else entirely]]></description></item>',
    '</options>',
    '<multiSelect>false</multiSelect>',
    '</item>',
    '</|DSML|parameter>',
    '</|DSML|invoke>',
    '</|DSML|tool_calls>',
  ].join('\n');
  const calls = parseToolCalls(payload, ['AskUserQuestion']);
  assert.equal(calls.length, 1);
  assert.deepEqual(calls[0].input.questions, [
    {
      question: 'What would you like to do next?',
      header: 'Next step',
      options: [
        { label: 'Run tests', description: 'Run the test suite' },
        { label: 'Other task', description: 'Something else entirely' },
      ],
      multiSelect: false,
    },
  ]);
});

test('parseToolCalls treats CDATA item-only body as array', () => {
  const todos = '<br>  <item><br>    <activeForm>Testing EnterWorktree tool</activeForm><br>    <content>Test EnterWorktree tool</content><br>    <status>in_progress</status><br>  </item><br>  <item><br>    <activeForm>Testing TodoWrite tool</activeForm><br>    <content>Test TodoWrite tool</content><br>    <status>completed</status><br>  </item><br>';
  const payload = `<|DSML|tool_calls><|DSML|invoke name="TodoWrite"><|DSML|parameter name="todos"><![CDATA[${todos}]]></|DSML|parameter></|DSML|invoke></|DSML|tool_calls>`;
  const calls = parseToolCalls(payload, ['TodoWrite']);
  assert.equal(calls.length, 1);
  assert.deepEqual(calls[0].input.todos, [
    {
      activeForm: 'Testing EnterWorktree tool',
      content: 'Test EnterWorktree tool',
      status: 'in_progress',
    },
    {
      activeForm: 'Testing TodoWrite tool',
      content: 'Test TodoWrite tool',
      status: 'completed',
    },
  ]);
});

test('parseToolCalls treats single-item CDATA body as array', () => {
  const payload = '<tool_calls><invoke name="TodoWrite"><parameter name="todos"><![CDATA[<item>one</item>]]></parameter></invoke></tool_calls>';
  const calls = parseToolCalls(payload, ['TodoWrite']);
  assert.equal(calls.length, 1);
  assert.deepEqual(calls[0].input.todos, ['one']);
});

test('parseToolCalls treats loose JSON list as array', () => {
  for (const [label, body] of [
    ['plain text', '{"content":"Test TodoWrite tool","status":"completed"}, {"content":"Another task","status":"pending"}'],
    ['cdata', '<![CDATA[{"content":"Test TodoWrite tool","status":"completed"}, {"content":"Another task","status":"pending"}]]>'],
  ]) {
    const payload = `<tool_calls><invoke name="TodoWrite"><parameter name="todos">${body}</parameter></invoke></tool_calls>`;
    const calls = parseToolCalls(payload, ['TodoWrite']);
    assert.equal(calls.length, 1, label);
    assert.deepEqual(calls[0].input.todos, [
      { content: 'Test TodoWrite tool', status: 'completed' },
      { content: 'Another task', status: 'pending' },
    ]);
  }
});

test('parseToolCalls keeps preserved text parameters as text', () => {
  const payload = '<tool_calls><invoke name="Write"><parameter name="content"><![CDATA[{"content":"Test TodoWrite tool","status":"completed"}, {"content":"Another task","status":"pending"}]]></parameter></invoke></tool_calls>';
  const calls = parseToolCalls(payload, ['Write']);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].input.content, '{"content":"Test TodoWrite tool","status":"completed"}, {"content":"Another task","status":"pending"}');
});

test('formatOpenAIStreamToolCalls normalizes camelCase inputSchema string fields', () => {
  const formatted = formatOpenAIStreamToolCalls([
    { name: 'Write', input: { content: { message: 'hi' }, taskId: 1 } },
  ], new Map(), [
    { name: 'Write', inputSchema: { type: 'object', properties: { content: { type: 'string' }, taskId: { type: 'string' } } } },
  ]);
  assert.equal(formatted.length, 1);
  const args = JSON.parse(formatted[0].function.arguments);
  assert.equal(args.content, '{"message":"hi"}');
  assert.equal(args.taskId, '1');
});

test('formatOpenAIStreamToolCalls preserves arrays when schema says array', () => {
  const todos = [{ content: 'x', status: 'pending', priority: 'high' }];
  const formatted = formatOpenAIStreamToolCalls([
    { name: 'todowrite', input: { todos } },
  ], new Map(), [
    { name: 'todowrite', inputSchema: { type: 'object', properties: { todos: { type: 'array', items: { type: 'object' } } } } },
  ]);
  assert.equal(formatted.length, 1);
  const args = JSON.parse(formatted[0].function.arguments);
  assert.deepEqual(args.todos, todos);
});

test('parseToolCalls treats CDATA object fragment as object', () => {
  const fragment = '<question><![CDATA[Pick one]]></question><options><item><label><![CDATA[A]]></label></item><item><label><![CDATA[B]]></label></item></options>';
  const payload = `<tool_calls><invoke name="AskUserQuestion"><parameter name="questions"><![CDATA[${fragment}]]></parameter></invoke></tool_calls>`;
  const calls = parseToolCalls(payload, ['AskUserQuestion']);
  assert.equal(calls.length, 1);
  assert.deepEqual(calls[0].input.questions, {
    question: 'Pick one',
    options: [
      { label: 'A' },
      { label: 'B' },
    ],
  });
});

test('parseToolCalls normalizes mixed DSML and XML tool tags', () => {
  // Models commonly mix DSML wrapper tags with canonical inner tags.
  const payload = '<|DSML|tool_calls><invoke name="read_file"><|DSML|parameter name="path">README.MD</|DSML|parameter></invoke></|DSML|tool_calls>';
  const calls = parseToolCalls(payload, ['read_file']);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].name, 'read_file');
  assert.deepEqual(calls[0].input, { path: 'README.MD' });
});

test('parseToolCalls skips prose mention of same wrapper variant', () => {
  const payload = [
    'Summary: support canonical <tool_calls> and DSML <|DSML|tool_calls> wrappers.',
    '',
    '<|DSML|tool_calls>',
    '<|DSML|invoke name="Bash">',
    '<|DSML|parameter name="command"><![CDATA[git status]]></|DSML|parameter>',
    '</|DSML|invoke>',
    '</|DSML|tool_calls>',
  ].join('\n');
  const calls = parseToolCalls(payload, ['Bash']);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].name, 'Bash');
  assert.equal(calls[0].input.command, 'git status');
});

test('sieve emits tool_calls after prose mentions same wrapper variant', () => {
  const events = runSieve([
    'Summary: support canonical <tool_calls> and DSML <|DSML|tool_calls> wrappers.\n\n',
    '<|DSML|tool_calls>\n',
    '<|DSML|invoke name="Bash">\n',
    '<|DSML|parameter name="command"><![CDATA[git status]]></|DSML|parameter>\n',
    '</|DSML|invoke>\n',
    '</|DSML|tool_calls>',
  ], ['Bash']);
  const finalCalls = events.filter((evt) => evt.type === 'tool_calls').flatMap((evt) => evt.calls || []);
  assert.equal(finalCalls.length, 1);
  assert.equal(finalCalls[0].name, 'Bash');
  assert.equal(finalCalls[0].input.command, 'git status');
  assert.equal(collectText(events).includes('Summary:'), true);
});

test('sieve emits tool_calls for DSML space-separator typo', () => {
  const events = runSieve([
    '准备读取文件。\n',
    '<|DSML tool_calls>\n',
    '<|DSML invoke name="Read">\n',
    '<|DSML parameter name="file_path"><![CDATA[/tmp/input.txt]]></|DSML parameter>\n',
    '</|DSML invoke>\n',
    '</|DSML tool_calls>',
  ], ['Read']);
  const text = collectText(events);
  const finalCalls = events.filter((evt) => evt.type === 'tool_calls').flatMap((evt) => evt.calls || []);
  assert.equal(finalCalls.length, 1);
  assert.equal(finalCalls[0].name, 'Read');
  assert.equal(finalCalls[0].input.file_path, '/tmp/input.txt');
  assert.equal(text.includes('准备读取文件'), true);
  assert.equal(text.includes('<|DSML invoke'), false);
});

test('sieve emits tool_calls for DSML trailing pipe tag terminator', () => {
  const events = runSieve([
    '<|DSML|tool_calls| \n',
    '<|DSML|invoke name="terminal">\n',
    '<|DSML|parameter name="command"><![CDATA[find "/home" -type d]]></|DSML|parameter>\n',
    '<|DSML|parameter name="timeout"><![CDATA[10]]></|DSML|parameter>\n',
    '</|DSML|invoke>\n',
    '</|DSML|tool_calls>',
  ], ['terminal']);
  const finalCalls = events.filter((evt) => evt.type === 'tool_calls').flatMap((evt) => evt.calls || []);
  const text = collectText(events);
  assert.equal(finalCalls.length, 1);
  assert.equal(finalCalls[0].name, 'terminal');
  assert.deepEqual(finalCalls[0].input, { command: 'find "/home" -type d', timeout: 10 });
  assert.equal(text.toLowerCase().includes('dsml'), false);
});

test('sieve emits tool_calls for extra leading less-than DSML tags without leaking prefix', () => {
  const events = runSieve([
    '<<|DSML|tool_calls>\n',
    '<<|DSML|invoke name="Bash">\n',
    '<<|DSML|parameter name="command"><![CDATA[pwd]]></|DSML|parameter>\n',
    '</|DSML|invoke>\n',
    '</|DSML|tool_calls>',
  ], ['Bash']);
  const finalCalls = events.filter((evt) => evt.type === 'tool_calls').flatMap((evt) => evt.calls || []);
  const text = collectText(events);
  assert.equal(finalCalls.length, 1);
  assert.equal(finalCalls[0].name, 'Bash');
  assert.deepEqual(finalCalls[0].input, { command: 'pwd' });
  assert.equal(text.includes('<'), false);
});

test('sieve keeps DSML space lookalike tag names as text', () => {
  const input = '<|DSML tool_calls_extra><|DSML invoke name="Read"><|DSML parameter name="file_path">/tmp/input.txt</|DSML parameter></|DSML invoke></|DSML tool_calls_extra>';
  const events = runSieve([input], ['Read']);
  const finalCalls = events.filter((evt) => evt.type === 'tool_calls').flatMap((evt) => evt.calls || []);
  assert.equal(finalCalls.length, 0);
  assert.equal(collectText(events), input);
});

test('sieve emits tool_calls for collapsed DSML tag names and preserves prefix text', () => {
  const todos = [
    '[x] 检查 toolcalls_format.go 格式化逻辑',
    '[x] 检查 toolcalls_parse.go 解析逻辑',
    '[x] 检查 toolcalls_xml.go 和 toolcalls_dsml.go',
    '[x] 检查 toolcalls_markup.go 和 toolcalls_json_repair.go',
    '[x] 检查 prompt/tool_calls.go 注入逻辑',
    '[x] 检查 toolstream 流式解析',
    '[x] 查看测试文件确认预期行为',
    '[x] 给出调查结论',
  ].join('\n');
  const events = runSieve([
    '[]\n',
    '<DSMLtool_calls>\n',
    '<DSMLinvoke name="update_todo_list">\n',
    `<DSMLparameter name="todos"><![CDATA[${todos}]]></DSMLparameter>\n`,
    '</DSMLinvoke>\n',
    '</DSMLtool_calls>',
  ], ['update_todo_list']);
  const text = collectText(events);
  const finalCalls = events.filter((evt) => evt.type === 'tool_calls').flatMap((evt) => evt.calls || []);
  assert.equal(finalCalls.length, 1);
  assert.equal(finalCalls[0].name, 'update_todo_list');
  assert.equal(finalCalls[0].input.todos, todos);
  assert.equal(text, '[]\n');
});

test('sieve keeps collapsed DSML lookalike tag names as text', () => {
  const input = '<DSMLtool_calls_extra><DSMLinvoke name="update_todo_list"><DSMLparameter name="todos">x</DSMLparameter></DSMLinvoke></DSMLtool_calls_extra>';
  const events = runSieve([input], ['update_todo_list']);
  const finalCalls = events.filter((evt) => evt.type === 'tool_calls').flatMap((evt) => evt.calls || []);
  assert.equal(finalCalls.length, 0);
  assert.equal(collectText(events), input);
});

test('sieve preserves review body with alias mentions before real DSML tool calls', () => {
  const events = runSieve([
    "Done reviewing the diff. Here's my analysis before we commit:\n\n",
    'Summary of Changes\n',
    'DSML wrapper variant support — recognize aliases (<dsml|tool_calls>, <|tool_calls>, <｜tool_calls>) alongside canonical <tool_calls> and <|DSML|tool_calls> wrappers.\n\n',
    '<|DSML|tool_calls>\n',
    '<|DSML|invoke name="Bash">\n',
    '<|DSML|parameter name="command"><![CDATA[git add docs/toolcall-semantics.md internal/toolstream/tool_sieve_xml.go]]></|DSML|parameter>\n',
    '<|DSML|parameter name="description"><![CDATA[Stage all relevant changed files]]></|DSML|parameter>\n',
    '</|DSML|invoke>\n',
    '<|DSML|invoke name="Bash">\n',
    '<|DSML|parameter name="command"><![CDATA[git commit -m "$(cat <<\'EOF\'\nfeat(toolstream): expand DSML wrapper detection\n\nSupport DSML wrapper aliases: <dsml|tool_calls>, <|tool_calls>, <｜tool_calls> alongside existing canonical wrappers.\nEOF\n)"]]></|DSML|parameter>\n',
    '<|DSML|parameter name="description"><![CDATA[Create commit with all staged changes]]></|DSML|parameter>\n',
    '</|DSML|invoke>\n',
    '</|DSML|tool_calls>',
  ], ['Bash']);
  const text = collectText(events);
  const finalCalls = events.filter((evt) => evt.type === 'tool_calls').flatMap((evt) => evt.calls || []);
  assert.equal(finalCalls.length, 2);
  assert.equal(text.includes('<|DSML|tool_calls> wrappers'), true);
  assert.equal(text.includes('Summary of Changes'), true);
  assert.equal(text.includes('git add docs/toolcall-semantics.md'), false);
});

test('sieve preserves Chinese review body with inline DSML mention before real tool call', () => {
  const events = runSieve([
    '# Context from my IDE setup:\n\n## My request for Codex:\n',
    '基于我的审查，这是工作区更改的总结和提交。\n\n## 审查报告\n\n### 文档\n\nAPI.md 中的工具调用部分缺少针对新 DSML 别名的更新——它只提到了 `',
    '<|DSML|tool_calls>` 和 canonical `<tool_calls>`。由于这涉及 API 兼容性和文档准确性，需要在下游进行记录。\n\n',
    '### 代码\n\n所有更改现在一致地处理四个 DSML wrapper 变体。\n\n现在提交已暂存的更改。\n\n',
    '<|DSML|tool_calls>\n',
    '  <|DSML|invoke name="Bash">\n',
    '    <|DSML|parameter name="command"><![CDATA[git commit -m "$(cat <<\'EOF\'\nfeat: expand DSML tool-call alias and fence handling\nEOF\n)"]]></|DSML|parameter>\n',
    '    <|DSML|parameter name="description"><![CDATA[Commit staged changes]]></|DSML|parameter>\n',
    '  </|DSML|invoke>\n',
    '</|DSML|tool_calls>\n\n补充',
  ], ['Bash']);
  const text = collectText(events);
  const finalCalls = events.filter((evt) => evt.type === 'tool_calls').flatMap((evt) => evt.calls || []);
  assert.equal(finalCalls.length, 1);
  assert.equal(text.includes('它只提到了 `<|DSML|tool_calls>` 和 canonical `<tool_calls>`。由于这涉及 API 兼容性'), true);
  assert.equal(text.includes('补充'), true);
  assert.equal(text.includes('<|DSML|invoke'), false);
});

test('sieve captures hyphenated DSML shell with here-doc CDATA', () => {
  const events = runSieve([
    '<dsml-tool-calls>\n',
    '<dsml-invoke name="Bash">\n',
    '<dsml-parameter name="command"><![CDATA[git commit -m "$(cat <<\'EOF\'\n',
    'docs: add missing directory entries and package descriptions to architecture docs\n',
    'Fill gaps identified in architecture audit: add artifacts/ and static/ to\n',
    'directory tree, and document 7 auxiliary internal/ packages (textclean,\n',
    'claudeconv, compat, rawsample, devcapture, util, version) in Section 3.\n\n',
    'Co-Authored-By: Claude Opus 4.7 noreply@anthropic.com\n',
    'EOF\n',
    ')"]]></dsml-parameter>\n',
    '<dsml-parameter name="description"><![CDATA[Create commit with architecture doc updates]]></dsml-parameter>\n',
    '</dsml-invoke>\n',
    '</dsml-tool-calls>',
  ], ['Bash']);
  const text = collectText(events);
  const finalCalls = events.filter((evt) => evt.type === 'tool_calls').flatMap((evt) => evt.calls || []);
  assert.equal(finalCalls.length, 1);
  assert.equal(finalCalls[0].input.command.includes('git commit -m "$(cat <<\'EOF\''), true);
  assert.equal(finalCalls[0].input.command.includes('Co-Authored-By: Claude Opus 4.7'), true);
  assert.equal(text.includes('dsml-tool-calls'), false);
  assert.equal(text.includes('git commit -m'), false);
});

test('parseToolCalls ignores JSON tool_calls payload (XML-only)', () => {
  const payload = JSON.stringify({
    tool_calls: [{ name: 'read_file', input: { path: 'README.MD' } }],
  });
  const calls = parseToolCalls(payload, ['read_file']);
  assert.equal(calls.length, 0);
});

test('parseToolCalls ignores tool_call payloads that exist only inside fenced code blocks', () => {
  const text = [
    'I will call a tool now.',
    '```xml',
    '<tool_calls><invoke name="read_file"><parameter name="path">README.md</parameter></invoke></tool_calls>',
    '```',
  ].join('\n');
  const calls = parseToolCalls(text, ['read_file']);
  assert.equal(calls.length, 0);
});

test('parseToolCalls keeps unknown schema names when toolNames is provided', () => {
  const payload = '<tool_calls><invoke name="not_in_schema"><parameter name="q">go</parameter></invoke></tool_calls>';
  const calls = parseToolCalls(payload, ['search']);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].name, 'not_in_schema');
});

test('sieve emits tool_calls for XML tool call payload', () => {
  const events = runSieve(
    ['<tool_calls><invoke name="read_file"><parameter name="path">README.MD</parameter></invoke></tool_calls>'],
    ['read_file'],
  );
  const finalCalls = events.filter((evt) => evt.type === 'tool_calls').flatMap((evt) => evt.calls || []);
  assert.equal(finalCalls.length, 1);
  assert.equal(finalCalls[0].name, 'read_file');
});

test('sieve emits tool_calls when XML tag spans multiple chunks', () => {
  const events = runSieve(
    [
      '<tool_calls><invoke name="read_file">',
      '<parameter name="path">README.MD</parameter></invoke></tool_calls>',
    ],
    ['read_file'],
  );
  const finalCalls = events.filter((evt) => evt.type === 'tool_calls').flatMap((evt) => evt.calls || []);
  assert.equal(finalCalls.length, 1);
  assert.equal(finalCalls[0].name, 'read_file');
});

test('sieve emits tool_calls when DSML tag spans multiple chunks', () => {
  const events = runSieve(
    [
      '<|DSML|tool',
      '_calls><|DSML|invoke name="read_file">',
      '<|DSML|parameter name="path">README.MD</|DSML|parameter></|DSML|invoke></|DSML|tool_calls>',
    ],
    ['read_file'],
  );
  const leakedText = collectText(events);
  const finalCalls = events.filter((evt) => evt.type === 'tool_calls').flatMap((evt) => evt.calls || []);
  assert.equal(leakedText, '');
  assert.equal(finalCalls.length, 1);
  assert.equal(finalCalls[0].name, 'read_file');
});

test('sieve emits tool_calls when fullwidth DSML prefix variant spans multiple chunks', () => {
  const events = runSieve(
    [
      '<｜DSML|tool',
      '_calls>\n',
      '<|DSML|invoke name="Bash">\n',
      '<|DSML|parameter name="command"><![CDATA[ls -la /Users/aq/Desktop/myproject/ds2api/]]></|DSML|parameter>\n',
      '<|DSML|parameter name="description"><![CDATA[List project root contents]]></|DSML|parameter>\n',
      '</|DSML|invoke>\n',
      '<|DSML|invoke name="Bash">\n',
      '<|DSML|parameter name="command"><![CDATA[cat /Users/aq/Desktop/myproject/ds2api/package.json 2>/dev/null || echo "No package.json found"]]></|DSML|parameter>\n',
      '<|DSML|parameter name="description"><![CDATA[Check for existing package.json]]></|DSML|parameter>\n',
      '</|DSML|invoke>\n',
      '</|DSML|tool_calls>',
    ],
    ['Bash'],
  );
  const leakedText = collectText(events);
  const finalCalls = events.filter((evt) => evt.type === 'tool_calls').flatMap((evt) => evt.calls || []);
  assert.equal(leakedText, '');
  assert.equal(finalCalls.length, 2);
  assert.equal(finalCalls[0].name, 'Bash');
  assert.equal(finalCalls[1].name, 'Bash');
});

test('sieve keeps long XML tool calls buffered until the closing tag arrives', () => {
  const longContent = 'x'.repeat(4096);
  const splitAt = longContent.length / 2;
  const events = runSieve(
    [
      '<tool_calls>\n  <invoke name="write_to_file">\n    <parameter name="content"><![CDATA[',
      longContent.slice(0, splitAt),
      longContent.slice(splitAt),
      ']]></parameter>\n  </invoke>\n</tool_calls>',
    ],
    ['write_to_file'],
  );
  const leakedText = collectText(events);
  const finalCalls = events.filter((evt) => evt.type === 'tool_calls').flatMap((evt) => evt.calls || []);
  assert.equal(leakedText, '');
  assert.equal(finalCalls.length, 1);
  assert.equal(finalCalls[0].name, 'write_to_file');
  assert.equal(finalCalls[0].input.content, longContent);
});

test('sieve recovers when CDATA never closes inside a valid wrapper', () => {
  const events = runSieve(
    [
      '<tool_calls>\n  <invoke name="Write">\n    <parameter name="content"><![CDATA[',
      'hello world',
      '</parameter>\n  </invoke>\n</tool_calls>',
    ],
    ['Write'],
  );
  const leakedText = collectText(events);
  const finalCalls = events.filter((evt) => evt.type === 'tool_calls').flatMap((evt) => evt.calls || []);
  assert.equal(finalCalls.length, 1);
  assert.equal(finalCalls[0].name, 'Write');
  assert.equal(finalCalls[0].input.content, 'hello world');
  assert.equal(leakedText, '');
});

test('sieve keeps CDATA tool examples buffered until the outer closing tag arrives', () => {
  const content = [
    '# Whale2API 4.0 更新内容',
    '',
    'x'.repeat(4096),
    '```xml',
    '<tool_calls>',
    '  <invoke name="demo">',
    '    <parameter name="value">x</parameter>',
    '  </invoke>',
    '</tool_calls>',
    '```',
    'tail',
  ].join('\n');
  const innerClose = content.indexOf('</tool_calls>') + '</tool_calls>'.length;
  const state = createToolSieveState();
  const chunks = [
    '<tool_calls>\n  <invoke name="Write">\n    <parameter name="content"><![CDATA[',
    content.slice(0, innerClose),
    content.slice(innerClose),
    ']]></parameter>\n    <parameter name="file_path">DS2API-4.0-Release-Notes.md</parameter>\n  </invoke>\n</tool_calls>',
  ];
  const events = [];
  chunks.forEach((chunk, idx) => {
    const next = processToolSieveChunk(state, chunk, ['Write']);
    if (idx <= 1) {
      assert.deepEqual(next, []);
    }
    events.push(...next);
  });
  events.push(...flushToolSieve(state, ['Write']));

  const leakedText = collectText(events);
  const finalCalls = events.filter((evt) => evt.type === 'tool_calls').flatMap((evt) => evt.calls || []);
  assert.equal(leakedText, '');
  assert.equal(finalCalls.length, 1);
  assert.equal(finalCalls[0].name, 'Write');
  assert.equal(finalCalls[0].input.content, content);
});

test('parseToolCalls keeps XML-looking CDATA content intact', () => {
  const content = [
    '# Release notes',
    '```xml',
    '<tool_calls><invoke name="demo"><parameter name="value">x</parameter></invoke></tool_calls>',
    '```',
  ].join('\n');
  const payload = `<tool_calls><invoke name="Write"><parameter name="content"><![CDATA[${content}]]></parameter><parameter name="file_path">DS2API-4.0-Release-Notes.md</parameter></invoke></tool_calls>`;
  const calls = parseToolCalls(payload, ['Write']);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].input.content, content);
  assert.equal(calls[0].input.file_path, 'DS2API-4.0-Release-Notes.md');
});

test('sieve passes JSON tool_calls payload through as text (XML-only)', () => {
  const events = runSieve(
    ['{"tool_calls":[{"name":"read_file","input":{"path":"README.MD"}}]}'],
    ['read_file'],
  );
  const leakedText = collectText(events);
  const hasToolCall = events.some((evt) => evt.type === 'tool_calls' && evt.calls?.length > 0);
  assert.equal(hasToolCall, false);
  assert.equal(leakedText.includes('tool_calls'), true);
});

test('sieve keeps embedded invalid tool-like json as normal text to avoid stream stalls', () => {
  const events = runSieve(
    [
      '前置正文D。',
      "{'tool_calls':[{'name':'read_file','input':{'path':'README.MD'}}]}",
      '后置正文E。',
    ],
    ['read_file'],
  );
  const leakedText = collectText(events);
  const hasToolCall = events.some((evt) => evt.type === 'tool_calls');
  assert.equal(hasToolCall, false);
  assert.equal(leakedText.includes('前置正文D。'), true);
  assert.equal(leakedText.includes('后置正文E。'), true);
  assert.equal(leakedText.toLowerCase().includes('tool_calls'), true);
});

test('sieve passes malformed executable-looking XML through as text', () => {
  const chunk = '<tool_calls><invoke name="read_file"><param>{"path":"README.MD"}</param></invoke></tool_calls>';
  const events = runSieve([chunk], ['read_file']);
  const leakedText = collectText(events);
  const hasToolCalls = events.some((evt) => evt.type === 'tool_calls' && evt.calls?.length > 0);
  assert.equal(hasToolCalls, false);
  assert.equal(leakedText, chunk);
});

test('sieve keeps bare tool_call XML as plain text without wrapper', () => {
  const chunk = '<invoke name="read_file"><parameter name="path">README.MD</parameter></invoke>';
  const events = runSieve([chunk], ['read_file']);
  const leakedText = collectText(events);
  const hasToolCalls = events.some((evt) => evt.type === 'tool_calls' && evt.calls?.length > 0);
  assert.equal(hasToolCalls, false);
  assert.equal(leakedText, chunk);
});

test('sieve flushes incomplete captured XML tool blocks by falling back to raw text', () => {
  const events = runSieve(
    [
      '前置正文G。',
      '<tool_calls>\n',
      '  <invoke name="read_file">\n',
    ],
    ['read_file'],
  );
  const leakedText = collectText(events);
  const expected = ['前置正文G。', '<tool_calls>\n', '  <invoke name="read_file">\n'].join('');
  const hasToolCalls = events.some((evt) => evt.type === 'tool_calls' && evt.calls?.length > 0);
  assert.equal(hasToolCalls, false);
  assert.equal(leakedText, expected);
});

test('sieve captures XML wrapper tags with attributes without leaking wrapper text', () => {
  const events = runSieve(
    [
      '前置正文H。',
      '<tool_calls id="x"><invoke name="read_file"><parameter name="path">README.MD</parameter></invoke></tool_calls>',
      '后置正文I。',
    ],
    ['read_file'],
  );
  const leakedText = collectText(events);
  const hasToolCall = events.some((evt) => evt.type === 'tool_calls' && evt.calls?.length > 0);
  assert.equal(hasToolCall, true);
  assert.equal(leakedText.includes('前置正文H。'), true);
  assert.equal(leakedText.includes('后置正文I。'), true);
  assert.equal(leakedText.includes('<tool_calls id=\"x\">'), false);
  assert.equal(leakedText.includes('</tool_calls>'), false);
});

test('sieve keeps plain text intact in tool mode when no tool call appears', () => {
  const events = runSieve(
    ['你好，', '这是普通文本回复。', '请继续。'],
    ['read_file'],
  );
  const leakedText = collectText(events);
  const hasToolCall = events.some((evt) => evt.type === 'tool_calls');
  assert.equal(hasToolCall, false);
  assert.equal(leakedText, '你好，这是普通文本回复。请继续。');
});

test('sieve keeps plain "tool_calls" prose as text when no valid payload follows', () => {
  const events = runSieve(
    ['前置。', '这里提到 tool_calls 只是解释，不是调用。', '后置。'],
    ['read_file'],
  );
  const leakedText = collectText(events);
  const hasToolCall = events.some((evt) => evt.type === 'tool_calls' && evt.calls?.length > 0);
  assert.equal(hasToolCall, false);
  assert.equal(leakedText.includes('tool_calls'), true);
  assert.equal(leakedText, '前置。这里提到 tool_calls 只是解释，不是调用。后置。');
});

test('sieve keeps numbered planning prose when no tool payload follows', () => {
  const events = runSieve(
    ['好的，我会依次测试每个工具。\n\n1. 获取当前时间'],
    ['get_current_time'],
  );
  const leakedText = collectText(events);
  const hasToolCall = events.some((evt) => evt.type === 'tool_calls' && evt.calls?.length > 0);
  assert.equal(hasToolCall, false);
  assert.equal(leakedText, '好的，我会依次测试每个工具。\n\n1. 获取当前时间');
});

test('sieve does not trigger tool calls for long fenced examples beyond legacy tail window', () => {
  const longPadding = 'x'.repeat(700);
  const events = runSieve(
    [
      `前置说明\n\`\`\`json\n${longPadding}\n`,
      '{"tool_calls":[{"name":"read_file","input":{"path":"README.MD"}}]}\n',
      '```',
      '\n后置说明',
    ],
    ['read_file'],
  );
  const hasTool = events.some((evt) => evt.type === 'tool_calls' && evt.calls?.length > 0);
  const leakedText = collectText(events);
  assert.equal(hasTool, false);
  assert.equal(leakedText.includes('后置说明'), true);
  assert.equal(leakedText.toLowerCase().includes('tool_calls'), true);
});

test('sieve keeps fence state when triple-backticks are split across chunks', () => {
  const events = runSieve(
    [
      '示例开始\n``',
      '`json\n{"tool_calls":[{"name":"read_file","input":{"path":"README.MD"}}]}\n',
      '```',
      '\n示例结束',
    ],
    ['read_file'],
  );
  const hasTool = events.some((evt) => evt.type === 'tool_calls' && evt.calls?.length > 0);
  const leakedText = collectText(events);
  assert.equal(hasTool, false);
  assert.equal(leakedText.includes('示例结束'), true);
  assert.equal(leakedText.toLowerCase().includes('tool_calls'), true);
});

test('formatOpenAIStreamToolCalls reuses ids with the same idStore', () => {
  const idStore = new Map();
  const calls = [{ name: 'read_file', input: { path: 'README.MD' } }];
  const first = formatOpenAIStreamToolCalls(calls, idStore);
  const second = formatOpenAIStreamToolCalls(calls, idStore);
  assert.equal(first.length, 1);
  assert.equal(second.length, 1);
  assert.equal(first[0].id, second[0].id);
});

test('parseToolCalls rejects mismatched markup tags', () => {
  const payload = '<tool_calls><invoke name="read_file"><parameter name="path">README.md</function></invoke></tool_calls>';
  const calls = parseToolCalls(payload, ['read_file']);
  assert.equal(calls.length, 0);
});
