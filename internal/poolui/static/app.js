const TOKEN_KEY = "pool_ui_token";

const ICON_TEST =
  '<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polygon points="6,4 6,12 12,8"/></svg>';
const ICON_DISCARD =
  '<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3.5 5h9"/><path d="M6.5 5V4a1 1 0 0 1 1-1h1a1 1 0 0 1 1 1v1"/><path d="M5.5 5l.5 8.5h4l.5-8.5"/></svg>';
const ICON_RESTORE =
  '<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3.5 8a4.5 4.5 0 0 1 7.7-3.2"/><path d="M12.5 3.5V6h-2.5"/><path d="M12.5 8a4.5 4.5 0 0 1-7.7 3.2"/><path d="M3.5 12.5V10H6"/></svg>';

const PAGE_SIZE = 50;

const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => [...document.querySelectorAll(sel)];

let state = {
  token: sessionStorage.getItem(TOKEN_KEY) || "",
  keys: [],
  currentKey: null,
  accounts: [],
  tab: "active",
  page: 1,
  testStatus: {},
  lastFailedIds: [],
  testing: false,
  testProgress: { done: 0, total: 0 },
  testProgressEverShown: false,
  testPollTimer: null,
  testPollResolve: null,
  testCancelled: false,
  userStoppedTest: false,
};

function headers() {
  return {
    Authorization: `Bearer ${state.token}`,
    "Content-Type": "application/json",
  };
}

async function api(path, opts = {}) {
  const res = await fetch(path, {
    ...opts,
    headers: { ...headers(), ...(opts.headers || {}) },
  });
  let data = {};
  try {
    data = await res.json();
  } catch (_) {}
  if (!res.ok) {
    const msg = data.detail || data.message || res.statusText;
    throw new Error(msg);
  }
  return data;
}

function encKey(key) {
  return encodeURIComponent(key);
}

function encIdent(id) {
  return encodeURIComponent(id);
}

function toast(msg, isErr) {
  const el = $("#toast");
  el.textContent = msg;
  el.classList.toggle("hidden", false);
  el.classList.toggle("is-error", !!isErr);
  clearTimeout(toast._t);
  toast._t = setTimeout(() => el.classList.add("hidden"), 3500);
}

function showApp() {
  $("#loginPage").classList.add("hidden");
  $("#app").classList.remove("hidden");
  setInspectorMode(false);
}

function showLogin() {
  $("#loginPage").classList.remove("hidden");
  $("#app").classList.add("hidden");
}

function openDialog(id) {
  const d = $(id);
  if (d && !d.open) d.showModal();
}

function closeDialogs() {
  $$("dialog").forEach((d) => d.close());
}

function poolDisplayTitle(row) {
  if (row?.name?.trim()) return row.name.trim();
  return "未命名号池";
}

function setInspectorMode(hasPool) {
  $("#inspectorIdle")?.classList.toggle("hidden", hasPool);
  $("#inspectorActive")?.classList.toggle("hidden", !hasPool);
}

function discardFailedCount() {
  return state.lastFailedIds.filter((id) => {
    const acc = state.accounts.find((a) => a.identifier === id);
    return acc && !acc.discarded;
  }).length;
}

function mutedCount() {
  return state.accounts.filter((a) => a.discarded && a.discard_reason === "muted").length;
}

function setTestingUI(testing) {
  state.testing = testing;
  const busy = [
    "#btnTestAll",
    "#btnTestMuted",
    "#btnDiscardFailed",
    "#btnRotateKey",
    "#btnDeletePool",
    "#btnRenamePool",
    "#btnExportCSV",
    "#btnImportCSV",
    "#btnViewKey",
  ];
  busy.forEach((sel) => {
    const el = $(sel);
    if (el) el.disabled = testing;
  });
  $$(".tab").forEach((b) => { b.disabled = testing; });
  const stopBtn = $("#btnStopTest");
  if (stopBtn) stopBtn.disabled = testing ? false : true;
}

