#!/usr/bin/env node
/**
 * device-harvest — produce real Shumei device tokens for DeepSeek logins.
 *
 * DeepSeek expects a real device fingerprint in the login payload `device_id`
 * field ("B" + SMID; "D" + smEncryptedData is an unverified fallback that is
 * only produced before the SMID round-trip completes). This tool generates
 * that token by loading https://chat.deepseek.com/sign_in in a real headed
 * Chromium and calling `window.SMSdk.getDeviceId()`.
 *
 * This tool NEVER submits a login and never calls /api/v0/users/*: every
 * request to that path is intercepted and aborted before it leaves the
 * browser, so harvesting cannot consume an account.
 *
 * Modes:
 *   serve [--port 8090] [--concurrency 2]
 *     Long-running HTTP service for whale2api: POST /harvest -> {"device_id":"B..."}.
 *
 *   cli --input accounts.csv [--per-account 1] [--output out.csv]
 *       [--state state.json] [--limit N] [--include-discarded]
 *     Batch pre-seed tokens for a CSV of accounts (optional).
 *
 * Headed Chromium needs an X display. Locally run it under Xvfb:
 *   xvfb-run -a node harvest.mjs serve --port 8090
 * In Docker the image entrypoint wraps the command with xvfb-run.
 */
import { chromium } from 'playwright';
import http from 'node:http';
import crypto from 'node:crypto';
import fs from 'node:fs';
import fsp from 'node:fs/promises';
import path from 'node:path';
import process from 'node:process';

const SIGN_IN_URL = 'https://chat.deepseek.com/sign_in';
const USER_API_PATH = '/api/v0/users/';
const TOKEN_DEADLINE_MS = 30_000;
const POLL_INTERVAL_MS = 200;
const NAV_TIMEOUT_MS = 60_000;
const DEFAULT_PORT = 8090;
const DEFAULT_CONCURRENCY = 2;
const MAX_CONCURRENCY = 8;
const DEFAULT_MAX_QUEUE = 64;
const MAX_BODY_BYTES = 64 * 1024;
const STATE_VERSION = 1;

// ---------------------------------------------------------------------------
// logging helpers
// ---------------------------------------------------------------------------

function ts() {
  return new Date().toISOString();
}

function log(msg) {
  console.log(`${ts()} ${msg}`);
}

function warn(msg) {
  console.warn(`${ts()} WARN ${msg}`);
}

function fail(msg) {
  console.error(`${ts()} ERROR ${msg}`);
}

/** Device tokens are fingerprint data: never log them in full. */
function maskToken(token) {
  if (!token) return '(none)';
  return `${token.slice(0, 6)}…(${token.length})`;
}

// ---------------------------------------------------------------------------
// small utilities
// ---------------------------------------------------------------------------

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

function delay(ms, signal) {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(new Error('request cancelled'));
      return;
    }
    const timer = setTimeout(() => {
      signal?.removeEventListener('abort', onAbort);
      resolve();
    }, ms);
    const onAbort = () => {
      clearTimeout(timer);
      reject(new Error('request cancelled'));
    };
    signal?.addEventListener('abort', onAbort, { once: true });
  });
}

function toPositiveInt(raw, fallback) {
  if (raw === undefined || raw === null || raw === '' || raw === true) return fallback;
  const n = Number.parseInt(String(raw), 10);
  if (!Number.isFinite(n) || n < 1) {
    throw new Error(`expected a positive integer, got "${raw}"`);
  }
  return n;
}

function envInt(name, fallback, min, max) {
  const raw = process.env[name];
  if (raw === undefined || raw.trim() === '') return fallback;
  const n = Number.parseInt(raw, 10);
  if (!Number.isFinite(n)) {
    warn(`ignoring invalid ${name}="${raw}"`);
    return fallback;
  }
  return Math.min(max, Math.max(min, n));
}

function stripBOM(text) {
  return text.charCodeAt(0) === 0xfeff ? text.slice(1) : text;
}

function keyFor(identifier) {
  return String(identifier || '').trim().toLowerCase();
}

async function writeFileAtomic(file, data) {
  await fsp.mkdir(path.dirname(file), { recursive: true });
  const tmp = `${file}.tmp-${process.pid}`;
  await fsp.writeFile(tmp, data, 'utf8');
  await fsp.rename(tmp, file);
}

// ---------------------------------------------------------------------------
// HTTP helpers (serve mode)
// ---------------------------------------------------------------------------

