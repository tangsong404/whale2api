#!/usr/bin/env node
import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const chatStream = require('../node/chat-stream.js');
const { parseChunkForContent } = chatStream.__test;
const { trimContinuationOverlap } = chatStream.__test;

function parseArgs(argv) {
  const out = {
    samplesRoot: 'tests/raw_stream_samples',
    reportPath: '',
    outputRoot: '',
    baselineRoot: '',
    sampleId: '',
    failOnLeak: true,
    failOnReferenceLeak: true,
    failOnMissingFinish: true,
    failOnBaselineMismatch: true,
    failOnTokenMismatch: false,
    showOutput: false,
    writeReplayText: false,
  };
  for (let i = 2; i < argv.length; i += 1) {
    const a = argv[i];
    if (a === '--samples-root' && argv[i + 1]) {
      out.samplesRoot = argv[++i];
    } else if (a === '--report' && argv[i + 1]) {
      out.reportPath = argv[++i];
    } else if (a === '--output-root' && argv[i + 1]) {
      out.outputRoot = argv[++i];
    } else if (a === '--baseline-root' && argv[i + 1]) {
      out.baselineRoot = argv[++i];
    } else if (a === '--sample-id' && argv[i + 1]) {
      out.sampleId = argv[++i];
    } else if (a === '--no-fail-on-leak') {
      out.failOnLeak = false;
    } else if (a === '--no-fail-on-reference-leak') {
      out.failOnReferenceLeak = false;
    } else if (a === '--no-fail-on-missing-finish') {
      out.failOnMissingFinish = false;
    } else if (a === '--no-fail-on-baseline-mismatch' || a === '--no-fail-on-processed-mismatch') {
      out.failOnBaselineMismatch = false;
    } else if (a === '--fail-on-token-mismatch') {
      out.failOnTokenMismatch = true;
    } else if (a === '--no-fail-on-token-mismatch') {
      out.failOnTokenMismatch = false;
    } else if (a === '--show-output') {
      out.showOutput = true;
    } else if (a === '--write-replay-text' || a === '--write-processed-text') {
      out.writeReplayText = true;
    }
  }
  return out;
}

function loadManifest(root) {
  const manifestPath = path.join(root, 'manifest.json');
  if (!fs.existsSync(manifestPath)) {
    return null;
  }
  try {
    const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'));
    const defaultSamples = Array.isArray(manifest.default_samples)
      ? manifest.default_samples.map((v) => String(v).trim()).filter(Boolean)
      : [];
    if (defaultSamples.length === 0) {
      return null;
    }
    return { manifestPath, defaultSamples };
  } catch (err) {
    throw new Error(`[sim] failed to parse ${manifestPath}: ${err.message}`);
  }
}

function resolveSampleDirs(root, sampleID) {
  if (!fs.existsSync(root)) {
    return { dirs: [], manifestPath: '' };
  }

  if (sampleID) {
    const dir = path.join(root, sampleID);
    const ssePath = path.join(dir, 'upstream.stream.sse');
    if (!fs.existsSync(dir) || !fs.statSync(dir).isDirectory() || !fs.existsSync(ssePath)) {
      throw new Error(`[sim] sample missing: ${sampleID}`);
    }
    return { dirs: [dir], manifestPath: '' };
  }

  const manifest = loadManifest(root);
  if (manifest) {
    const dirs = [];
    const missing = [];
    for (const sampleID of manifest.defaultSamples) {
      const dir = path.join(root, sampleID);
      const ssePath = path.join(dir, 'upstream.stream.sse');
      if (!fs.existsSync(dir) || !fs.statSync(dir).isDirectory() || !fs.existsSync(ssePath)) {
        missing.push(sampleID);
        continue;
      }
      dirs.push(dir);
    }
    if (missing.length > 0) {
      throw new Error(`[sim] manifest sample(s) missing: ${missing.join(', ')}`);
    }
    return { dirs, manifestPath: manifest.manifestPath };
  }

  const dirs = fs.readdirSync(root)
    .map((name) => path.join(root, name))
    .filter((p) => fs.statSync(p).isDirectory())
    .filter((p) => fs.existsSync(path.join(p, 'upstream.stream.sse')))
    .sort();
  return { dirs, manifestPath: '' };
}

