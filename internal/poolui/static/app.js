const TOKEN_KEY = "pool_ui_token";

const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => [...document.querySelectorAll(sel)];

let state = {
  token: sessionStorage.getItem(TOKEN_KEY) || "",
  keys: [],
  currentKey: null,
  keyVisible: false,
  accounts: [],
  tab: "active",
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
  el.style.borderColor = isErr ? "var(--danger)" : "var(--border)";
  clearTimeout(toast._t);
  toast._t = setTimeout(() => el.classList.add("hidden"), 3500);
}

function showApp() {
  $("#app").classList.remove("hidden");
  $("#loginDialog").close?.();
}

function showLogin() {
  $("#app").classList.add("hidden");
  const d = $("#loginDialog");
  if (!d.open) d.showModal();
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
  if (row?.remark?.trim()) return row.remark.trim();
  return "未命名号池";
}

function setKeyVisible(visible) {
  state.keyVisible = visible;
  const block = $("#keyRevealBlock");
  const btn = $("#btnToggleKey");
  if (!block || !btn) return;
  block.classList.toggle("hidden", !visible);
  btn.textContent = visible ? "隐藏 Key" : "显示 Key";
}

function updateKeyRevealUI() {
  const valueEl = $("#currentKeyValue");
  if (valueEl) {
    valueEl.textContent = state.currentKey || "";
  }
  setKeyVisible(state.keyVisible);
}

function setTestingUI(testing) {
  state.testing = testing;
  const busy = [
    "#btnTestAll",
    "#btnTestMuted",
    "#btnDiscardFailed",
    "#btnRotateKey",
    "#btnDeletePool",
    "#btnExportCSV",
    "#btnToggleKey",
    "#btnCopyKey",
    "#csvFile",
  ];
  busy.forEach((sel) => {
    const el = $(sel);
    if (el) el.disabled = testing;
  });
  $$(".tab").forEach((b) => { b.disabled = testing; });
  const stopBtn = $("#btnStopTest");
  if (stopBtn) {
    stopBtn.classList.toggle("hidden", !testing);
    if (testing) stopBtn.disabled = false;
  }
}

function progressPercent(done, total, testing) {
  if (total <= 0) {
    return 0;
  }
  let value = done;
  if (testing && done < total) {
    // In-flight account counts as half a step so the bar moves before the first completes.
    value = done + 0.5;
  }
  let pct = Math.round((value / total) * 100);
  if (testing) {
    pct = Math.max(pct, 8);
    if (done < total) {
      pct = Math.min(pct, 99);
    }
  } else if (done >= total) {
    pct = 100;
  }
  return pct;
}

function updateProgressUI() {
  const wrap = $("#testProgressWrap");
  const fill = $("#testProgressFill");
  const label = $("#testProgressLabel");
  if (!wrap || !fill || !label) return;
  const { done, total } = state.testProgress;
  const shouldShow = state.testing || state.testProgressEverShown || total > 0;
  if (!shouldShow) {
    wrap.classList.add("hidden");
    fill.style.width = "0%";
    fill.classList.remove("is-active");
    return;
  }
  if (state.testing || total > 0) {
    state.testProgressEverShown = true;
  }
  wrap.classList.remove("hidden");
  const pct = progressPercent(done, total, state.testing);
  fill.style.width = `${pct}%`;
  fill.classList.toggle("is-active", state.testing);
  if (state.testing) {
    const current = total > 0 ? Math.min(done + 1, total) : 0;
    label.textContent = total > 0
      ? `测号中 ${current} / ${total}（${pct}%）`
      : "准备测号…";
  } else if (state.testCancelled && total > 0) {
    label.textContent = `测号已中止 ${done} / ${total}（已完成账号见下方状态列）`;
  } else if (total > 0) {
    label.textContent = `测号完成 ${done} / ${total}（成功/失败见下方状态列，不会自动清除）`;
  } else {
    label.textContent = "等待测号";
  }
}

function updateDiscardFailedButton() {
  const btn = $("#btnDiscardFailed");
  if (!btn) return;
  const count = state.lastFailedIds.filter((id) => {
    const acc = state.accounts.find((a) => a.identifier === id);
    return acc && !acc.discarded;
  }).length;
  if (count > 0 && !state.testing) {
    btn.classList.remove("hidden");
    btn.textContent = `作废失败（${count}）`;
  } else {
    btn.classList.add("hidden");
  }
}