function sendJSON(res, status, payload) {
  if (res.headersSent || res.writableEnded || res.destroyed) return;
  const body = JSON.stringify(payload);
  res.writeHead(status, {
    'Content-Type': 'application/json; charset=utf-8',
    'Content-Length': Buffer.byteLength(body),
    'Cache-Control': 'no-store',
  });
  res.end(body);
}

function readJSONBody(req) {
  return new Promise((resolve, reject) => {
    let settled = false;
    let size = 0;
    const chunks = [];
    const done = (fn, value) => {
      if (settled) return;
      settled = true;
      fn(value);
    };
    req.on('data', (chunk) => {
      if (settled) return;
      size += chunk.length;
      if (size > MAX_BODY_BYTES) {
        done(reject, new Error(`request body exceeds ${MAX_BODY_BYTES} bytes`));
        req.destroy();
        return;
      }
      chunks.push(chunk);
    });
    req.on('end', () => {
      const raw = Buffer.concat(chunks).toString('utf8').trim();
      if (raw === '') {
        done(resolve, {});
        return;
      }
      try {
        done(resolve, JSON.parse(raw));
      } catch {
        done(reject, new Error('invalid JSON body'));
      }
    });
    req.on('error', (err) => done(reject, err));
  });
}

function tokenMatches(provided, expected) {
  const a = Buffer.from(String(provided ?? ''), 'utf8');
  const b = Buffer.from(String(expected), 'utf8');
  return a.length === b.length && crypto.timingSafeEqual(a, b);
}

// ---------------------------------------------------------------------------
// harvest core
// ---------------------------------------------------------------------------

async function launchBrowser() {
  if (!process.env.DISPLAY) {
    throw new Error(
      'headed Chromium requires an X display: run under Xvfb (e.g. `xvfb-run -a node harvest.mjs ...`) ' +
        'or use the device-harvest container (its entrypoint uses xvfb-run)'
    );
  }
  return chromium.launch({
    headless: false,
    // Keep Chromium from advertising itself as automation.
    ignoreDefaultArgs: ['--enable-automation'],
    args: [
      '--no-sandbox',
      '--disable-dev-shm-usage',
      '--disable-blink-features=AutomationControlled',
    ],
  });
}

/**
 * Intercepts every /api/v0/users/* request in the context and aborts it before
 * it is sent. Returns a live counter so callers can prove no login was tried.
 */
async function blockUserAPI(context, accountId) {
  const counter = { blocked: 0 };
  await context.route(
    (url) => url.pathname.includes(USER_API_PATH),
    async (route) => {
      counter.blocked += 1;
      const request = route.request();
      let pathname = '';
      try {
        pathname = new URL(request.url()).pathname;
      } catch {
        pathname = request.url();
      }
      warn(
        `[harvest] BLOCKED ${request.method()} ${pathname} (login requests are never sent) ` +
          `account=${accountId || 'unknown'} count=${counter.blocked}`
      );
      await route.abort('blockedbyclient').catch(() => {});
    }
  );
  return counter;
}

async function readDeviceId(page) {
  try {
    return await page.evaluate(() => {
      try {
        const sdk = window.SMSdk;
        if (!sdk || typeof sdk.getDeviceId !== 'function') return '';
        const value = sdk.getDeviceId();
        return typeof value === 'string' ? value : '';
      } catch {
        return '';
      }
    });
  } catch {
    return ''; // page busy/navigating: keep polling until the deadline
  }
}

/**
 * Creates a fresh browser context, loads the sign-in page and polls
 * window.SMSdk.getDeviceId() every 200ms until a "B"-prefixed token appears.
 * "D"-prefixed fallbacks are observed but never returned.
 */