function parseSSE(raw) {
  const events = [];
  for (const block of raw.split(/\r?\n\r?\n/)) {
    if (!block.trim()) {
      continue;
    }
    let eventType = 'message';
    const dataLines = [];
    for (const line of block.split(/\r?\n/)) {
      if (line.startsWith('event:')) {
        eventType = line.slice(6).trim() || 'message';
      } else if (line.startsWith('data:')) {
        dataLines.push(line.slice(5).trimStart());
      }
    }
    if (dataLines.length === 0) {
      continue;
    }
    const payload = dataLines.join('\n').trim();
    events.push({ event: eventType, payload });
  }
  return events;
}

function collectVisibleText(value) {
  if (value == null) {
    return '';
  }
  if (typeof value === 'string') {
    return value;
  }
  if (Array.isArray(value)) {
    let out = '';
    for (const item of value) {
      out += collectVisibleText(item);
    }
    return out;
  }
  if (typeof value !== 'object') {
    return '';
  }
  let out = '';
  if (typeof value.reasoning_content === 'string') {
    out += value.reasoning_content;
  }
  if (Object.prototype.hasOwnProperty.call(value, 'text')) {
    out += collectVisibleText(value.text);
  }
  if (Object.prototype.hasOwnProperty.call(value, 'content')) {
    out += collectVisibleText(value.content);
  }
  if (Object.prototype.hasOwnProperty.call(value, 'output_text')) {
    out += collectVisibleText(value.output_text);
  }
  if (Object.prototype.hasOwnProperty.call(value, 'message')) {
    out += collectVisibleText(value.message);
  }
  if (Object.prototype.hasOwnProperty.call(value, 'delta')) {
    out += collectVisibleText(value.delta);
  }
  return out;
}