function progressPercent(done, total, testing) {
  if (total <= 0) return 0;
  let value = done;
  if (testing && done < total) value = done + 0.5;
  let pct = Math.round((value / total) * 100);
  if (testing) {
    pct = Math.max(pct, 8);
    if (done < total) pct = Math.min(pct, 99);
  } else if (done >= total) {
    pct = 100;
  }
  return pct;
}

function updateProgressUI() {
  const slot = $("#mainProgressSlot");
  const wrap = $("#testProgressWrap");
  const fill = $("#testProgressFill");
  if (!wrap || !fill) return;
  const { done, total } = state.testProgress;
  slot?.classList.toggle("is-active", state.testing);
  wrap.classList.toggle("is-visible", state.testing);
  if (!state.testing) {
    fill.style.width = "0%";
    fill.classList.remove("is-active");
    return;
  }
  const pct = progressPercent(done, total, state.testing);
  fill.style.width = `${pct}%`;
  fill.classList.toggle("is-active", state.testing);
}

function updateTestModeButtons() {
  const isDiscardedTab = state.tab === "discarded";
  $("#btnTestAll")?.classList.toggle("hidden", isDiscardedTab);
  $("#btnTestMuted")?.classList.toggle("hidden", !isDiscardedTab);
}

function updateDiscardFailedButton() {
  const btn = $("#btnDiscardFailed");
  const desc = $("#btnDiscardFailedDesc");
  if (!btn) return;
  const count = discardFailedCount();
  if (desc) desc.textContent = count > 0 ? `${count} 个失败账号` : "—";
  btn.classList.toggle("hidden", count === 0);
}

function updateMutedTestButton() {
  const desc = $("#btnTestMutedDesc");
  if (!desc) return;
  const count = mutedCount();
  desc.textContent = count > 0 ? `${count} 个禁言账号` : "检测禁言账号";
}

function accountOtherTitle(a) {
  if (a.token_preview && a.token) return a.token;
  return accountOtherText(a);
}

function poolStatusHTML(a) {
  if (!a.discarded) return '<span class="badge ok">可用</span>';
  if (a.discard_reason === "muted") return '<span class="badge off">禁言</span>';
  if (a.discard_reason === "banned") return '<span class="badge off">封号</span>';
  const label = a.pool_status_text || "已作废";
  return `<span class="badge off">${escapeHtml(label)}</span>`;
}

function accountStatusHTML(a) {
  const t = state.testStatus[a.identifier];
  if (t) return testStatusBadgeHTML(a.identifier);
  return poolStatusHTML(a);
}