async function harvestOnce(browser, { accountId = '', deadlineMs = TOKEN_DEADLINE_MS, signal } = {}) {
  if (signal?.aborted) throw new Error('request cancelled');
  if (!browser.isConnected()) throw new Error('Chromium is not connected');
  const startedAt = Date.now();
  const context = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    locale: 'zh-CN',
    timezoneId: 'Asia/Shanghai',
    serviceWorkers: 'block',
  });
  try {
    const counter = await blockUserAPI(context, accountId);
    await context.addInitScript(() => {
      Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
    });
    const page = await context.newPage();
    await page.goto(SIGN_IN_URL, { waitUntil: 'domcontentloaded', timeout: NAV_TIMEOUT_MS });
    const pollStartedAt = Date.now();
    const deadline = pollStartedAt + deadlineMs;
    let lastFallback = '';
    for (;;) {
      if (signal?.aborted) throw new Error('request cancelled');
      const token = await readDeviceId(page);
      if (token) {
        if (token.startsWith('B')) {
          return { token, elapsedMs: Date.now() - startedAt, blocked: counter.blocked };
        }
        lastFallback = token;
      }
      if (Date.now() >= deadline) break;
      await delay(POLL_INTERVAL_MS, signal);
    }
    const title = await page.title().catch(() => '');
    const hint = lastFallback ? ` (only saw a "${lastFallback[0]}"-prefixed fallback)` : '';
    throw new Error(
      `no "B"-prefixed device token within ${deadlineMs}ms${hint}` +
        `${title ? ` page title="${title.slice(0, 80)}"` : ''}`
    );
  } finally {
    await context.close().catch(() => {});
  }
}

/**
 * Bounded worker pool: at most `concurrency` harvests run at once, up to
 * `maxQueue` wait in line; beyond that requests fail fast with QUEUE_FULL.
 */
function createLimiter(concurrency, maxQueue) {
  let active = 0;
  const waiting = [];

  function pump() {
    while (active < concurrency && waiting.length > 0) {
      const job = waiting.shift();
      active += 1;
      Promise.resolve()
        .then(job.fn)
        .then(job.resolve, job.reject)
        .finally(() => {
          active -= 1;
          pump();
        });
    }
  }

  return {
    run(fn) {
      return new Promise((resolve, reject) => {
        if (waiting.length >= maxQueue) {
          const err = new Error(`harvest queue is full (${waiting.length} waiting, max ${maxQueue}); retry later`);
          err.code = 'QUEUE_FULL';
          reject(err);
          return;
        }
        waiting.push({ fn, resolve, reject });
        pump();
      });
    },
    stats() {
      return { active, queued: waiting.length, concurrency, maxQueue };
    },
  };
}

// ---------------------------------------------------------------------------
// serve mode
// ---------------------------------------------------------------------------

async function runServe(argv) {
  const { flags } = parseFlags(argv);
  if (flags.help) {
    printUsage();
    return;
  }
  const port =
    flags.port !== undefined ? toPositiveInt(flags.port, DEFAULT_PORT) : envInt('PORT', DEFAULT_PORT, 1, 65535);
  if (port > 65535) throw new Error(`invalid --port ${port}`);
  const concurrency =
    flags.concurrency !== undefined
      ? Math.min(MAX_CONCURRENCY, toPositiveInt(flags.concurrency, DEFAULT_CONCURRENCY))
      : envInt('HARVEST_CONCURRENCY', DEFAULT_CONCURRENCY, 1, MAX_CONCURRENCY);
  const maxQueue = envInt('HARVEST_MAX_QUEUE', DEFAULT_MAX_QUEUE, 1, 4096);
  const sharedToken = (process.env.HARVEST_TOKEN || '').trim();

  const browser = await launchBrowser();
  const limiter = createLimiter(concurrency, maxQueue);
  let shuttingDown = false;

  browser.on('disconnected', () => {
    if (!shuttingDown) {
      fail('[serve] Chromium disconnected unexpectedly; exiting so the service can be restarted');
      process.exit(1);
    }
  });

  async function handleHTTP(req, res) {
    let pathname = '/';
    try {
      pathname = new URL(req.url, 'http://127.0.0.1').pathname;
    } catch {
      /* keep '/' */
    }

    if (pathname === '/healthz') {
      if (req.method !== 'GET' && req.method !== 'HEAD') {
        sendJSON(res, 405, { error: 'method not allowed' });
        return;
      }
      const browserUp = browser.isConnected();
      const ok = browserUp && !shuttingDown;
      sendJSON(res, ok ? 200 : 503, {
        status: ok ? 'ok' : 'degraded',
        browser: browserUp ? 'up' : 'down',
        ...limiter.stats(),
      });
      return;
    }

    if (pathname === '/harvest') {
      if (req.method !== 'POST') {
        sendJSON(res, 405, { error: 'method not allowed' });
        return;
      }
      if (sharedToken && !tokenMatches(req.headers['x-harvest-token'], sharedToken)) {
        warn('[serve] rejected /harvest: bad or missing X-Harvest-Token');
        sendJSON(res, 401, { error: 'invalid harvest token' });
        return;
      }
      let body;
      try {
        body = await readJSONBody(req);
      } catch (err) {
        sendJSON(res, 400, { error: err?.message || String(err) });
        return;
      }
      const accountId = typeof body?.account_id === 'string' ? body.account_id.trim() : '';
      const controller = new AbortController();
      res.on('close', () => {
        if (!res.writableEnded) controller.abort();
      });
      const requestStartedAt = Date.now();
      try {
        const result = await limiter.run(() => harvestOnce(browser, { accountId, signal: controller.signal }));
        log(
          `[harvest] ok account=${accountId || 'unknown'} token=${maskToken(result.token)} ` +
            `elapsed=${result.elapsedMs}ms total=${Date.now() - requestStartedAt}ms ` +
            `blocked_users_api=${result.blocked}`
        );
        sendJSON(res, 200, { device_id: result.token });
      } catch (err) {
        const status = err?.code === 'QUEUE_FULL' ? 503 : 502;
        warn(
          `[harvest] failed account=${accountId || 'unknown'} after ${Date.now() - requestStartedAt}ms: ` +
            `${err?.message || err}`
        );
        sendJSON(res, status, { error: err?.message || String(err) });
      }
      return;
    }

    sendJSON(res, 404, { error: 'not found' });
  }

  const server = http.createServer((req, res) => {
    handleHTTP(req, res).catch((err) => {
      fail(`[serve] request handler crashed: ${err?.stack || err}`);
      sendJSON(res, 500, { error: 'internal error' });
    });
  });

  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(port, '0.0.0.0', () => {
      server.removeListener('error', reject);
      resolve();
    });
  });
  log(
    `[serve] listening on 0.0.0.0:${port} (concurrency=${concurrency}, maxQueue=${maxQueue}, ` +
      `auth=${sharedToken ? 'on' : 'off'})`
  );

  const shutdown = async (signal) => {
    if (shuttingDown) return;
    shuttingDown = true;
    log(`[serve] ${signal} received, shutting down`);
    server.close();
    await browser.close().catch(() => {});
    process.exit(0);
  };
  process.once('SIGINT', () => void shutdown('SIGINT'));
  process.once('SIGTERM', () => void shutdown('SIGTERM'));
}