function parseDeepSeekReplay(raw) {
  const events = parseSSE(raw);
  let currentType = 'thinking';
  let sawFinish = false;
  let outputText = '';
  let thinkingText = '';
  let textOutput = '';
  let parsedChunks = 0;
  let parsedOutputTokens = 0;
  let expectedOutputTokens = 0;

  for (const evt of events) {
    if (evt.event === 'finish') {
      sawFinish = true;
    }
    if (!evt.payload || evt.payload === '[DONE]' || evt.payload[0] !== '{') {
      continue;
    }
    let obj;
    try {
      obj = JSON.parse(evt.payload);
    } catch {
      continue;
    }
    parsedChunks += 1;
    const expected = extractAccumulatedTokenUsageFromRawChunk(obj);
    if (expected > 0) {
      expectedOutputTokens = expected;
    }
    const parsed = parseChunkForContent(obj, true, currentType);
    if (parsed.outputTokens > 0) {
      parsedOutputTokens = parsed.outputTokens;
    }
    currentType = parsed.newType;
    if (parsed.finished) {
      sawFinish = true;
    }
    for (const part of parsed.parts) {
      if (part.type === 'thinking') {
        const trimmed = trimContinuationOverlap(thinkingText, part.text);
        thinkingText += trimmed;
        outputText += trimmed;
      } else {
        const trimmed = trimContinuationOverlap(textOutput, part.text);
        textOutput += trimmed;
        outputText += trimmed;
      }
    }
  }

  return {
    events: events.length,
    parsedChunks,
    sawFinish,
    parsedOutputTokens,
    expectedOutputTokens,
    tokenMismatch: expectedOutputTokens > 0 && parsedOutputTokens !== expectedOutputTokens,
    outputText,
    outputChars: outputText.length,
    leakedFinishedText: outputText.includes('FINISHED'),
    leakedReferenceMarkers: /\[reference:/i.test(outputText),
    referenceLeakCount: (outputText.match(/\[reference:/gi) || []).length,
  };
}

function extractAccumulatedTokenUsageFromRawChunk(v) {
  if (Array.isArray(v)) {
    for (const item of v) {
      const n = extractAccumulatedTokenUsageFromRawChunk(item);
      if (n > 0) {
        return n;
      }
    }
    return 0;
  }
  if (!v || typeof v !== 'object') {
    return 0;
  }
  const direct = toTokenInt(v.accumulated_token_usage);
  if (direct > 0) {
    return direct;
  }
  const pathValue = typeof v.p === 'string' ? v.p.trim().toLowerCase() : '';
  if (pathValue.includes('accumulated_token_usage')) {
    const n = toTokenInt(v.v);
    if (n > 0) {
      return n;
    }
  }
  for (const value of Object.values(v)) {
    const n = extractAccumulatedTokenUsageFromRawChunk(value);
    if (n > 0) {
      return n;
    }
  }
  return 0;
}

function toTokenInt(v) {
  if (typeof v === 'number' && Number.isFinite(v)) {
    return Math.trunc(v);
  }
  if (typeof v === 'string' && v.trim() !== '') {
    const n = Number(v);
    if (Number.isFinite(n)) {
      return Math.trunc(n);
    }
  }
  return 0;
}

function parseOpenAIStream(raw) {
  const events = parseSSE(raw);
  let outputText = '';
  let parsedChunks = 0;
  let sawFinish = false;

  for (const evt of events) {
    if (evt.event === 'finish') {
      sawFinish = true;
    }
    if (!evt.payload || evt.payload === '[DONE]' || evt.payload[0] !== '{') {
      continue;
    }
    let obj;
    try {
      obj = JSON.parse(evt.payload);
    } catch {
      continue;
    }
    parsedChunks += 1;
    if (Array.isArray(obj.choices)) {
      for (const choice of obj.choices) {
        if (!choice || typeof choice !== 'object') {
          continue;
        }
        if (choice.finish_reason) {
          sawFinish = true;
        }
        if (choice.delta) {
          outputText += collectVisibleText(choice.delta);
        }
        if (choice.message) {
          outputText += collectVisibleText(choice.message);
        }
      }
    } else {
      outputText += collectVisibleText(obj);
    }
  }

  return {
    events: events.length,
    parsedChunks,
    sawFinish,
    outputText,
    outputChars: outputText.length,
  };
}

function parseOpenAIJSON(raw) {
  let obj;
  try {
    obj = JSON.parse(raw);
  } catch {
    return {
      parsedChunks: 0,
      sawFinish: false,
      outputText: '',
      outputChars: 0,
    };
  }
  let outputText = '';
  let sawFinish = false;
  if (typeof obj.output_text === 'string') {
    outputText += obj.output_text;
  }
  if (Array.isArray(obj.output)) {
    for (const item of obj.output) {
      outputText += collectVisibleText(item);
    }
  }
  if (Array.isArray(obj.choices)) {
    for (const choice of obj.choices) {
      if (!choice || typeof choice !== 'object') {
        continue;
      }
      if (choice.finish_reason) {
        sawFinish = true;
      }
      if (choice.message) {
        outputText += collectVisibleText(choice.message);
      }
      if (choice.delta) {
        outputText += collectVisibleText(choice.delta);
      }
    }
  }
  return {
    parsedChunks: 1,
    sawFinish,
    outputText,
    outputChars: outputText.length,
  };
}

function loadBaselineSample(dir, baselineRoot) {
  const sampleID = path.basename(dir);
  const roots = [];
  if (baselineRoot) {
    roots.push(path.join(baselineRoot, sampleID));
  }
  roots.push(dir);

  for (const root of roots) {
    const textPath = path.join(root, 'replay.output.txt');
    if (fs.existsSync(textPath)) {
      return {
        path: textPath,
        kind: 'text',
        raw: fs.readFileSync(textPath, 'utf8'),
      };
    }
    const legacyTextPath = path.join(root, 'openai.output.txt');
    if (fs.existsSync(legacyTextPath)) {
      return {
        path: legacyTextPath,
        kind: 'text',
        raw: fs.readFileSync(legacyTextPath, 'utf8'),
      };
    }
    const streamPath = path.join(root, 'openai.stream.sse');
    if (fs.existsSync(streamPath)) {
      return {
        path: streamPath,
        kind: 'stream',
        raw: fs.readFileSync(streamPath, 'utf8'),
      };
    }
    const jsonPath = path.join(root, 'openai.response.json');
    if (fs.existsSync(jsonPath)) {
      return {
        path: jsonPath,
        kind: 'json',
        raw: fs.readFileSync(jsonPath, 'utf8'),
      };
    }
  }
  return null;
}

function replaySample(dir, opts) {
  const raw = fs.readFileSync(path.join(dir, 'upstream.stream.sse'), 'utf8');
  const rawResult = parseDeepSeekReplay(raw);

  let replayOutputPath = '';
  if (opts.outputRoot) {
    const sampleOutputDir = path.join(opts.outputRoot, path.basename(dir));
    fs.mkdirSync(sampleOutputDir, { recursive: true });
    replayOutputPath = path.join(sampleOutputDir, 'replay.output.txt');
    fs.writeFileSync(replayOutputPath, rawResult.outputText);
  }

  const baseline = loadBaselineSample(dir, opts.baselineRoot);
  const baselineResult = baseline
    ? (baseline.kind === 'text'
      ? {
          events: 0,
          parsedChunks: 0,
          sawFinish: false,
          outputText: baseline.raw,
          outputChars: baseline.raw.length,
        }
      : baseline.kind === 'stream'
        ? parseOpenAIStream(baseline.raw)
        : parseOpenAIJSON(baseline.raw))
    : null;
  const baselineMatch = baselineResult ? baselineResult.outputText === rawResult.outputText : null;
  const baselinePreview = baselineResult ? previewText(baselineResult.outputText, 280) : '';
  const errors = [];

  if (opts.failOnMissingFinish && !rawResult.sawFinish) {
    errors.push('missing finish signal');
  }
  if (opts.failOnLeak && rawResult.leakedFinishedText) {
    errors.push('FINISHED leaked into output text');
  }
  if (opts.failOnReferenceLeak && rawResult.leakedReferenceMarkers) {
    errors.push('reference markers leaked into output text');
  }
  if (baselineResult && opts.failOnBaselineMismatch && !baselineMatch) {
    errors.push('baseline output mismatch');
  }
  if (opts.failOnTokenMismatch && rawResult.tokenMismatch) {
    errors.push(`token mismatch expected=${rawResult.expectedOutputTokens} parsed=${rawResult.parsedOutputTokens}`);
  }

  return {
    sample_id: path.basename(dir),
    raw_events: rawResult.events,
    raw_parsed_chunks: rawResult.parsedChunks,
    raw_saw_finish: rawResult.sawFinish,
    raw_expected_output_tokens: rawResult.expectedOutputTokens,
    raw_parsed_output_tokens: rawResult.parsedOutputTokens,
    raw_token_mismatch: rawResult.tokenMismatch,
    raw_output_chars: rawResult.outputChars,
    raw_leaked_finished_text: rawResult.leakedFinishedText,
    raw_leaked_reference_markers: rawResult.leakedReferenceMarkers,
    raw_reference_leak_count: rawResult.referenceLeakCount,
    baseline_available: Boolean(baselineResult),
    baseline_path: baseline ? baseline.path : '',
    baseline_kind: baseline ? baseline.kind : '',
    baseline_parsed_chunks: baselineResult ? baselineResult.parsedChunks : 0,
    baseline_saw_finish: baselineResult ? baselineResult.sawFinish : false,
    baseline_output_chars: baselineResult ? baselineResult.outputChars : 0,
    baseline_output_matches_replay: baselineResult ? baselineMatch : null,
    baseline_output_preview: baselinePreview,
    ok: errors.length === 0,
    errors,
    replay_output_text: rawResult.outputText,
    replay_output_path: replayOutputPath,
    baseline_output_text: baselineResult ? baselineResult.outputText : '',
  };
}

function previewText(text, limit) {
  if (!text) {
    return '';
  }
  if (text.length <= limit) {
    return text;
  }
  return `${text.slice(0, limit)}...`;
}

function main() {
  const opts = parseArgs(process.argv);
  if (!opts.outputRoot && opts.writeReplayText) {
    const stamp = new Date().toISOString().replace(/[:.]/g, '-');
    opts.outputRoot = path.join('artifacts/raw-stream-sim', `adhoc-${stamp}`);
  }
  const { dirs, manifestPath } = resolveSampleDirs(opts.samplesRoot, opts.sampleId);
  if (dirs.length === 0) {
    console.error(`[sim] no samples found: ${opts.samplesRoot}`);
    process.exit(1);
  }

  const report = {
    generated_at: new Date().toISOString(),
    samples_root: opts.samplesRoot,
    manifest_path: manifestPath,
    output_root: opts.outputRoot,
    baseline_root: opts.baselineRoot,
    sample_id: opts.sampleId,
    total: dirs.length,
    failed: 0,
    samples: [],
  };

  if (manifestPath) {
    console.log(`[sim] using manifest ${manifestPath} samples=${dirs.length}`);
  }

  for (const dir of dirs) {
    const sample = replaySample(dir, opts);
    const errors = [...sample.errors];
    if (errors.length > 0) {
      report.failed += 1;
    }
    report.samples.push({
      sample_id: sample.sample_id,
      raw_events: sample.raw_events,
      raw_parsed_chunks: sample.raw_parsed_chunks,
      raw_saw_finish: sample.raw_saw_finish,
      raw_expected_output_tokens: sample.raw_expected_output_tokens,
      raw_parsed_output_tokens: sample.raw_parsed_output_tokens,
      raw_token_mismatch: sample.raw_token_mismatch,
      raw_output_chars: sample.raw_output_chars,
      raw_leaked_finished_text: sample.raw_leaked_finished_text,
      raw_leaked_reference_markers: sample.raw_leaked_reference_markers,
      raw_reference_leak_count: sample.raw_reference_leak_count,
      baseline_available: sample.baseline_available,
      baseline_path: sample.baseline_path,
      baseline_kind: sample.baseline_kind,
      baseline_parsed_chunks: sample.baseline_parsed_chunks,
      baseline_saw_finish: sample.baseline_saw_finish,
      baseline_output_chars: sample.baseline_output_chars,
      baseline_output_matches_replay: sample.baseline_output_matches_replay,
      baseline_output_preview: sample.baseline_output_preview,
      replay_output_path: sample.replay_output_path,
      ok: errors.length === 0,
      errors,
    });

    const status = sample.ok ? 'OK' : 'FAIL';
    const leakNote = sample.raw_leaked_reference_markers ? ` refLeaks=${sample.raw_reference_leak_count}` : '';
    const matchNote = sample.baseline_available
      ? ` baseline=${sample.baseline_output_matches_replay ? 'match' : 'mismatch'}`
      : ' baseline=missing';
    const note = errors.length > 0 ? ` errors=${errors.join(';')}` : '';
    console.log(`[sim] ${status} ${sample.sample_id} events=${sample.raw_events} parsed=${sample.raw_parsed_chunks} tokens=${sample.raw_parsed_output_tokens}/${sample.raw_expected_output_tokens} chars=${sample.raw_output_chars}${leakNote}${matchNote}${note}`);
    if (opts.showOutput) {
      console.log(`[sim] replay output for ${sample.sample_id}:`);
      console.log(sample.replay_output_text || '(empty)');
    }
  }

  if (opts.reportPath) {
    fs.writeFileSync(opts.reportPath, JSON.stringify(report, null, 2));
  }

  if (report.failed > 0) {
    console.error(`[sim] ${report.failed}/${report.total} samples failed`);
    process.exit(2);
  }
  console.log(`[sim] all ${report.total} samples passed`);
}

main();