function formatAccountDate(value) {
  if (!value) return "";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return String(value);
  return d.toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function accountOtherText(a) {
  if (a.discard_reason === "muted" && a.mute_until) {
    return `解禁 ${formatAccountDate(a.mute_until)}`;
  }
  if (a.token_preview) return a.token_preview;
  return "";
}

function accountOtherHTML(a) {
  const text = accountOtherText(a);
  if (!text) return "";
  const title = accountOtherTitle(a);
  return `<span class="cell-ellipsis" title="${escapeAttr(title)}">${escapeHtml(text)}</span>`;
}

function paginateRows(rows) {
  const total = rows.length;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  if (state.page > totalPages) state.page = totalPages;
  if (state.page < 1) state.page = 1;
  const start = (state.page - 1) * PAGE_SIZE;
  return {
    pageRows: rows.slice(start, start + PAGE_SIZE),
    total,
    totalPages,
    page: state.page,
  };
}

function renderPagination(meta) {
  const el = $("#accountPagination");
  if (!el) return;
  if (meta.total <= PAGE_SIZE) {
    el.innerHTML = "";
    return;
  }
  const prevDisabled = meta.page <= 1 ? "disabled" : "";
  const nextDisabled = meta.page >= meta.totalPages ? "disabled" : "";
  el.innerHTML = `
    <span class="pagination-info">第 ${meta.page} / ${meta.totalPages} 页 · 共 ${meta.total} 条</span>
    <button type="button" class="btn sm" data-page="prev" ${prevDisabled}>上一页</button>
    <button type="button" class="btn sm" data-page="next" ${nextDisabled}>下一页</button>
  `;
}

function applyTestResult(ident, data) {
  if (data.skipped) {
    state.testStatus[ident] = { status: "skip", message: data.message || "跳过" };
    return;
  }
  if (data.ok) {
    state.testStatus[ident] = {
      status: "ok",
      message: "可用",
      token_updated: !!data.token_updated,
    };
    const acc = state.accounts.find((a) => a.identifier === ident);
    if (acc) {
      if (data.token_updated) {
        acc.has_token = true;
        acc.token_preview = "已更新";
      }
      acc.discarded = false;
      acc.discard_reason = "";
      acc.pool_status_text = "可用";
    }
    return;
  }
  const acc = state.accounts.find((a) => a.identifier === ident);
  if (acc && data.auto_discarded) {
    acc.discarded = true;
    acc.discard_reason = data.discard_reason || "";
    if (data.discard_reason === "muted" && data.mute_until) {
      acc.mute_until = data.mute_until;
    }
    acc.pool_status_text =
      data.discard_reason === "muted"
        ? "禁言"
        : data.discard_reason === "banned"
          ? "封号"
          : "已作废";
  }
  state.testStatus[ident] = {
    status: "fail",
    message: data.message || "失败",
    pool_status: data.pool_status,
    auto_discarded: !!data.auto_discarded,
  };
  if (!data.auto_discarded && !state.lastFailedIds.includes(ident)) {
    state.lastFailedIds.push(ident);
  }
}

function testStatusBadgeHTML(identifier) {
  const t = state.testStatus[identifier];
  if (!t) return '<span class="muted">—</span>';
  switch (t.status) {
    case "pending":
      return '<span class="badge warn">等待</span>';
    case "testing":
      return '<span class="badge testing">测号中</span>';
    case "ok":
      return '<span class="badge ok">可用</span>';
    case "skip":
      return '<span class="badge warn">跳过</span>';
    case "fail": {
      const tag =
        t.pool_status === "muted"
          ? "禁言"
          : t.pool_status === "banned"
            ? "封号"
            : t.pool_status === "transport"
              ? "网络异常"
              : "失败";
      return `<span class="badge off">${tag}</span>`;
    }
    default:
      return '<span class="muted">—</span>';
  }
}

async function loadKeys() {
  const data = await api("/api/keys");
  state.keys = data.keys || [];
  renderKeys();
}

function generateAPIKey() {
  const bytes = crypto.getRandomValues(new Uint8Array(24));
  let bin = "";
  bytes.forEach((b) => { bin += String.fromCharCode(b); });
  const b64 = btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  return `sk-${b64}`;
}

function fillNewKeyInput() {
  $("#newKeyValue").value = generateAPIKey();
}

function fillRotateKeyInput() {
  $("#rotateNew").value = generateAPIKey();
}

function renderKeys() {
  const ul = $("#keyList");
  ul.innerHTML = "";
  state.keys.forEach((k) => {
    const li = document.createElement("li");
    li.className = k.api_key === state.currentKey ? "active" : "";
    const title = poolDisplayTitle(k);
    li.innerHTML = `
      <div class="key-name">${escapeHtml(title)}</div>
      <div class="key-meta">${k.pool_size} 可用 · ${k.enabled ? "启用" : "停用"}</div>
    `;
    li.addEventListener("click", () => selectKey(k.api_key));
    ul.appendChild(li);
  });
}

function stopTestPoll(resolveWith) {
  if (state.testPollTimer) {
    clearInterval(state.testPollTimer);
    state.testPollTimer = null;
  }
  if (state.testPollResolve) {
    const resolve = state.testPollResolve;
    state.testPollResolve = null;
    resolve(resolveWith || null);
  }
}

function isTestJobActive(job) {
  return job && job.status === "running" && !state.userStoppedTest;
}

function resetTestUI() {
  state.testStatus = {};
  state.testProgress = { done: 0, total: 0 };
  state.testProgressEverShown = false;
  state.testCancelled = false;
  state.userStoppedTest = false;
  state.lastFailedIds = [];
  setTestingUI(false);
  updateProgressUI();
  renderAccounts();
}

function onTestPollFinished(job) {
  if (!job) return;
  if (job.status === "cancelled") {
    resetTestUI();
    toast("测号已中止");
  } else if (job.status === "completed") {
    toast(`测号完成：成功 ${job.ok || 0}，失败 ${job.failed || 0}`);
  }
}

function startTestPolling({ awaitDone = false } = {}) {
  stopTestPoll();
  if (awaitDone) {
    return new Promise((resolve, reject) => {
      state.testPollResolve = resolve;
      state.testPollTimer = setInterval(async () => {
        try {
          const job = await fetchTestJob();
          if (!job) return;
          syncTestJobFromServer(job);
          if (!isTestJobActive(job)) {
            stopTestPoll(job);
            if (job.status !== "running") {
              if (job.status === "cancelled" || state.userStoppedTest) resetTestUI();
              await loadAccounts();
              await loadKeys();
              updateDiscardFailedButton();
              updateMutedTestButton();
              onTestPollFinished(job);
            }
          }
        } catch (err) {
          stopTestPoll();
          state.testPollResolve = null;
          reject(err);
        }
      }, 800);
    });
  }
  state.testPollTimer = setInterval(async () => {
    const job = await fetchTestJob();
    if (!job) return;
    syncTestJobFromServer(job);
    if (!isTestJobActive(job)) {
      stopTestPoll(job);
      if (job.status !== "running") {
        if (job.status === "cancelled" || state.userStoppedTest) resetTestUI();
        await loadAccounts();
        await loadKeys();
        updateDiscardFailedButton();
        updateMutedTestButton();
        onTestPollFinished(job);
      }
    }
  }, 800);
}

function syncTestJobFromServer(job) {
  if (!job || job.status === "idle") return;
  state.testProgress = { done: job.done || 0, total: job.total || 0 };
  if (job.total > 0 || job.status === "running") state.testProgressEverShown = true;
  state.testCancelled = job.status === "cancelled";
  const running = job.status === "running" && !state.userStoppedTest;
  if (job.status === "cancelled" || job.status === "completed") state.userStoppedTest = false;
  setTestingUI(running);
  (job.results || []).forEach((row) => {
    if (row?.identifier) applyTestResult(row.identifier, row);
  });
  updateProgressUI();
  renderAccounts();
}

async function fetchTestJob() {
  if (!state.currentKey) return null;
  try {
    return await api(`/api/keys/${encKey(state.currentKey)}/accounts/test`);
  } catch (_) {
    return null;
  }
}

async function restoreTestJob() {
  stopTestPoll();
  const job = await fetchTestJob();
  if (!job || job.status === "idle" || job.status === "cancelled") {
    resetTestUI();
    return;
  }
  syncTestJobFromServer(job);
  if (isTestJobActive(job)) startTestPolling();
}

async function stopAccountTest() {
  if (!state.currentKey) return;
  const stopBtn = $("#btnStopTest");
  if (stopBtn) stopBtn.disabled = true;
  state.userStoppedTest = true;
  stopTestPoll();
  resetTestUI();
  try {
    await api(`/api/keys/${encKey(state.currentKey)}/accounts/test/cancel`, {
      method: "POST",
      body: "{}",
    });
    toast("测号已中止");
  } catch (err) {
    toast(err.message || "中止请求失败", true);
  } finally {
    if (stopBtn) stopBtn.disabled = false;
    await loadAccounts();
    await loadKeys();
    updateDiscardFailedButton();
    updateMutedTestButton();
  }
}

function testJobDoneToast(job) {
  if (!job || job.status === "idle") return;
  if (job.status === "cancelled") {
    resetTestUI();
    toast("测号已中止");
    return;
  }
  if (job.status === "completed") {
    toast(`测号完成：成功 ${job.ok || 0}，失败 ${job.failed || 0}`);
  }
}

async function selectKey(apiKey) {
  stopTestPoll();
  state.currentKey = apiKey;
  state.page = 1;
  state.testStatus = {};
  state.lastFailedIds = [];
  state.testProgress = { done: 0, total: 0 };
  state.testProgressEverShown = false;
  state.testCancelled = false;
  state.userStoppedTest = false;
  renderKeys();
  $("#emptyState").classList.add("hidden");
  $("#poolView").classList.remove("hidden");
  setInspectorMode(true);
  const row = state.keys.find((k) => k.api_key === apiKey);
  $("#currentPoolTitle").textContent = poolDisplayTitle(row);
  $("#currentKeyMeta").textContent = row
    ? [row.enabled ? "启用" : "停用", `${row.pool_size} 可用`].join(" · ")
    : "";
  updateDiscardFailedButton();
  updateMutedTestButton();
  updateTestModeButtons();
  updateProgressUI();
  await loadAccounts();
  await restoreTestJob();
}

async function loadAccounts() {
  if (!state.currentKey) return;
  const include = state.tab === "discarded";
  const q = include ? "?include_discarded=1" : "";
  const data = await api(`/api/keys/${encKey(state.currentKey)}/accounts${q}`);
  state.accounts = data.accounts || [];
  renderAccounts();
}

function filterAccounts(list) {
  if (state.tab === "discarded") return list.filter((a) => a.discarded);
  return list.filter((a) => !a.discarded);
}

function rowActionsHTML(a) {
  const disabled = state.testing ? "disabled" : "";
  const testBtn = `<button type="button" class="btn sm icon-btn" title="测号" data-test="${escapeAttr(a.identifier)}" ${disabled}>${ICON_TEST}</button>`;
  const discardBtn = `<button type="button" class="btn sm icon-btn danger" title="作废" data-discard="${escapeAttr(a.identifier)}" ${disabled}>${ICON_DISCARD}</button>`;
  const restoreBtn = `<button type="button" class="btn sm icon-btn" title="恢复" data-restore="${escapeAttr(a.identifier)}" ${disabled}>${ICON_RESTORE}</button>`;
  if (!a.discarded) {
    return `${testBtn}${discardBtn}`;
  }
  if (a.discard_reason === "muted") {
    return `${testBtn}${restoreBtn}`;
  }
  return restoreBtn;
}

function renderAccounts() {
  const tbody = $("#accountBody");
  const filtered = filterAccounts(state.accounts);
  const meta = paginateRows(filtered);
  tbody.innerHTML = "";
  meta.pageRows.forEach((a) => {
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td>${a.position}</td>
      <td>${escapeHtml(a.identifier)}</td>
      <td>${a.has_password ? "●●●" : "—"}</td>
      <td class="status-cell">${accountStatusHTML(a)}</td>
      <td class="other-cell">${accountOtherHTML(a)}</td>
      <td class="col-actions"><div class="row-actions">${rowActionsHTML(a)}</div></td>
    `;
    tbody.appendChild(tr);
  });
  renderPagination(meta);
  const active = state.accounts.filter((a) => !a.discarded).length;
  const disc = state.accounts.filter((a) => a.discarded).length;
  const muted = state.accounts.filter((a) => a.discarded && a.discard_reason === "muted").length;
  const banned = state.accounts.filter((a) => a.discarded && a.discard_reason === "banned").length;
  let stats = `共 ${state.accounts.length} 条 · 可用 ${active} · 已作废 ${disc}`;
  if (muted || banned) stats += `（禁言 ${muted} · 封号 ${banned}）`;
  if (state.lastFailedIds.length) stats += ` · 最近失败 ${state.lastFailedIds.length}`;
  $("#accountStats").textContent = stats;
  updateDiscardFailedButton();
  updateMutedTestButton();
  updateTestModeButtons();
}

function escapeHtml(s) {
  return String(s)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function escapeAttr(s) {
  return escapeHtml(s).replace(/'/g, "&#39;");
}

async function startAccountTestJob(identifiers, activeOnly) {
  const body = { active_only: activeOnly };
  if (identifiers?.length) body.identifiers = identifiers;
  return api(`/api/keys/${encKey(state.currentKey)}/accounts/test`, {
    method: "POST",
    body: JSON.stringify(body),
  });
}

async function runBatchAccountTest(identifiers, activeOnly) {
  if (!state.currentKey) return;
  state.testCancelled = false;
  state.userStoppedTest = false;
  state.lastFailedIds = [];
  const pending = identifiers?.length
    ? identifiers
    : state.accounts.filter((a) => (activeOnly ? !a.discarded : true)).map((a) => a.identifier);
  pending.forEach((id) => {
    state.testStatus[id] = { status: "pending", message: "等待" };
  });
  state.testProgress = { done: 0, total: pending.length };
  state.testProgressEverShown = true;
  setTestingUI(true);
  updateProgressUI();
  renderAccounts();
  try {
    const job = await startAccountTestJob(identifiers, activeOnly);
    syncTestJobFromServer(job);
    if (isTestJobActive(job)) {
      await startTestPolling({ awaitDone: true });
      return;
    }
    await loadAccounts();
    await loadKeys();
    testJobDoneToast(job);
  } catch (err) {
    toast(err.message, true);
  } finally {
    setTestingUI(false);
    updateProgressUI();
    updateDiscardFailedButton();
    updateMutedTestButton();
    const stopBtn = $("#btnStopTest");
    if (stopBtn) stopBtn.disabled = false;
  }
}

$("#btnStopTest").addEventListener("click", () => stopAccountTest());

$("#btnTestAll").addEventListener("click", () => {
  const rows = state.accounts.filter((a) => !a.discarded);
  if (!rows.length) {
    toast("没有可测的可用账号", true);
    return;
  }
  runBatchAccountTest(rows.map((a) => a.identifier), true);
});

$("#btnTestMuted").addEventListener("click", () => {
  const rows = state.accounts.filter((a) => a.discarded && a.discard_reason === "muted");
  if (!rows.length) {
    toast("没有禁言账号可测", true);
    return;
  }
  runBatchAccountTest(rows.map((a) => a.identifier), false);
});

$("#btnDiscardFailed").addEventListener("click", async () => {
  if (!state.currentKey || state.testing) return;
  const ids = state.lastFailedIds.filter((id) => {
    const acc = state.accounts.find((a) => a.identifier === id);
    return acc && !acc.discarded;
  });
  if (!ids.length) {
    toast("没有可作废的失败账号", true);
    updateDiscardFailedButton();
    return;
  }
  if (!confirm(`确定作废 ${ids.length} 个测号失败的账号？`)) return;
  setTestingUI(true);
  let n = 0;
  try {
    for (const id of ids) {
      await api(
        `/api/keys/${encKey(state.currentKey)}/accounts/${encIdent(id)}/discard`,
        { method: "POST", body: "{}" }
      );
      delete state.testStatus[id];
      n++;
    }
    state.lastFailedIds = state.lastFailedIds.filter((id) => !ids.includes(id));
    await loadAccounts();
    await loadKeys();
    toast(`已作废 ${n} 个账号`);
  } catch (err) {
    toast(err.message, true);
  } finally {
    setTestingUI(false);
    updateDiscardFailedButton();
    updateMutedTestButton();
  }
});

$("#btnViewKey").addEventListener("click", () => {
  if (!state.currentKey) return;
  $("#viewKeyValue").textContent = state.currentKey;
  openDialog("#viewKeyDialog");
});

$("#btnDialogCopyKey").addEventListener("click", async () => {
  if (!state.currentKey) return;
  try {
    await navigator.clipboard.writeText(state.currentKey);
    toast("Key 已复制");
  } catch (_) {
    toast("复制失败", true);
  }
});

$("#accountBody").addEventListener("click", async (e) => {
  const testBtn = e.target.closest("[data-test]");
  const disc = e.target.closest("[data-discard]");
  const rest = e.target.closest("[data-restore]");
  if (!state.currentKey || state.testing) return;
  try {
    if (testBtn) {
      await runBatchAccountTest([testBtn.dataset.test], false);
      return;
    }
    if (disc) {
      await api(
        `/api/keys/${encKey(state.currentKey)}/accounts/${encIdent(disc.dataset.discard)}/discard`,
        { method: "POST", body: "{}" }
      );
      delete state.testStatus[disc.dataset.discard];
      state.lastFailedIds = state.lastFailedIds.filter((id) => id !== disc.dataset.discard);
      toast("已作废");
    }
    if (rest) {
      await api(
        `/api/keys/${encKey(state.currentKey)}/accounts/${encIdent(rest.dataset.restore)}/restore`,
        { method: "POST", body: "{}" }
      );
      delete state.testStatus[rest.dataset.restore];
      toast("已恢复");
    }
    await loadAccounts();
    await loadKeys();
    updateDiscardFailedButton();
    updateMutedTestButton();
  } catch (err) {
    toast(err.message, true);
  }
});

$$(".tab").forEach((btn) => {
  btn.addEventListener("click", async () => {
    if (state.testing) return;
    $$(".tab").forEach((b) => b.classList.remove("active"));
    btn.classList.add("active");
    state.tab = btn.dataset.tab;
    state.page = 1;
    updateTestModeButtons();
    await loadAccounts();
  });
});

$("#accountPagination").addEventListener("click", (e) => {
  const btn = e.target.closest("[data-page]");
  if (!btn || btn.disabled) return;
  const filtered = filterAccounts(state.accounts);
  const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  if (btn.dataset.page === "prev" && state.page > 1) state.page -= 1;
  if (btn.dataset.page === "next" && state.page < totalPages) state.page += 1;
  renderAccounts();
});

$("#btnRenamePool").addEventListener("click", () => {
  if (!state.currentKey) return;
  const row = state.keys.find((k) => k.api_key === state.currentKey);
  $("#renamePoolName").value = row?.name?.trim() || "";
  openDialog("#renamePoolDialog");
});

$("#renamePoolForm").addEventListener("submit", async (e) => {
  e.preventDefault();
  if (!state.currentKey) return;
  const name = $("#renamePoolName").value.trim();
  try {
    await api(`/api/keys/${encKey(state.currentKey)}`, {
      method: "PATCH",
      body: JSON.stringify({ name }),
    });
    closeDialogs();
    await loadKeys();
    const row = state.keys.find((k) => k.api_key === state.currentKey);
    $("#currentPoolTitle").textContent = poolDisplayTitle(row);
    toast("名称已更新");
  } catch (err) {
    toast(err.message, true);
  }
});

$("#btnImportCSV").addEventListener("click", () => {
  if (!state.currentKey) return;
  $("#csvFile").value = "";
  $("#csvFileName").textContent = "未选择文件";
  openDialog("#importCsvDialog");
});

$("#csvFile").addEventListener("change", (e) => {
  const file = e.target.files?.[0];
  $("#csvFileName").textContent = file ? file.name : "未选择文件";
});

$("#importCsvForm").addEventListener("submit", async (e) => {
  e.preventDefault();
  const file = $("#csvFile").files?.[0];
  if (!file || !state.currentKey) return;
  try {
    const text = await file.text();
    const res = await fetch(`/api/keys/${encKey(state.currentKey)}/import-csv`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${state.token}`,
        "Content-Type": "text/csv",
      },
      body: text,
    });
    const data = await res.json();
    if (!res.ok) throw new Error(data.detail || "导入失败");
    closeDialogs();
    const msg = `导入 ${data.imported} 条，跳过 ${data.skipped}`;
    toast(data.errors?.length ? `${msg}（${data.errors.length} 条错误）` : msg);
    state.page = 1;
    await loadAccounts();
    await loadKeys();
  } catch (err) {
    toast(err.message, true);
  }
});