// ---------------------------------------------------------------------------
// CSV helpers (cli mode)
// ---------------------------------------------------------------------------

/** Minimal RFC4180-ish parser: quoted fields, escaped quotes, LF normalized. */
function parseCSV(text) {
  const normalized = text.replace(/\r\n?/g, '\n');
  const rows = [];
  let row = [];
  let field = '';
  let inQuotes = false;
  for (let i = 0; i < normalized.length; i += 1) {
    const ch = normalized[i];
    if (inQuotes) {
      if (ch === '"') {
        if (normalized[i + 1] === '"') {
          field += '"';
          i += 1;
        } else {
          inQuotes = false;
        }
      } else {
        field += ch;
      }
      continue;
    }
    if (ch === '"') {
      inQuotes = true;
      continue;
    }
    if (ch === ',') {
      row.push(field);
      field = '';
      continue;
    }
    if (ch === '\n') {
      row.push(field);
      field = '';
      rows.push(row);
      row = [];
      continue;
    }
    field += ch;
  }
  if (field !== '' || row.length > 0) {
    row.push(field);
    rows.push(row);
  }
  return rows.filter((cells) => cells.some((cell) => String(cell).trim() !== ''));
}

function csvField(value) {
  const s = value === undefined || value === null ? '' : String(value);
  return /[",\n\r]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

function toCSV(rows) {
  return `${rows.map((row) => row.map(csvField).join(',')).join('\n')}\n`;
}

/**
 * Parses email,password[,extra] rows. Accepts the legacy
 * `email,password,discarded` layout and the pool import layout
 * `email,password[,device_id]` (header names are respected).
 */
function parseAccountsCSV(text) {
  const rows = parseCSV(stripBOM(text));
  if (rows.length === 0) return { header: null, accounts: [] };
  const firstCells = rows[0].map((cell) => cell.trim().toLowerCase());
  const hasHeader = firstCells.includes('email') || firstCells.includes('identifier') || firstCells.includes('password');
  const header = hasHeader ? rows[0].map((cell) => cell.trim()) : null;
  let discardedCol = -1;
  if (header) {
    discardedCol = header.findIndex((h) => h.toLowerCase() === 'discarded');
  }
  const dataRows = hasHeader ? rows.slice(1) : rows;
  const accounts = [];
  for (const row of dataRows) {
    const email = (row[0] || '').trim();
    if (email === '') continue;
    let discarded = false;
    if (discardedCol >= 0) {
      discarded = String(row[discardedCol] || '').trim().toLowerCase() === 'true';
    } else if (!hasHeader) {
      // Legacy headerless layout: email,password,discarded.
      discarded = String(row[2] || '').trim().toLowerCase() === 'true';
    }
    accounts.push({ email, password: row[1] || '', discarded });
  }
  return { header, accounts };
}

function defaultOutputPath(inputPath) {
  const dir = path.dirname(inputPath);
  const base = path.basename(inputPath).replace(/\.csv$/i, '');
  return path.join(dir, `${base}.device-ids.csv`);
}

async function loadState(statePath) {
  try {
    const parsed = JSON.parse(await fsp.readFile(statePath, 'utf8'));
    if (parsed && typeof parsed === 'object' && parsed.accounts && typeof parsed.accounts === 'object') {
      return parsed;
    }
    warn(`[cli] ignoring state file ${statePath}: unexpected shape`);
  } catch (err) {
    if (err?.code !== 'ENOENT') warn(`[cli] could not read state ${statePath}: ${err?.message || err}`);
  }
  return { version: STATE_VERSION, accounts: {} };
}

async function saveState(statePath, state) {
  state.version = STATE_VERSION;
  state.updated_at = new Date().toISOString();
  await writeFileAtomic(statePath, `${JSON.stringify(state, null, 2)}\n`);
}

async function writeAccountsOutput(outputPath, accounts, state) {
  const rows = [['email', 'password', 'device_id']];
  for (const account of accounts) {
    const entry = state.accounts[keyFor(account.email)];
    const tokens = Array.isArray(entry?.tokens) ? entry.tokens.filter((t) => typeof t === 'string' && t) : [];
    rows.push([account.email, account.password, tokens.join('|')]);
  }
  await writeFileAtomic(outputPath, toCSV(rows));
}

// ---------------------------------------------------------------------------
// cli mode
// ---------------------------------------------------------------------------

async function runCli(argv) {
  const { flags } = parseFlags(argv);
  if (flags.help) {
    printUsage();
    return;
  }
  if (!flags.input || flags.input === true) throw new Error('cli mode requires --input <accounts.csv>');
  const inputPath = path.resolve(flags.input);
  const perAccount = toPositiveInt(flags['per-account'], 1);
  if (perAccount > 10) throw new Error('--per-account is capped at 10');
  const outputPath = path.resolve(
    flags.output && flags.output !== true ? flags.output : defaultOutputPath(inputPath)
  );
  const statePath = path.resolve(flags.state && flags.state !== true ? flags.state : `${outputPath}.state.json`);
  const limit = flags.limit === undefined ? Number.POSITIVE_INFINITY : toPositiveInt(flags.limit, Number.POSITIVE_INFINITY);
  const includeDiscarded = Boolean(flags['include-discarded']);

  const { accounts } = parseAccountsCSV(await fsp.readFile(inputPath, 'utf8'));
  if (accounts.length === 0) throw new Error(`no accounts found in ${inputPath}`);

  const state = await loadState(statePath);
  if (!state.accounts || typeof state.accounts !== 'object') state.accounts = {};

  const selected = accounts.filter((account) => includeDiscarded || !account.discarded);
  const pending = [];
  for (const account of selected) {
    const key = keyFor(account.email);
    const entry = state.accounts[key];
    const tokens = Array.isArray(entry?.tokens) ? entry.tokens.filter((t) => typeof t === 'string' && t.startsWith('B')) : [];
    state.accounts[key] = { ...(entry || {}), tokens };
    if (tokens.length < perAccount) pending.push({ account, key, tokens });
  }
  const thisRun = Number.isFinite(limit) ? pending.slice(0, limit) : pending;

  log(`[cli] input=${inputPath}`);
  log(`[cli] output=${outputPath} state=${statePath}`);
  log(
    `[cli] accounts=${selected.length} (skipped_discarded=${accounts.length - selected.length}) ` +
      `need_tokens=${pending.length} this_run=${thisRun.length} per_account=${perAccount}`
  );

  await writeAccountsOutput(outputPath, accounts, state);

  if (thisRun.length === 0) {
    log('[cli] nothing to do: every selected account already has enough tokens');
    return;
  }

  const browser = await launchBrowser();
  let failures = 0;
  let harvested = 0;
  try {
    for (let i = 0; i < thisRun.length; i += 1) {
      const { account, key, tokens } = thisRun[i];
      const label = `[${i + 1}/${thisRun.length}]`;
      try {
        while (tokens.length < perAccount) {
          const result = await harvestOnce(browser, { accountId: account.email });
          tokens.push(result.token);
          harvested += 1;
          log(
            `[cli] ${label} ${account.email} token=${maskToken(result.token)} ` +
              `(${result.elapsedMs}ms, blocked_users_api=${result.blocked})`
          );
        }
        state.accounts[key] = { tokens: [...tokens], updated_at: new Date().toISOString() };
      } catch (err) {
        failures += 1;
        warn(`[cli] ${label} ${account.email} failed: ${err?.message || err}`);
        state.accounts[key] = {
          tokens: [...tokens],
          updated_at: new Date().toISOString(),
          last_error: String(err?.message || err),
        };
      }
      await saveState(statePath, state).catch((err) => warn(`[cli] save state failed: ${err?.message || err}`));
      await writeAccountsOutput(outputPath, accounts, state).catch((err) =>
        warn(`[cli] write output failed: ${err?.message || err}`)
      );
    }
  } finally {
    await browser.close().catch(() => {});
  }

  log(`[cli] done: harvested=${harvested} failures=${failures}`);
  if (failures > 0) {
    process.exitCode = 1;
  }
}

// ---------------------------------------------------------------------------
// argument parsing / usage
// ---------------------------------------------------------------------------

function parseFlags(args) {
  const flags = {};
  for (let i = 0; i < args.length; i += 1) {
    const arg = args[i];
    if (arg === '--help' || arg === '-h') {
      flags.help = true;
      continue;
    }
    if (!arg.startsWith('--')) continue; // positional args are ignored
    const eq = arg.indexOf('=');
    if (eq !== -1) {
      flags[arg.slice(2, eq)] = arg.slice(eq + 1);
      continue;
    }
    const next = args[i + 1];
    if (next === undefined || next.startsWith('--')) {
      flags[arg.slice(2)] = true;
    } else {
      flags[arg.slice(2)] = next;
      i += 1;
    }
  }
  return { flags };
}

function printUsage() {
  console.log(`device-harvest — real Shumei device tokens for DeepSeek logins

Usage:
  node harvest.mjs serve [--port 8090] [--concurrency 2]
  node harvest.mjs cli --input accounts.csv [--per-account 1]
      [--output out.csv] [--state state.json] [--limit N] [--include-discarded]

serve:
  GET  /healthz                -> 200 {"status":"ok",...}
  POST /harvest                -> 200 {"device_id":"B..."}
       body: {"account_id":"..."}   header: X-Harvest-Token (when HARVEST_TOKEN is set)
  Env: PORT, HARVEST_TOKEN, HARVEST_CONCURRENCY (default 2), HARVEST_MAX_QUEUE (default 64)

cli:
  Reads email,password[,discarded|device_id] rows and writes
  email,password,device_id (multiple tokens joined with "|", import takes the
  first one), resumable via --state.
  Env: none (uses the same browser options as serve).

Headed Chromium needs a display; run locally with:
  xvfb-run -a node harvest.mjs serve --port 8090
`);
}

// ---------------------------------------------------------------------------
// entrypoint
// ---------------------------------------------------------------------------

async function main() {
  const [mode, ...rest] = process.argv.slice(2);
  if (!mode || mode === 'help') {
    printUsage();
    process.exit(mode ? 0 : 1);
  }
  if (mode === '--help' || mode === '-h') {
    printUsage();
    return;
  }

  process.on('unhandledRejection', (err) => {
    fail(`unhandled rejection: ${err?.stack || err}`);
    process.exit(1);
  });
  process.on('uncaughtException', (err) => {
    fail(`uncaught exception: ${err?.stack || err}`);
    process.exit(1);
  });

  if (mode === 'serve') {
    await runServe(rest);
  } else if (mode === 'cli') {
    await runCli(rest);
  } else {
    fail(`unknown mode "${mode}"`);
    printUsage();
    process.exit(1);
  }
}

main().catch((err) => {
  fail(err?.stack || String(err));
  process.exit(1);
});