function updateMutedTestButton() {
  const btn = $("#btnTestMuted");
  if (!btn) return;
  const muted = state.accounts.filter((a) => a.discarded && a.discard_reason === "muted");
  if (muted.length > 0 && !state.testing) {
    btn.classList.remove("hidden");
    btn.textContent = `测禁言号（${muted.length}）`;
  } else {
    btn.classList.add("hidden");
  }
}

function poolStatusHTML(a) {
  if (!a.discarded) {
    return '<span class="badge ok">可用</span>';
  }
  if (a.discard_reason === "muted") {
    return '<span class="badge off">禁言</span>';
  }
  if (a.discard_reason === "banned") {
    return '<span class="badge off">封号</span>';
  }
  const label = a.pool_status_text || "已作废";
  return `<span class="badge off">${escapeHtml(label)}</span>`;
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

function testStatusHTML(identifier) {
  const t = state.testStatus[identifier];
  if (!t) return '<span class="muted">—</span>';
  const msg = escapeHtml(t.message || "");
  switch (t.status) {
    case "pending":
      return `<span class="badge warn">等待</span><span class="test-msg muted">${msg}</span>`;
    case "testing":
      return `<span class="badge testing">测号中</span>`;
    case "ok": {
      const tokenNote = t.token_updated ? '<span class="test-msg muted"> · token已更新</span>' : "";
      return `<span class="badge ok">可用</span>${tokenNote}`;
    }
    case "skip":
      return `<span class="badge warn">跳过</span><span class="test-msg muted" title="${msg}">${msg}</span>`;
    case "fail": {
      const tag =
        t.pool_status === "muted"
          ? "禁言"
          : t.pool_status === "banned"
            ? "封号"
            : t.pool_status === "transport"
              ? "网络异常"
              : "失败";
      const auto = t.auto_discarded ? " · 已自动作废" : "";
      return `<span class="badge off">${tag}</span><span class="test-msg fail-msg" title="${msg}">${msg}${auto}</span>`;
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
              if (job.status === "cancelled" || state.userStoppedTest) {
                resetTestUI();
              }
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
        if (job.status === "cancelled" || state.userStoppedTest) {
          resetTestUI();
        }
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
  if (!job || job.status === "idle") {
    return;
  }
  state.testProgress = { done: job.done || 0, total: job.total || 0 };
  if (job.total > 0 || job.status === "running") {
    state.testProgressEverShown = true;
  }
  state.testCancelled = job.status === "cancelled";
  const running = job.status === "running" && !state.userStoppedTest;
  if (job.status === "cancelled" || job.status === "completed") {
    state.userStoppedTest = false;
  }
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
  if (isTestJobActive(job)) {
    startTestPolling();
  }
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
  state.keyVisible = false;
  state.testStatus = {};
  state.lastFailedIds = [];
  state.testProgress = { done: 0, total: 0 };
  state.testProgressEverShown = false;
  state.testCancelled = false;
  state.userStoppedTest = false;
  renderKeys();
  $("#emptyState").classList.add("hidden");
  $("#poolView").classList.remove("hidden");
  const row = state.keys.find((k) => k.api_key === apiKey);
  $("#currentPoolTitle").textContent = poolDisplayTitle(row);
  $("#currentKeyMeta").textContent = row
    ? [row.remark, row.enabled ? "启用" : "停用", `${row.pool_size} 可用`].filter(Boolean).join(" · ")
    : "";
  updateKeyRevealUI();
  updateDiscardFailedButton();
  updateMutedTestButton();
  updateProgressUI();
  await loadAccounts();
  await restoreTestJob();
}

async function loadAccounts() {
  if (!state.currentKey) return;
  const include = state.tab !== "active";
  const q = include ? "?include_discarded=1" : "";
  const data = await api(`/api/keys/${encKey(state.currentKey)}/accounts${q}`);
  state.accounts = data.accounts || [];
  renderAccounts();
}

function filterAccounts(list) {
  if (state.tab === "discarded") return list.filter((a) => a.discarded);
  if (state.tab === "active") return list.filter((a) => !a.discarded);
  return list;
}

function rowActionsHTML(a) {
  const disabled = state.testing ? "disabled" : "";
  if (!a.discarded) {
    return `<button type="button" class="btn sm" data-test="${escapeAttr(a.identifier)}" ${disabled}>测号</button>
         <button type="button" class="btn sm danger" data-discard="${escapeAttr(a.identifier)}" ${disabled}>作废</button>`;
  }
  if (a.discard_reason === "muted") {
    return `<button type="button" class="btn sm" data-test="${escapeAttr(a.identifier)}" ${disabled}>测号</button>
         <button type="button" class="btn sm" data-restore="${escapeAttr(a.identifier)}" ${disabled}>恢复</button>`;
  }
  return `<button type="button" class="btn sm" data-restore="${escapeAttr(a.identifier)}" ${disabled}>恢复</button>`;
}

function renderAccounts() {
  const tbody = $("#accountBody");
  const rows = filterAccounts(state.accounts);
  tbody.innerHTML = "";
  rows.forEach((a) => {
    const tr = document.createElement("tr");
    if (state.testStatus[a.identifier]?.status === "fail") {
      tr.classList.add("row-fail");
    } else if (state.testStatus[a.identifier]?.status === "ok") {
      tr.classList.add("row-ok");
    }
    tr.innerHTML = `
      <td>${a.position}</td>
      <td>${escapeHtml(a.identifier)}</td>
      <td>${a.has_password ? "●●●" : "—"}</td>
      <td>${escapeHtml(a.token_preview || (a.has_token ? "有" : "—"))}</td>
      <td>${poolStatusHTML(a)}</td>
      <td class="test-cell">${testStatusHTML(a.identifier)}</td>
      <td class="row-actions">${rowActionsHTML(a)}</td>
    `;
    tbody.appendChild(tr);
  });
  const active = state.accounts.filter((a) => !a.discarded).length;
  const disc = state.accounts.filter((a) => a.discarded).length;
  const muted = state.accounts.filter((a) => a.discarded && a.discard_reason === "muted").length;
  const banned = state.accounts.filter((a) => a.discarded && a.discard_reason === "banned").length;
  let stats = `共 ${state.accounts.length} 条 · 可用 ${active} · 已作废 ${disc}`;
  if (muted || banned) {
    stats += `（禁言 ${muted} · 封号 ${banned}）`;
  }
  if (state.lastFailedIds.length) {
    stats += ` · 最近失败 ${state.lastFailedIds.length}`;
  }
  $("#accountStats").textContent = stats;
  updateDiscardFailedButton();
  updateMutedTestButton();
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

$("#btnStopTest").addEventListener("click", () => {
  stopAccountTest();
});

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
  if (!confirm(`对 ${rows.length} 个禁言账号测号？通过将自动恢复为可用。`)) return;
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

$("#btnToggleKey").addEventListener("click", () => {
  if (!state.currentKey) return;
  setKeyVisible(!state.keyVisible);
});

$("#btnCopyKey").addEventListener("click", async () => {
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
    await loadAccounts();
  });
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
  $("#newKeyRemark").value = "";
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
        remark: $("#newKeyRemark").value.trim(),
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
  $("#rotateOld").value = state.currentKey;
  fillRotateKeyInput();
  openDialog("#rotateDialog");
});

$("#rotateForm").addEventListener("submit", async (e) => {
  e.preventDefault();
  const oldKey = $("#rotateOld").value.trim();
  const newKey = $("#rotateNew").value.trim();
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

$("#csvFile").addEventListener("change", async (e) => {
  const file = e.target.files?.[0];
  e.target.value = "";
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
    const msg = `导入 ${data.imported} 条，跳过 ${data.skipped}`;
    toast(data.errors?.length ? `${msg}（${data.errors.length} 条错误）` : msg);
    await loadAccounts();
    await loadKeys();
  } catch (err) {
    toast(err.message, true);
  }
});

$$("[data-close]").forEach((btn) => {
  btn.addEventListener("click", closeDialogs);
});

async function init() {
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