$("#loginForm").addEventListener("submit", async (e) => {
  e.preventDefault();
  state.token = $("#loginToken").value.trim();
  try {
    await api("/api/keys");
    sessionStorage.setItem(TOKEN_KEY, state.token);
    showApp();
    await loadKeys();
    toast("登录成功");
  } catch (err) {
    state.token = "";
    toast(err.message || "登录失败", true);
  }
});

$("#btnLogout").addEventListener("click", () => {
  sessionStorage.removeItem(TOKEN_KEY);
  state.token = "";
  state.currentKey = null;
  showLogin();
});

$("#btnNewKey").addEventListener("click", () => {
  fillNewKeyInput();
  $("#newKeyName").value = "";
  openDialog("#newKeyDialog");
});

$("#btnRegenNewKey").addEventListener("click", fillNewKeyInput);
$("#btnRegenRotateKey").addEventListener("click", fillRotateKeyInput);

$("#newKeyForm").addEventListener("submit", async (e) => {
  e.preventDefault();
  try {
    const created = await api("/api/keys", {
      method: "POST",
      body: JSON.stringify({
        api_key: $("#newKeyValue").value.trim(),
        name: $("#newKeyName").value.trim(),
        remark: "",
      }),
    });
    closeDialogs();
    $("#newKeyForm").reset();
    await loadKeys();
    if (created.api_key) await selectKey(created.api_key);
    toast("已创建");
  } catch (err) {
    toast(err.message, true);
  }
});

$("#btnRotateKey").addEventListener("click", () => {
  if (!state.currentKey) return;
  fillRotateKeyInput();
  openDialog("#rotateDialog");
});

$("#rotateForm").addEventListener("submit", async (e) => {
  e.preventDefault();
  const oldKey = state.currentKey;
  const newKey = $("#rotateNew").value.trim();
  if (!oldKey) return;
  try {
    await api("/api/keys/rotate", {
      method: "POST",
      body: JSON.stringify({ old_api_key: oldKey, new_api_key: newKey }),
    });
    closeDialogs();
    state.currentKey = newKey;
    await loadKeys();
    await selectKey(newKey);
    toast("Key 已轮换");
  } catch (err) {
    toast(err.message, true);
  }
});

$("#btnDeletePool").addEventListener("click", async () => {
  if (!state.currentKey) return;
  const row = state.keys.find((k) => k.api_key === state.currentKey);
  const label = poolDisplayTitle(row);
  if (
    !confirm(
      `确定删除号池「${label}」？\n将删除该 Gateway Key 及其全部账号绑定，且不可恢复。`,
    )
  ) {
    return;
  }
  try {
    await api(`/api/keys/${encKey(state.currentKey)}`, { method: "DELETE" });
    state.currentKey = null;
    state.accounts = [];
    $("#poolView").classList.add("hidden");
    setInspectorMode(false);
    $("#emptyState").classList.remove("hidden");
    await loadKeys();
    toast("号池已删除");
  } catch (err) {
    toast(err.message, true);
  }
});

$("#btnExportCSV").addEventListener("click", async () => {
  if (!state.currentKey) return;
  try {
    const res = await fetch(`/api/keys/${encKey(state.currentKey)}/export-csv`, {
      headers: { Authorization: `Bearer ${state.token}` },
    });
    if (!res.ok) {
      let data = {};
      try {
        data = await res.json();
      } catch (_) {}
      throw new Error(data.detail || data.message || res.statusText);
    }
    const blob = await res.blob();
    const cd = res.headers.get("Content-Disposition") || "";
    const m = /filename="?([^";]+)"?/i.exec(cd);
    const date = new Date().toISOString().slice(0, 10);
    const prefix = state.currentKey.slice(0, 8).replace(/[^a-zA-Z0-9_-]/g, "_");
    const filename = m?.[1] || `pool-${prefix}-${date}.csv`;
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = filename;
    a.click();
    URL.revokeObjectURL(url);
    toast("CSV 已导出");
  } catch (err) {
    toast(err.message, true);
  }
});

$$("[data-close]").forEach((btn) => {
  btn.addEventListener("click", closeDialogs);
});

async function init() {
  setInspectorMode(false);
  if (state.token) {
    try {
      await api("/api/keys");
      showApp();
      await loadKeys();
      return;
    } catch (_) {
      state.token = "";
      sessionStorage.removeItem(TOKEN_KEY);
    }
  }
  showLogin();
}

init();
