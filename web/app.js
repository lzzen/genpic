const $ = id => document.getElementById(id);

window.__genpicDefaultBaseUrl = '';

const CRED_PACK_KEY = 'genpic:cred-v1';

function safeDecodeURIComponent(s) {
  try { return decodeURIComponent(s); } catch { return s; }
}

function looksLikeMaskedKey(key) {
  const k = (key || '').trim();
  if (!k) return false;
  if (/\*{3,}/.test(k)) return true;
  if (k.includes('****')) return true;
  if (/^sk-/i.test(k) && k.includes('*')) return true;
  return false;
}

function setCredWarn(msg) {
  const el = $('cred-warn');
  if (!el) return;
  if (msg) {
    el.textContent = msg;
    el.classList.add('show');
  } else {
    el.textContent = '';
    el.classList.remove('show');
  }
}

function updateCredWarnUI() {
  const inp = $('api-key');
  if (!inp) return;
  if (looksLikeMaskedKey(inp.value)) {
    setCredWarn('检测到脱敏或占位符（如 sk****…）。NewAPI 传入的密钥常不完整，请粘贴完整密钥后再生成。');
  } else {
    setCredWarn('');
  }
}

function openCredDialog() {
  const p = $('cred-panel');
  if (p) p.hidden = false;
  updateCredWarnUI();
  requestAnimationFrame(() => $('base-url')?.focus());
}

function closeCredDialog() {
  const p = $('cred-panel');
  if (p) p.hidden = true;
}

/** 首次进入或凭证不完整 / 脱敏时自动弹出配置。 */
function maybeAutoOpenCredDialog() {
  const base = effectiveBaseURL();
  const key = ($('api-key')?.value || '').trim();
  if (looksLikeMaskedKey(key) || !base || !key) openCredDialog();
}

let _derivedKeyPromise;
function getDerivedKey() {
  if (!window.crypto?.subtle) return Promise.reject(new Error('no subtle'));
  if (!_derivedKeyPromise) {
    const enc = new TextEncoder();
    const pepper = enc.encode('genpic-mvp-lite-cred-v1');
    _derivedKeyPromise = crypto.subtle.importKey('raw', pepper, 'PBKDF2', false, ['deriveBits', 'deriveKey']).then(
      keyMaterial => crypto.subtle.deriveKey(
        { name: 'PBKDF2', salt: enc.encode('genpic-local-salt-v1'), iterations: 120000, hash: 'SHA-256' },
        keyMaterial,
        { name: 'AES-GCM', length: 256 },
        false,
        ['encrypt', 'decrypt']
      )
    );
  }
  return _derivedKeyPromise;
}

async function saveCredentialsEncrypted(address, apiKey) {
  const payload = JSON.stringify({ address, apiKey });
  if (!window.crypto?.subtle) {
    try {
      localStorage.setItem('genpic:cred-fallback', btoa(encodeURIComponent(payload)));
    } catch {}
    return;
  }
  try {
    const key = await getDerivedKey();
    const iv = crypto.getRandomValues(new Uint8Array(12));
    const pt = new TextEncoder().encode(payload);
    const ct = await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, key, pt);
    const pack = { v: 1, iv: Array.from(iv), ct: Array.from(new Uint8Array(ct)) };
    localStorage.setItem(CRED_PACK_KEY, JSON.stringify(pack));
    try { localStorage.removeItem('genpic:cred-fallback'); } catch {}
  } catch (e) {
    console.warn('encrypt credentials failed', e);
  }
}

async function loadCredentialsEncrypted() {
  try {
    const fb = localStorage.getItem('genpic:cred-fallback');
    if (fb) {
      return JSON.parse(decodeURIComponent(atob(fb)));
    }
    const raw = localStorage.getItem(CRED_PACK_KEY);
    if (!raw) return null;
    const pack = JSON.parse(raw);
    if (pack.v !== 1 || !pack.iv || !pack.ct) return null;
    if (!window.crypto?.subtle) return null;
    const key = await getDerivedKey();
    const iv = new Uint8Array(pack.iv);
    const ct = new Uint8Array(pack.ct);
    const pt = await crypto.subtle.decrypt({ name: 'AES-GCM', iv }, key, ct);
    return JSON.parse(new TextDecoder().decode(pt));
  } catch {
    return null;
  }
}

let _saveCredTimer;
function scheduleSaveCredentials() {
  clearTimeout(_saveCredTimer);
  _saveCredTimer = setTimeout(() => {
    saveCredentialsEncrypted($('base-url').value.trim(), $('api-key').value.trim());
  }, 450);
}

function effectiveBaseURL() {
  const u = $('base-url').value.trim();
  if (u) return u;
  return (window.__genpicDefaultBaseUrl || '').trim();
}

// Stable anonymous identity for server-side generation history (see X-Genpic-Session).
const GENPIC_SESSION_KEY = 'genpic:browser-session';

function getOrCreateGenpicSessionId() {
  try {
    let s = localStorage.getItem(GENPIC_SESSION_KEY);
    if (s && typeof s === 'string') {
      s = s.trim();
      if (s.length > 0 && s.length <= 128) return s;
    }
    s = (crypto.randomUUID && crypto.randomUUID()) ||
      'sess_' + Math.random().toString(36).slice(2) + Date.now().toString(36);
    localStorage.setItem(GENPIC_SESSION_KEY, s);
    return s;
  } catch {
    return 'sess_anon_fallback';
  }
}

function genpicEscape(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

async function genpicFetch(url, opts = {}) {
  const next = { credentials: 'same-origin', ...opts };
  next.headers = { 'X-Genpic-Session': getOrCreateGenpicSessionId(), ...(opts.headers || {}) };
  return fetch(url, next);
}

let authUser = null;

async function refreshAuthUser() {
  try {
    const r = await genpicFetch('/api/auth/me');
    if (r.ok) authUser = await r.json();
    else authUser = null;
  } catch {
    authUser = null;
  }
  updateAuthChrome();
}

function updateAuthChrome() {
  const btn = $('btn-auth');
  const em = $('auth-dropdown-email');
  if (!btn) return;
  if (authUser && authUser.email) {
    const nick = (authUser.display_name || '').trim();
    btn.textContent = nick ? nick.slice(0, 12) : authUser.email.split('@')[0].slice(0, 12);
  } else {
    btn.textContent = '登录';
  }
  if (em) em.textContent = authUser && authUser.email ? authUser.email : '';
}

function closeAuthDropdown() {
  const dd = $('auth-dropdown');
  if (dd) dd.hidden = true;
}

const taskQueue = new Map();
let taskQueueCollapsed = false;
const taskQueueTimers = new Map();

function fmtTaskElapsed(ms) {
  const s = Math.floor(ms / 1000);
  if (s < 60) return s + ' 秒';
  return Math.floor(s / 60) + ' 分 ' + (s % 60) + ' 秒';
}

function renderTaskQueue() {
  const bar = $('task-queue-bar');
  if (!bar) return;
  if (taskQueue.size === 0) {
    bar.hidden = true;
    bar.innerHTML = '';
    return;
  }
  bar.hidden = false;
  const items = [...taskQueue.entries()];
  let html = '<div class="task-queue-head"><span>进行中任务（' + items.length + '）</span>';
  html += '<button type="button" class="task-queue-toggle" id="task-queue-collapse">' + (taskQueueCollapsed ? '展开' : '收起') + '</button></div>';
  html += '<div id="task-queue-list" class="task-queue-list' + (taskQueueCollapsed ? ' collapsed' : '') + '">';
  for (const [id, t] of items) {
    const st = t.status || 'queued';
    const cls = st === 'failed' ? 'failed' : (st === 'succeeded' ? 'succeeded' : 'running');
    const left = (st === 'queued' || st === 'running')
      ? '<div class="task-spin" aria-hidden="true"></div>'
      : '<span class="task-pill-icon">' + (st === 'succeeded' ? '✓' : '✕') + '</span>';
    const pr = t.prompt || '';
    const promptShort = pr.slice(0, 34) + (pr.length > 34 ? '…' : '');
    html += '<div class="task-pill ' + cls + '">' + left + '<div class="task-pill-meta">';
    html += '<div class="task-pill-model">' + genpicEscape(t.model || '') + '</div>';
    html += '<div class="task-pill-prompt">' + genpicEscape(promptShort) + '</div>';
    html += '<div class="task-pill-time">' + fmtTaskElapsed(Date.now() - (t.startedAt || Date.now())) + ' · ' + genpicEscape(st) + '</div>';
    html += '</div></div>';
  }
  html += '</div>';
  bar.innerHTML = html;
  $('task-queue-collapse')?.addEventListener('click', () => {
    taskQueueCollapsed = !taskQueueCollapsed;
    renderTaskQueue();
  });
}

function scheduleRemoveTask(jobId, ms) {
  if (taskQueueTimers.has(jobId)) clearTimeout(taskQueueTimers.get(jobId));
  taskQueueTimers.set(jobId, setTimeout(() => {
    taskQueue.delete(jobId);
    taskQueueTimers.delete(jobId);
    renderTaskQueue();
  }, ms));
}

async function pollJobUntilDone(jobId, meta) {
  const terminal = { succeeded: true, failed: true };
  for (;;) {
    const jr = await genpicFetch('/jobs/' + encodeURIComponent(jobId));
    const raw = await jr.text();
    let j = {};
    if (raw) { try { j = JSON.parse(raw); } catch (_) {} }
    if (!jr.ok) {
      meta.status = 'failed';
      meta.errorMessage = j?.error?.message ?? ('HTTP ' + jr.status);
      taskQueue.set(jobId, { ...meta });
      renderTaskQueue();
      scheduleRemoveTask(jobId, 4000);
      return;
    }
    meta.status = j.status || meta.status;
    taskQueue.set(jobId, { ...meta });
    renderTaskQueue();
    if (terminal[j.status]) {
      if (j.status === 'succeeded') {
        const images = j.data || [];
        const prompt = meta.prompt || '';
        if (images.length) {
          addImages(images, meta.model || effectiveModelId(), meta.provider || activeProvider);
          saveToHistory(images, meta.model || effectiveModelId(), meta.provider || activeProvider, prompt, jobId);
        }
      }
      void mergeAndPersistHistory({ reset: true }).catch(() => {});
      renderHistoryPanel();
      scheduleRemoveTask(jobId, 3500);
      return;
    }
    await new Promise((r) => setTimeout(r, 1000));
  }
}

let communityNextCursor = null;
let communityLoading = false;

async function loadCommunityFeed(reset) {
  const feed = $('community-feed');
  if (!feed) return;
  if (!reset && !communityNextCursor) return;
  if (reset) {
    communityNextCursor = null;
    feed.innerHTML = '';
  }
  if (communityLoading) return;
  communityLoading = true;
  try {
    const params = new URLSearchParams({ limit: '12' });
    if (communityNextCursor) params.set('cursor', communityNextCursor);
    const r = await genpicFetch('/api/community/feed?' + params.toString());
    if (!r.ok) {
      feed.innerHTML = '<div class="note">暂时无法加载社区（请确认已配置数据库）</div>';
      return;
    }
    const data = await r.json();
    const rows = data.data || [];
    communityNextCursor = (data.next_cursor === undefined || data.next_cursor === null || data.next_cursor === '')
      ? null
      : String(data.next_cursor);
    if (reset && rows.length === 0) {
      feed.innerHTML = '<div class="note">还没有公开作品</div>';
      return;
    }
    for (const job of rows) {
      feed.appendChild(renderCommunityCard(job));
    }
  } catch {
    if (reset) feed.innerHTML = '<div class="note">网络错误</div>';
  } finally {
    communityLoading = false;
  }
}

function renderCommunityCard(job) {
  const card = document.createElement('div');
  card.className = 'comm-card';
  const img0 = (job.data || [])[0];
  if (img0 && img0.url) {
    const im = document.createElement('img');
    im.className = 'comm-card-img';
    im.alt = '';
    im.loading = 'lazy';
    im.src = img0.url;
    card.appendChild(im);
  }
  const body = document.createElement('div');
  body.className = 'comm-card-body';
  const meta = document.createElement('div');
  meta.className = 'comm-card-meta';
  meta.innerHTML = '<span>' + genpicEscape(job.model || '') + '</span>';
  body.appendChild(meta);
  const pr = document.createElement('div');
  pr.className = 'comm-card-prompt';
  pr.textContent = job.prompt ? job.prompt : '（提示词仅对部分访客可见）';
  body.appendChild(pr);
  const act = document.createElement('div');
  act.className = 'comm-card-actions';
  const b = document.createElement('button');
  b.type = 'button';
  b.textContent = '创作同款';
  b.addEventListener('click', () => applyCreateSimilar(job));
  act.appendChild(b);
  body.appendChild(act);
  card.appendChild(body);
  return card;
}

function applyCreateSimilar(job) {
  if (!authUser) {
    alert('登录后可查看完整提示词并创作同款');
    openAuthModal('login');
    return;
  }
  const p = job.params || {};
  const modelId = p.model || job.model || '';
  const prov = modelToProvider(modelId || 'openai/gpt-image-2');
  if (activeVendorId === 'community') {
    selectVendor(prov);
  }
  if (modelId) {
    $('model-select').value = modelId;
    setActiveModel(modelId, prov);
  }
  if (p.n) {
    const gn = $('gpt-n'), gemn = $('gem-n'), wann = $('wan-n');
    if (gn && activeProvider === 'openai') gn.value = String(p.n);
    if (gemn && activeProvider === 'gemini') gemn.value = String(p.n);
    if (wann && activeProvider === 'wan') wann.value = String(p.n);
  }
  if (p.aspect_ratio && activeProvider === 'gemini') {
    document.querySelectorAll('#gem-ratio-grid .ratio-btn').forEach((btn) => {
      btn.classList.toggle('active', btn.dataset.aspect === p.aspect_ratio);
    });
  }
  if (p.image_size && $('gem-image-size')) $('gem-image-size').value = p.image_size;
  if (p.size && $('gpt-size')) $('gpt-size').value = p.size;
  $('prompt').value = job.prompt || '';
  $('prompt').scrollIntoView({ behavior: 'smooth', block: 'center' });
  $('prompt').focus();
}

async function bootstrapCredentials() {
  let defaultBase = '';
  try {
    const r = await fetch('/api/public-config');
    if (r.ok) {
      const j = await r.json();
      defaultBase = (j.default_base_url || '').trim();
    }
  } catch (_) {}
  window.__genpicDefaultBaseUrl = defaultBase;

  let address = '';
  let apiKey = '';
  const fromStore = await loadCredentialsEncrypted();
  if (fromStore && typeof fromStore === 'object') {
    address = (fromStore.address || '').trim();
    apiKey = (fromStore.apiKey || '').trim();
  }

  const legacyBase = load('base-url', null);
  if (legacyBase) {
    if (!address) address = legacyBase.trim();
    try { localStorage.removeItem('genpic:base-url'); } catch {}
  }

  const params = new URLSearchParams(location.search);
  const qAddr = (params.get('address') || params.get('Address') || '').trim();
  const qKey = (params.get('key') || params.get('Key') || '').trim();
  if (qAddr) address = safeDecodeURIComponent(qAddr);
  if (qKey) apiKey = safeDecodeURIComponent(qKey);

  const finalAddr = (address || defaultBase).trim();
  $('base-url').value = finalAddr;
  $('api-key').value = apiKey;
  updateCredWarnUI();

  if (qAddr || qKey) {
    openCredDialog();
    try { history.replaceState(null, '', location.pathname + location.hash); } catch (_) {}
  }

  await saveCredentialsEncrypted($('base-url').value.trim(), $('api-key').value.trim());

  $('base-url').addEventListener('input', () => { updateCredWarnUI(); scheduleSaveCredentials(); });
  $('base-url').addEventListener('change', () => { updateCredWarnUI(); scheduleSaveCredentials(); });
  $('api-key').addEventListener('input', () => { updateCredWarnUI(); scheduleSaveCredentials(); });
  $('api-key').addEventListener('change', () => { updateCredWarnUI(); scheduleSaveCredentials(); });

  maybeAutoOpenCredDialog();
}

// ── Active state ────────────────────────────────────────
let activeModel = 'openai/gpt-image-2';
let activeProvider = 'openai';
let activeVendorId = 'openai';

/** Reference images for image-to-image (max 6). */
let referenceEntries = [];

function setActiveModel(modelId, provider) {
  activeModel = modelId;
  activeProvider = provider;

  const ms = $('model-select');
  if (ms && ms.value !== modelId) ms.value = modelId;

  activeVendorId = provider;
  document.querySelectorAll('#vendor-rail .vendor-btn').forEach((b) => {
    b.classList.toggle('active', b.dataset.vendor === provider);
  });

  ['openai', 'gemini', 'wan'].forEach((p) => {
    $('params-' + p)?.classList.toggle('show', p === provider);
    $('more-extra-' + p)?.classList.toggle('show', p === provider);
  });

  // Show thinking field only for thinking-capable Gemini models
  const thinkingCapable = ['gemini/gemini-3.1-flash-image-preview', 'gemini/gemini-3-pro-image-preview'];
  const gemThinking = $('gem-thinking-field');
  if (gemThinking) gemThinking.style.display = thinkingCapable.includes(modelId) ? 'block' : 'none';

  // Show wan thinking mode only for pro
  const wanThinking = $('wan-thinking-row');
  if (wanThinking) wanThinking.style.display = modelId === 'wan/wan2.7-image-pro' ? 'flex' : 'none';

  // Update button color via data attribute
  $('app').dataset.activeProvider = provider;

  save('active-model', modelId);
  save('active-provider', provider);
  save('active-vendor', provider);

  if (provider === 'gemini') syncGemImageSizeUI(modelId);
}

/** Gemini imageSize options depend on catalog model (see model constraints). */
function syncGemImageSizeUI(modelId) {
  const wrap = $('gem-image-size-wrap');
  const sel = $('gem-image-size');
  const label = $('gem-image-size-label');
  if (!wrap || !sel) return;

  const prev = sel.value;
  sel.innerHTML = '';

  if (modelId === 'gemini/gemini-2.5-flash-image') {
    wrap.style.display = 'none';
    return;
  }

  wrap.style.display = '';
  if (label) label.textContent = '分辨率档位';
  let rows;
  let defVal;
  if (modelId === 'gemini/gemini-3-pro-image-preview') {
    rows = [['1K', '1K'], ['2K', '2K'], ['4K', '4K']];
    defVal = '1K';
  } else {
    rows = [['512', '512'], ['1K', '1K'], ['2K', '2K'], ['4K', '4K']];
    defVal = '1K';
  }
  rows.forEach(([v, t]) => {
    const o = document.createElement('option');
    o.value = v;
    o.textContent = t;
    sel.appendChild(o);
  });
  const allowed = rows.map((r) => r[0]);
  sel.value = allowed.includes(prev) ? prev : defVal;
}

// ── Persist helpers ─────────────────────────────────────
function save(k, v) { try { localStorage.setItem('genpic:' + k, v); } catch {} }
function load(k, def) { try { return localStorage.getItem('genpic:' + k) ?? def; } catch { return def; } }

let uiCatalog = null;

const FALLBACK_UI_CATALOG = {
  vendors: [
    { id: 'openai', name: 'GPT image', models: [{ id: 'openai/gpt-image-2', label: 'GPT Image 2' }] },
    {
      id: 'gemini',
      name: 'Banana',
      models: [
        { id: 'gemini/gemini-3.1-flash-image-preview', label: 'Gemini 3.1' },
        { id: 'gemini/gemini-3-pro-image-preview', label: 'Gemini 3 Pro' },
        { id: 'gemini/gemini-2.5-flash-image', label: 'Gemini 2.5 Flash' },
      ],
    },
    {
      id: 'wan',
      name: '万相生图',
      models: [
        { id: 'wan/wan2.7-image', label: '万相 2.7' },
        { id: 'wan/wan2.7-image-pro', label: '万相 2.7 Pro' },
      ],
    },
  ],
};

async function loadUICatalog() {
  try {
    const r = await fetch('/api/ui/catalog');
    if (r.ok) {
      uiCatalog = await r.json();
      return;
    }
  } catch (_) {}
  uiCatalog = FALLBACK_UI_CATALOG;
}

function applyVendorRail() {
  const rail = $('vendor-rail');
  if (!rail) return;
  rail.innerHTML = '';
  (uiCatalog?.vendors || []).forEach((v) => {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'vendor-btn';
    b.dataset.vendor = v.id;
    b.textContent = v.name || v.id;
    b.addEventListener('click', () => selectVendor(v.id));
    rail.appendChild(b);
  });
  const c = document.createElement('button');
  c.type = 'button';
  c.className = 'vendor-btn';
  c.dataset.vendor = 'community';
  c.textContent = '社区';
  c.title = '公开作品广场';
  c.addEventListener('click', () => selectVendor('community'));
  rail.appendChild(c);
}

function getModelsForVendor(vid) {
  const v = (uiCatalog?.vendors || []).find((x) => x.id === vid);
  return Array.isArray(v?.models) ? v.models : [];
}

function selectVendor(vid) {
  const sidebar = $('sidebar');
  const genArea = $('generate-area');
  if (vid === 'community') {
    activeVendorId = 'community';
    save('active-vendor', 'community');
    document.querySelectorAll('#vendor-rail .vendor-btn').forEach((b) => {
      b.classList.toggle('active', b.dataset.vendor === 'community');
    });
    if (sidebar) sidebar.hidden = true;
    if (genArea) genArea.dataset.view = 'community';
    void loadCommunityFeed(true);
    return;
  }
  if (sidebar) sidebar.hidden = false;
  if (genArea) genArea.dataset.view = 'generate';

  activeVendorId = vid;
  save('active-vendor', vid);
  document.querySelectorAll('#vendor-rail .vendor-btn').forEach((b) => {
    b.classList.toggle('active', b.dataset.vendor === vid);
  });
  const sel = $('model-select');
  if (!sel) return;
  const prev = sel.value;
  const models = getModelsForVendor(vid);
  sel.innerHTML = '';
  models.forEach((m) => {
    const o = document.createElement('option');
    o.value = m.id;
    o.textContent = m.label || m.id;
    sel.appendChild(o);
  });
  let next = models[0]?.id || '';
  if (models.some((m) => m.id === prev)) next = prev;
  if (next) {
    sel.value = next;
    setActiveModel(next, modelToProvider(next));
  }
}

function modelToProvider(modelId) {
  const m = String(modelId || '').trim();
  if (m.startsWith('openai/')) return 'openai';
  if (m.startsWith('gemini/')) return 'gemini';
  if (m.startsWith('wan/')) return 'wan';
  return 'openai';
}

async function initCatalogAndModels() {
  await loadUICatalog();
  if (!uiCatalog || !Array.isArray(uiCatalog.vendors) || uiCatalog.vendors.length === 0) {
    uiCatalog = FALLBACK_UI_CATALOG;
  }
  applyVendorRail();
  const vids = uiCatalog.vendors.map((v) => v.id);
  let vend = load('active-vendor', null);
  if (vend === 'community') {
    selectVendor('community');
    return;
  }
  if (!vend || !vids.includes(vend)) vend = uiCatalog.vendors[0]?.id || 'openai';
  selectVendor(vend);
  const sm = load('active-model', null);
  if (sm && getModelsForVendor(vend).some((m) => m.id === sm)) {
    $('model-select').value = sm;
    setActiveModel(sm, modelToProvider(sm));
  }
}

// Restore persisted values
const persistFields = [
  'prompt',
  'gpt-size', 'gpt-quality', 'gpt-n', 'gpt-style', 'gpt-format',
  'gpt-cw', 'gpt-ch',
  'gem-n', 'wan-n'
];
persistFields.forEach(id => {
  const el = $(id);
  if (!el) return;
  const v = load(id, null);
  if (v !== null) el.value = v;
  el.addEventListener('change', () => save(id, el.value));
  if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') {
    el.addEventListener('input', () => save(id, el.value));
  }
});

getOrCreateGenpicSessionId();

bootstrapCredentials()
  .then(() => initCatalogAndModels())
  .then(() => {
    const gisz = $('gem-image-size');
    if (gisz && gisz.options.length) {
      const v = load('gem-image-size', null);
      if (v !== null && [...gisz.options].some((o) => o.value === v)) gisz.value = v;
      gisz.addEventListener('change', () => save('gem-image-size', gisz.value));
    }
    wireRefAndGpt();
    const sz = ($('gpt-size')?.value || '').trim();
    const grid = $('gpt-ratio-grid');
    if (grid) {
      let matched = null;
      grid.querySelectorAll('.ratio-btn').forEach((b) => {
        if ((b.dataset.size || '').trim() === sz) matched = b;
      });
      if (matched) {
        grid.querySelectorAll('.ratio-btn').forEach((b) => b.classList.remove('active'));
        matched.classList.add('active');
      }
    }
    updateGptResEst();
  })
  .then(() => refreshAuthUser())
  .then(() => mergeAndPersistHistory({ reset: true }))
  .then(() => renderHistoryPanel())
  .catch((e) => console.warn('bootstrap', e));

$('model-select')?.addEventListener('change', () => {
  const id = $('model-select').value;
  setActiveModel(id, modelToProvider(id));
  save('active-model', id);
});

// ── Event handlers ──────────────────────────────────────
$('btn-cred-dialog')?.addEventListener('click', (e) => {
  e.stopPropagation();
  const p = $('cred-panel');
  if (p && !p.hidden) { closeCredDialog(); } else { openCredDialog(); }
});
$('cred-panel-close')?.addEventListener('click', () => closeCredDialog());

// Close cred panel on click outside
document.addEventListener('click', (e) => {
  const p = $('cred-panel');
  if (p && !p.hidden && !p.contains(e.target) && e.target.id !== 'btn-cred-dialog') {
    closeCredDialog();
  }
});
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') closeCredDialog();
});

// ── More-settings toggle ─────────────────────────────────
$('more-settings-toggle')?.addEventListener('click', () => {
  const body = $('more-settings-body');
  const btn  = $('more-settings-toggle');
  if (!body || !btn) return;
  const open = !body.hidden;
  body.hidden = open;
  btn.setAttribute('aria-expanded', String(!open));
});

function openHistDrawer() {
  const back = $('hist-drawer-backdrop');
  const dr = $('hist-drawer');
  if (back) back.hidden = false;
  if (dr) dr.hidden = false;
  mergeAndPersistHistory({ reset: true })
    .then(() => renderHistoryPanel())
    .catch(() => renderHistoryPanel());
  requestAnimationFrame(() => {
    $('hist-drawer-close')?.focus({ preventScroll: true });
  });
}

function closeHistDrawer() {
  const back = $('hist-drawer-backdrop');
  const dr = $('hist-drawer');
  if (back) back.hidden = true;
  if (dr) dr.hidden = true;
}

$('btn-hist-drawer')?.addEventListener('click', () => openHistDrawer());
$('hist-drawer-close')?.addEventListener('click', () => closeHistDrawer());
$('hist-drawer-backdrop')?.addEventListener('click', () => closeHistDrawer());

document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && !$('hist-drawer')?.hidden) closeHistDrawer();
});

$('eye-btn')?.addEventListener('click', () => {
  const inp = $('api-key');
  inp.type = inp.type === 'password' ? 'text' : 'password';
});

// Thinking budget display
const thinkSlider = $('gem-thinking');
if (thinkSlider) {
  thinkSlider.addEventListener('input', () => {
    const v = parseInt(thinkSlider.value);
    $('gem-thinking-display').textContent = v === 0 ? '关闭' : v.toString();
  });
}

// ── Status helpers ──────────────────────────────────────
function setStatus(type, msg) {
  const bar = $('status-bar');
  bar.className = 'status-bar show ' + type;
  bar.textContent = msg;
}
function clearStatus() { $('status-bar').className = 'status-bar'; }

function setBusy(busy) {
  $('btn-gen').disabled = busy;
  $('btn-text').innerHTML = busy
    ? '<span class="spinner"></span> 生成中…'
    : '生成图片';
}

function showGalleryCRTLoading() {
  const gallery = $('gallery');
  if (!gallery || $('gallery-crt-loading')) return;
  const empty = $('empty');
  if (empty) empty.style.display = 'none';
  const wrap = document.createElement('div');
  wrap.id = 'gallery-crt-loading';
  wrap.className = 'crt-loading-wrap';
  wrap.setAttribute('aria-busy', 'true');
  wrap.setAttribute('aria-live', 'polite');
  wrap.innerHTML = `
    <div class="crt-bezel">
      <div class="crt-label">OUTPUT</div>
      <div class="crt-screen" role="progressbar" aria-valuetext="图像生成中">
        <div class="crt-static-flicker" aria-hidden="true"></div>
        <div class="crt-scanbar" aria-hidden="true"></div>
        <div class="crt-scanbar crt-scanbar--trail" aria-hidden="true"></div>
        <div class="crt-scanlines" aria-hidden="true"></div>
        <div class="crt-content">
          <div class="crt-title">PIPELINE ACTIVE</div>
          <div class="crt-model"></div>
          <div class="crt-prompt-preview"></div>
          <div class="crt-status-row">
            <span>RASTER SYNC</span><span class="crt-dots"><span>.</span><span>.</span><span>.</span></span>
          </div>
        </div>
      </div>
    </div>`;
  wrap.querySelector('.crt-model').textContent = effectiveModelId();
  const pv = wrap.querySelector('.crt-prompt-preview');
  const raw = ($('prompt').value || '').trim();
  if (raw) {
    const short = raw.length > 96 ? raw.slice(0, 96) + '…' : raw;
    pv.textContent = '> ' + short.replace(/\s+/g, ' ');
  } else {
    pv.textContent = '> (no prompt)';
  }
  gallery.insertBefore(wrap, gallery.firstChild);
  wrap.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
}

function hideGalleryCRTLoading() {
  const el = $('gallery-crt-loading');
  if (el) el.remove();
  const gallery = $('gallery');
  const empty = $('empty');
  if (gallery && empty && !gallery.querySelector('.img-card')) {
    empty.style.display = '';
  }
}

function effectiveModelId() {
  return activeModel;
}

function parseDataURL(dataUrl) {
  const m = /^data:([^;,]+);base64,(.+)$/i.exec(String(dataUrl).trim());
  if (!m) return null;
  return { mime: m[1].trim(), b64: m[2].trim().replace(/\s/g, '') };
}

function renderRefPreviews() {
  const root = $('ref-previews');
  if (!root) return;
  root.innerHTML = '';
  referenceEntries.forEach((e, i) => {
    const div = document.createElement('div');
    div.className = 'ref-thumb';
    const img = document.createElement('img');
    img.src = e.thumb;
    img.alt = '';
    const rm = document.createElement('button');
    rm.type = 'button';
    rm.setAttribute('aria-label', '移除');
    rm.textContent = '×';
    rm.addEventListener('click', (ev) => {
      ev.stopPropagation();
      referenceEntries.splice(i, 1);
      renderRefPreviews();
    });
    div.appendChild(img);
    div.appendChild(rm);
    root.appendChild(div);
  });
}

function addReferenceFromFile(file) {
  if (referenceEntries.length >= 6) {
    setStatus('error', '最多 6 张参考图');
    return;
  }
  if (!file.type.startsWith('image/')) return;
  if (file.size > 4 * 1024 * 1024) {
    setStatus('error', '单张图片不能超过 4MB');
    return;
  }
  const reader = new FileReader();
  reader.onload = () => {
    const p = parseDataURL(reader.result);
    if (!p) return;
    referenceEntries.push({ mime: p.mime, b64: p.b64, thumb: reader.result });
    renderRefPreviews();
  };
  reader.readAsDataURL(file);
}

function updateGptResEst() {
  const est = $('gpt-res-est');
  const sel = $('gpt-size');
  if (!est || !sel) return;
  const v = sel.value;
  if (!v) {
    est.textContent = '分辨率 auto（由上游决定）';
    return;
  }
  const xy = v.toLowerCase().split('x');
  if (xy.length === 2) est.textContent = '分辨率 ' + xy[0] + '×' + xy[1];
}

function onGptCustomDimsInput() {
  if (!$('gpt-custom-size')?.checked) return;
  const w = parseInt($('gpt-cw')?.value, 10) || 0;
  const h = parseInt($('gpt-ch')?.value, 10) || 0;
  const est = $('gpt-res-est');
  if (est && w && h) est.textContent = '分辨率 ' + w + '×' + h;
}

function wireRefAndGpt() {
  $('gpt-ratio-grid')?.addEventListener('click', (ev) => {
    const btn = ev.target.closest('.ratio-btn');
    if (!btn) return;
    document.querySelectorAll('#gpt-ratio-grid .ratio-btn').forEach((b) => b.classList.remove('active'));
    btn.classList.add('active');
    const sz = btn.dataset.size || '';
    const sel = $('gpt-size');
    if (sel) sel.value = sz;
    updateGptResEst();
  });
  $('gpt-size')?.addEventListener('change', updateGptResEst);
  $('gpt-custom-size')?.addEventListener('change', () => {
    const on = $('gpt-custom-size').checked;
    const cd = $('gpt-custom-dims');
    const rg = $('gpt-ratio-grid');
    if (cd) cd.style.display = on ? 'grid' : 'none';
    if (rg) rg.style.display = on ? 'none' : 'grid';
    if (on) onGptCustomDimsInput();
    else updateGptResEst();
  });
  $('gpt-cw')?.addEventListener('input', onGptCustomDimsInput);
  $('gpt-ch')?.addEventListener('input', onGptCustomDimsInput);

  // ── Gemini ratio grid ──
  $('gem-ratio-grid')?.addEventListener('click', (ev) => {
    const btn = ev.target.closest('.ratio-btn');
    if (!btn) return;
    document.querySelectorAll('#gem-ratio-grid .ratio-btn').forEach((b) => b.classList.remove('active'));
    btn.classList.add('active');
    save('gem-aspect', btn.dataset.aspect || '1:1');
  });
  // Restore gemini ratio
  (function() {
    const saved = load('gem-aspect', null);
    if (!saved) return;
    const grid = $('gem-ratio-grid');
    if (!grid) return;
    const match = grid.querySelector(`.ratio-btn[data-aspect="${saved}"]`);
    if (match) {
      grid.querySelectorAll('.ratio-btn').forEach((b) => b.classList.remove('active'));
      match.classList.add('active');
    }
  })();

  // ── Wan ratio grid ──
  $('wan-ratio-grid')?.addEventListener('click', (ev) => {
    const btn = ev.target.closest('.ratio-btn');
    if (!btn) return;
    document.querySelectorAll('#wan-ratio-grid .ratio-btn').forEach((b) => b.classList.remove('active'));
    btn.classList.add('active');
    save('wan-size', btn.dataset.size || '1024*1024');
  });
  // Restore wan ratio
  (function() {
    const saved = load('wan-size', null);
    if (!saved) return;
    const grid = $('wan-ratio-grid');
    if (!grid) return;
    const match = grid.querySelector(`.ratio-btn[data-size="${saved}"]`);
    if (match) {
      grid.querySelectorAll('.ratio-btn').forEach((b) => b.classList.remove('active'));
      match.classList.add('active');
    }
  })();

  // ── Wan edit type — show/hide bbox section ──
  $('wan-edit-type')?.addEventListener('change', () => {
    const val = $('wan-edit-type').value;
    const bboxSec = $('wan-bbox-section');
    if (bboxSec) bboxSec.style.display = val === 'inpaint' ? 'block' : 'none';
  });

  // ── Wan bbox add/remove ──
  function addWanBboxRow() {
    const list = $('wan-bbox-list');
    if (!list) return;
    const row = document.createElement('div');
    row.className = 'bbox-row';
    row.innerHTML = `
      <input type="number" placeholder="x1" min="0" title="左边缘 x" />
      <input type="number" placeholder="y1" min="0" title="上边缘 y" />
      <input type="number" placeholder="x2" min="0" title="右边缘 x" />
      <input type="number" placeholder="y2" min="0" title="下边缘 y" />
      <button class="bbox-del" type="button" title="删除">✕</button>
    `;
    row.querySelector('.bbox-del').addEventListener('click', () => row.remove());
    list.appendChild(row);
  }
  $('wan-bbox-add')?.addEventListener('click', addWanBboxRow);

  const pick = () => $('ref-input')?.click();
  $('ref-pick')?.addEventListener('click', (e) => {
    e.stopPropagation();
    pick();
  });
  $('ref-zone')?.addEventListener('click', pick);
  $('ref-input')?.addEventListener('change', (e) => {
    const files = e.target.files;
    if (!files) return;
    for (const f of files) addReferenceFromFile(f);
    e.target.value = '';
  });
  const z = $('ref-zone');
  if (z) {
    ['dragenter', 'dragover'].forEach((ev) => {
      z.addEventListener(ev, (e) => {
        e.preventDefault();
        z.classList.add('drag');
      });
    });
    ['dragleave', 'drop'].forEach((ev) => {
      z.addEventListener(ev, (e) => {
        e.preventDefault();
        z.classList.remove('drag');
      });
    });
    z.addEventListener('drop', (e) => {
      const dt = e.dataTransfer;
      if (!dt?.files) return;
      for (const f of dt.files) addReferenceFromFile(f);
    });
  }
  updateGptResEst();
}

// ── Build request body per provider ────────────────────
function buildBody() {
  const prompt = $('prompt').value.trim();
  const base = { model: effectiveModelId(), prompt };

  if (activeProvider === 'openai') {
    let size = '';
    if ($('gpt-custom-size')?.checked) {
      const w = parseInt($('gpt-cw')?.value, 10) || 1024;
      const h = parseInt($('gpt-ch')?.value, 10) || 1024;
      size = w + 'x' + h;
    } else {
      size = $('gpt-size').value;
    }
    const quality = $('gpt-quality').value;
    const style = $('gpt-style').value;
    if (size) base.size = size;
    if (quality) base.quality = quality;
    if (style) base.style = style;
    base.n = parseInt($('gpt-n').value) || 1;
    base.response_format = $('gpt-format').value || 'url';
  } else if (activeProvider === 'gemini') {
    const gemBtn = document.querySelector('#gem-ratio-grid .ratio-btn.active');
    base.aspect_ratio = gemBtn?.dataset.aspect || '1:1';
    base.n = parseInt($('gem-n').value) || 1;
    base.response_format = 'b64_json';
    if (activeModel !== 'gemini/gemini-2.5-flash-image') {
      const g = $('gem-image-size');
      if (g) {
        base.image_size = g.value || (activeModel === 'gemini/gemini-3-pro-image-preview' ? '1K' : '512');
      }
    }
    const budget = parseInt($('gem-thinking')?.value ?? 0);
    if (budget > 0) base.thinking_budget = budget;
  } else if (activeProvider === 'wan') {
    const wanBtn = document.querySelector('#wan-ratio-grid .ratio-btn.active');
    base.size = wanBtn?.dataset.size || '1024*1024';
    base.n = parseInt($('wan-n').value) || 1;
    if (activeModel === 'wan/wan2.7-image-pro') {
      base.thinking_mode = $('wan-thinking')?.checked ?? false;
    }
    const editType = $('wan-edit-type')?.value || '';
    if (editType) base.wan_edit_type = editType;
    if (editType === 'inpaint') {
      const bboxRows = document.querySelectorAll('#wan-bbox-list .bbox-row');
      const bboxes = [];
      bboxRows.forEach((row) => {
        const inputs = row.querySelectorAll('input[type=number]');
        if (inputs.length === 4) {
          bboxes.push({ x1: parseInt(inputs[0].value)||0, y1: parseInt(inputs[1].value)||0,
                        x2: parseInt(inputs[2].value)||0, y2: parseInt(inputs[3].value)||0 });
        }
      });
      if (bboxes.length) base.wan_bbox_list = bboxes;
    }
  }
  if (referenceEntries.length) {
    base.reference_images = referenceEntries.map((e) => ({ mime_type: e.mime, b64_json: e.b64 }));
  }
  return base;
}

// ── Add images to gallery ───────────────────────────────
function addImages(images, model, provider) {
  $('empty').style.display = 'none';
  const gallery = $('gallery');

  images.forEach(img => {
    const card = document.createElement('div');
    card.className = 'img-card';

    const image = document.createElement('img');
    if (img.url) { image.src = img.url; }
    else if (img.b64_json) {
      const mt = img.mime_type || 'image/png';
      image.src = 'data:' + mt + ';base64,' + img.b64_json;
    }
    image.alt = model;
    image.loading = 'lazy';
    card.appendChild(image);

    const meta = document.createElement('div');
    meta.className = 'meta';
    const badge = `<span class="provider-badge ${provider}">${provider}</span>`;
    const time = new Date().toLocaleTimeString();
    let link = '';
    if (img.url) {
      link = `<a href="${img.url}" target="_blank" rel="noopener">原图 ↗</a>`;
    } else if (img.b64_json) {
      const a = document.createElement('a');
      a.href = image.src;
      a.download = 'genpic-' + Date.now() + '.png';
      a.textContent = '下载 ↓';
      link = a.outerHTML;
    }
    meta.innerHTML = badge + `<span>${time}</span>` + link;
    card.appendChild(meta);

    if (img.revised_prompt) {
      const rp = document.createElement('div');
      rp.className = 'revised';
      rp.textContent = '修订: ' + img.revised_prompt;
      card.appendChild(rp);
    }

    gallery.insertBefore(card, gallery.firstChild);
  });
}

// ── Generate ────────────────────────────────────────────
$('btn-gen').addEventListener('click', async () => {
  const baseURL = effectiveBaseURL();
  const apiKey  = $('api-key').value.trim();
  const prompt  = $('prompt').value.trim();

  if (!prompt) { setStatus('error', '请输入提示词'); return; }
  if (!baseURL) {
    setStatus('error', '请填写接口地址，或在服务器 config.yaml 中配置 mvp_lite.default_base_url');
    openCredDialog();
    return;
  }
  if (!apiKey)  { setStatus('error', '请点击右上角 ⚙️ 填写完整密钥'); openCredDialog(); return; }
  if (looksLikeMaskedKey(apiKey)) {
    setStatus('error', '密钥疑似脱敏或不完整，请替换为完整密钥');
    openCredDialog();
    return;
  }

  const body = buildBody();
  body.base_url = baseURL;
  body.api_key  = apiKey;

  clearStatus();
  setBusy(true);
  showGalleryCRTLoading();

  try {
    const res = await genpicFetch('/api/generate', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    const raw = await res.text();
    let data = {};
    if (raw) {
      try {
        data = JSON.parse(raw);
      } catch {
        setStatus('error', '失败：HTTP ' + res.status + ' — ' + raw.trim().slice(0, 200));
        return;
      }
    }

    if (!res.ok) {
      setStatus('error', '失败：' + (data?.error?.message ?? `HTTP ${res.status}`));
      return;
    }

    if (res.status === 202) {
      const jobId = data.id;
      if (!jobId) {
        setStatus('error', '未返回任务 id，无法轮询状态');
        return;
      }
      const meta = {
        model: effectiveModelId(),
        prompt,
        provider: activeProvider,
        status: data.status || 'queued',
        startedAt: Date.now(),
      };
      taskQueue.set(jobId, meta);
      renderTaskQueue();
      void pollJobUntilDone(jobId, meta);
      setStatus('success', '任务已提交，可同时发起多个生成');
      hideGalleryCRTLoading();
      setBusy(false);
      return;
    }

    const images = data.images ?? (data.data ? data.data : []);
    if (images.length === 0) {
      setStatus('error', '未返回图片，请检查参数或稍后重试');
      return;
    }

    setStatus('success', `✓ 生成 ${images.length} 张图片（${effectiveModelId()}）`);
    addImages(images, effectiveModelId(), activeProvider);
    saveToHistory(images, effectiveModelId(), activeProvider, prompt);
  } catch (err) {
    setStatus('error', '网络错误：' + err.message);
  } finally {
    hideGalleryCRTLoading();
    setBusy(false);
  }
});

// Ctrl/Cmd+Enter to submit
$('prompt').addEventListener('keydown', e => {
  if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') $('btn-gen').click();
});

// ── History storage ─────────────────────────────────────
// Server-backed rows use job `id` (same as DB primary key). Anonymous scope is
// localStorage `genpic:browser-session` → header `X-Genpic-Session` → DB `session_id`.
const HIST_KEY = 'genpic:history-v1';
const HIST_MAX = 20;
const HIST_PAGE_SIZE = 20;
const HIST_CLEAR_TS_KEY = 'genpic:hist-clear-ts';
const HIST_HIDDEN_IDS_KEY = 'genpic:hist-hidden-job-ids';

/** Merged rows for history UI (page 1 + 加载更多); null before first merge this page load. */
let histViewEntries = null;
let histNextCursor = null;
let histLoadMoreBusy = false;

function histClearTsLoad() {
  const t = parseInt(localStorage.getItem(HIST_CLEAR_TS_KEY) || '0', 10);
  return Number.isFinite(t) && t > 0 ? t : 0;
}

function histHiddenIdsLoad() {
  try {
    const a = JSON.parse(localStorage.getItem(HIST_HIDDEN_IDS_KEY) || '[]');
    return new Set(Array.isArray(a) ? a : []);
  } catch {
    return new Set();
  }
}

function histHiddenIdsAdd(id) {
  if (!id) return;
  const s = histHiddenIdsLoad();
  s.add(id);
  try { localStorage.setItem(HIST_HIDDEN_IDS_KEY, JSON.stringify([...s])); } catch {}
}

function histLoad() {
  try { return JSON.parse(localStorage.getItem(HIST_KEY) || '[]'); } catch { return []; }
}

function histPush(entry) {
  const MAX_B64 = 400_000;
  const safe = {
    ...entry,
    source: entry.source || 'local',
    images: (entry.images || []).map(img => ({
      url: img.url || undefined,
      mime_type: img.mime_type || undefined,
      revised_prompt: img.revised_prompt || undefined,
      b64_json: (img.b64_json && img.b64_json.length <= MAX_B64) ? img.b64_json : undefined,
    })),
  };
  let arr = histLoad();
  arr.unshift(safe);
  if (arr.length > HIST_MAX) arr = arr.slice(0, HIST_MAX);
  try { localStorage.setItem(HIST_KEY, JSON.stringify(arr)); } catch {}
}

function histDelete(id) {
  try { localStorage.setItem(HIST_KEY, JSON.stringify(histLoad().filter(e => e.id !== id))); } catch {}
  if (/^[0-9a-f]{32}$/i.test(id)) histHiddenIdsAdd(id);
}

function histClear() {
  try { localStorage.removeItem(HIST_KEY); } catch {}
  try { localStorage.setItem(HIST_CLEAR_TS_KEY, String(Date.now())); } catch {}
  try { localStorage.removeItem(HIST_HIDDEN_IDS_KEY); } catch {}
}

/** GET /jobs?page — same session header as generate (see getOrCreateGenpicSessionId). */
async function fetchServerJobPage(cursor) {
  const params = new URLSearchParams({ limit: String(HIST_PAGE_SIZE) });
  if (cursor) params.set('cursor', cursor);
  try {
    const r = await genpicFetch('/jobs?' + params.toString());
    if (!r.ok) return null;
    const text = await r.text();
    let j;
    try {
      j = JSON.parse(text);
    } catch {
      return null;
    }
    if (!j || !Array.isArray(j.data)) return null;
    const rawNext = j.next_cursor;
    const nextCursor = (rawNext === undefined || rawNext === null || rawNext === '')
      ? null
      : String(rawNext);
    return { jobs: j.data, nextCursor };
  } catch {
    return null;
  }
}

function jobRecordToHistEntry(job) {
  if (!job || !job.id) return null;
  const images = (job.data || []).map(img => ({
    url: img.url,
    b64_json: img.b64_json,
    mime_type: img.mime_type,
    revised_prompt: img.revised_prompt,
  }));
  const ts = (typeof job.created_at === 'number' ? job.created_at : 0) * 1000;
  let errorMessage = '';
  if (job.error && job.error.message) errorMessage = job.error.message;
  return {
    id: job.id,
    ts: ts || Date.now(),
    model: job.model || '',
    provider: job.provider || '',
    prompt: job.prompt || '',
    visibility: job.visibility || 'private',
    status: job.status || '',
    errorMessage,
    images,
    source: 'server',
  };
}

/** Merge first page of DB jobs with local cache; set histViewEntries + histNextCursor. */
async function mergeAndPersistHistory(opts) {
  const reset = opts == null || opts.reset !== false;
  if (reset) {
    histNextCursor = null;
    histLoadMoreBusy = false;
  }

  const clearTs = histClearTsLoad();
  const hidden = histHiddenIdsLoad();
  let local = [];
  try { local = histLoad(); } catch { local = []; }
  const byId = new Map();

  for (const e of local) {
    if (!e || !e.id) continue;
    if (hidden.has(e.id)) continue;
    if (clearTs > 0 && (e.ts || 0) <= clearTs) continue;
    byId.set(e.id, { ...e, source: e.source || 'local' });
  }

  const page = await fetchServerJobPage(null);
  if (page && page.jobs) {
    histNextCursor = page.nextCursor;
    for (const job of page.jobs) {
      if (!job || !job.id) continue;
      if (hidden.has(job.id)) continue;
      const cts = (typeof job.created_at === 'number' ? job.created_at : 0) * 1000;
      if (clearTs > 0 && cts <= clearTs) continue;
      const row = jobRecordToHistEntry(job);
      if (row) byId.set(job.id, row);
    }
  } else {
    histNextCursor = null;
  }

  const mergedFull = [...byId.values()].sort((a, b) => (b.ts || 0) - (a.ts || 0));
  histViewEntries = mergedFull;
  try { localStorage.setItem(HIST_KEY, JSON.stringify(mergedFull.slice(0, HIST_MAX))); } catch {}
}

async function appendHistoryPage() {
  if (!histNextCursor || histLoadMoreBusy) return;
  histLoadMoreBusy = true;
  updateHistLoadMoreUI();
  const clearTs = histClearTsLoad();
  const hidden = histHiddenIdsLoad();
  const page = await fetchServerJobPage(histNextCursor);
  histLoadMoreBusy = false;
  if (!page) {
    updateHistLoadMoreUI();
    renderHistoryPanel();
    return;
  }
  if (!page.jobs || !page.jobs.length) {
    histNextCursor = null;
    updateHistLoadMoreUI();
    renderHistoryPanel();
    return;
  }
  histNextCursor = page.nextCursor;

  const byId = new Map();
  for (const e of (histViewEntries || [])) {
    if (e && e.id) byId.set(e.id, e);
  }
  for (const job of page.jobs) {
    if (!job || !job.id) continue;
    if (hidden.has(job.id)) continue;
    const cts = (typeof job.created_at === 'number' ? job.created_at : 0) * 1000;
    if (clearTs > 0 && cts <= clearTs) continue;
    const row = jobRecordToHistEntry(job);
    if (row) byId.set(job.id, row);
  }
  histViewEntries = [...byId.values()].sort((a, b) => (b.ts || 0) - (a.ts || 0));
  updateHistLoadMoreUI();
  renderHistoryPanel();
}

function updateHistLoadMoreUI() {
  const wrap = $('hist-load-more-wrap');
  const btn = $('hist-load-more-btn');
  if (!wrap || !btn) return;
  const n = (histViewEntries !== null) ? histViewEntries.length : histLoad().length;
  if (n === 0) {
    wrap.style.display = 'none';
    return;
  }
  const show = Boolean(histNextCursor && histViewEntries !== null);
  wrap.style.display = show ? '' : 'none';
  if (!show) return;
  btn.disabled = histLoadMoreBusy;
  btn.textContent = histLoadMoreBusy ? '加载中…' : '加载更多';
}

function saveToHistory(images, model, provider, prompt, serverJobId) {
  if (!images || !images.length) return;
  const id = (serverJobId && String(serverJobId).trim())
    ? String(serverJobId).trim()
    : (Date.now() + '-' + Math.random().toString(36).slice(2, 7));
  histPush({
    id,
    ts: Date.now(),
    model,
    provider,
    prompt,
    source: serverJobId ? 'server' : 'local',
    images: images.map(img => ({
      url: img.url,
      b64_json: img.b64_json,
      mime_type: img.mime_type,
      revised_prompt: img.revised_prompt,
    })),
  });
  void mergeAndPersistHistory({ reset: true }).catch(() => {});
}

function fmtHistTime(ts) {
  try { return new Date(ts).toLocaleString(); } catch { return ''; }
}

function renderHistoryPanel() {
  const arr = histViewEntries !== null ? histViewEntries : histLoad();
  const emptyEl = $('hist-empty');
  const listEl  = $('hist-list');
  if (!emptyEl || !listEl) return;
  if (arr.length === 0) {
    emptyEl.style.display = '';
    listEl.style.display  = 'none';
    listEl.innerHTML = '';
    updateHistLoadMoreUI();
    return;
  }
  emptyEl.style.display = 'none';
  listEl.style.display  = '';
  listEl.innerHTML = '';

  arr.forEach(entry => {
    const card = document.createElement('div');
    card.className = 'hist-entry';

    const hdr = document.createElement('div');
    hdr.className = 'hist-entry-header';

    const tsSpan = document.createElement('span');
    tsSpan.className = 'hist-ts';
    tsSpan.textContent = fmtHistTime(entry.ts);

    const badge = document.createElement('span');
    badge.className = 'provider-badge ' + (entry.provider || '');
    badge.textContent = entry.provider || '';

    const modelSpan = document.createElement('span');
    modelSpan.className = 'hist-model';
    modelSpan.textContent = entry.model || '';

    const delBtn = document.createElement('button');
    delBtn.className = 'hist-del';
    delBtn.title = '从列表移除';
    delBtn.textContent = '×';
    delBtn.addEventListener('click', () => {
      histDelete(entry.id);
      mergeAndPersistHistory({ reset: true })
        .then(() => renderHistoryPanel())
        .catch(() => renderHistoryPanel());
    });

    hdr.appendChild(tsSpan);
    hdr.appendChild(badge);
    hdr.appendChild(modelSpan);

    if (authUser && entry.source === 'server' && /^[0-9a-f]{32}$/i.test(String(entry.id))) {
      const pubRow = document.createElement('label');
      pubRow.className = 'hist-pub-row';
      const chk = document.createElement('input');
      chk.type = 'checkbox';
      chk.checked = (entry.visibility === 'public');
      chk.title = '公开到社区';
      chk.addEventListener('change', async () => {
        const want = chk.checked;
        try {
          const r = await genpicFetch('/api/jobs/' + encodeURIComponent(entry.id) + '/visibility', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ visibility: want ? 'public' : 'private' }),
          });
          if (!r.ok) chk.checked = !want;
          else entry.visibility = want ? 'public' : 'private';
        } catch {
          chk.checked = !want;
        }
      });
      pubRow.appendChild(chk);
      pubRow.appendChild(document.createTextNode('公开'));
      hdr.appendChild(pubRow);
    }

    hdr.appendChild(delBtn);
    card.appendChild(hdr);

    if (entry.prompt) {
      const p = document.createElement('div');
      p.className = 'hist-prompt';
      p.textContent = entry.prompt;
      card.appendChild(p);
    } else if (entry.source === 'server') {
      const p = document.createElement('div');
      p.className = 'hist-prompt hist-prompt-locked';
      p.textContent = '登录后可见完整提示词';
      card.appendChild(p);
    }

    if (entry.status && entry.status !== 'succeeded') {
      const st = document.createElement('div');
      st.className = 'hist-status' + (entry.status === 'failed' ? ' hist-status-fail' : '');
      if (entry.status === 'failed') {
        st.textContent = '失败：' + (entry.errorMessage || '未知错误');
      } else {
        st.textContent = '状态：' + entry.status;
      }
      card.appendChild(st);
    }

    const imgsEl = document.createElement('div');
    imgsEl.className = 'hist-images';
    (entry.images || []).forEach(img => {
      const item = document.createElement('div');
      item.className = 'hist-img-item';

      const el = document.createElement('img');
      el.alt = '';
      el.loading = 'lazy';
      if (img.b64_json) {
        el.src = 'data:' + (img.mime_type || 'image/png') + ';base64,' + img.b64_json;
      } else if (img.url) {
        el.src = img.url;
      }
      item.appendChild(el);

      const a = document.createElement('a');
      if (img.b64_json) {
        a.href = el.src;
        a.download = 'genpic-' + entry.ts + '.png';
        a.textContent = '下载';
      } else if (img.url) {
        a.href = img.url;
        a.target = '_blank';
        a.rel = 'noopener';
        a.textContent = '原图 ↗';
      }
      if (a.href) item.appendChild(a);

      imgsEl.appendChild(item);
    });
    card.appendChild(imgsEl);

    listEl.appendChild(card);
  });
  updateHistLoadMoreUI();
}

function openAuthModal(tab) {
  const modal = $('auth-modal');
  if (!modal) return;
  modal.hidden = false;
  const err = $('auth-modal-error');
  if (err) err.style.display = 'none';
  setAuthModalTab(tab || 'login');
}

function setAuthModalTab(tab) {
  const loginPane = $('auth-pane-login');
  const regPane = $('auth-pane-register');
  document.querySelectorAll('#auth-tabs .modal-tab').forEach((t) => {
    t.classList.toggle('active', t.dataset.tab === tab);
  });
  if (loginPane) loginPane.hidden = (tab !== 'login');
  if (regPane) regPane.hidden = (tab !== 'register');
}

document.addEventListener('click', (e) => {
  if (!e.target.closest('.auth-wrap')) closeAuthDropdown();
});

$('btn-auth')?.addEventListener('click', (e) => {
  e.stopPropagation();
  if (authUser) {
    const dd = $('auth-dropdown');
    if (dd) dd.hidden = !dd.hidden;
    return;
  }
  openAuthModal('login');
});

$('auth-tabs')?.addEventListener('click', (e) => {
  const t = e.target.closest('.modal-tab');
  if (!t) return;
  setAuthModalTab(t.dataset.tab);
});

$('auth-modal-cancel')?.addEventListener('click', () => {
  const m = $('auth-modal');
  if (m) m.hidden = true;
});

$('auth-modal')?.addEventListener('click', (e) => {
  if (e.target.id === 'auth-modal') e.currentTarget.hidden = true;
});

$('auth-modal-submit')?.addEventListener('click', async () => {
  const activeTab = document.querySelector('#auth-tabs .modal-tab.active');
  const mode = activeTab?.dataset.tab === 'register' ? 'register' : 'login';
  const errEl = $('auth-modal-error');
  if (errEl) errEl.style.display = 'none';
  try {
    if (mode === 'login') {
      const email = $('auth-email').value.trim();
      const password = $('auth-password').value;
      const r = await genpicFetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password }),
      });
      const data = await r.json().catch(() => ({}));
      if (!r.ok) throw new Error(data?.error?.message || data?.message || '登录失败');
    } else {
      const email = $('auth-reg-email').value.trim();
      const password = $('auth-reg-password').value;
      const display_name = $('auth-reg-name').value.trim();
      const r = await genpicFetch('/api/auth/register', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password, display_name }),
      });
      const data = await r.json().catch(() => ({}));
      if (!r.ok) throw new Error(data?.error?.message || data?.message || '注册失败');
    }
    $('auth-modal').hidden = true;
    await refreshAuthUser();
    await mergeAndPersistHistory({ reset: true });
    renderHistoryPanel();
  } catch (err) {
    if (errEl) {
      errEl.textContent = err.message || String(err);
      errEl.style.display = 'block';
    }
  }
});

$('btn-auth-logout')?.addEventListener('click', async () => {
  closeAuthDropdown();
  try {
    await genpicFetch('/api/auth/logout', { method: 'POST' });
  } catch (_) {}
  authUser = null;
  updateAuthChrome();
});

async function openPrivacyModal() {
  closeAuthDropdown();
  const modal = $('privacy-modal');
  if (!modal) return;
  modal.hidden = false;
  try {
    const r = await genpicFetch('/api/user/settings');
    if (r.ok) {
      const s = await r.json();
      $('set-auto-public').checked = !!s.community_auto_public;
      $('set-prompt-public').checked = !!s.prompt_public;
    }
  } catch (_) {}
}

$('btn-open-privacy')?.addEventListener('click', () => { void openPrivacyModal(); });

$('privacy-modal-close')?.addEventListener('click', () => {
  const m = $('privacy-modal');
  if (m) m.hidden = true;
});

$('privacy-modal')?.addEventListener('click', (e) => {
  if (e.target.id === 'privacy-modal') e.currentTarget.hidden = true;
});

$('privacy-modal-save')?.addEventListener('click', async () => {
  try {
    await genpicFetch('/api/user/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        community_auto_public: $('set-auto-public')?.checked ?? false,
        prompt_public: $('set-prompt-public')?.checked ?? false,
      }),
    });
    $('privacy-modal').hidden = true;
  } catch (_) {}
});

$('community-feed-scroll')?.addEventListener('scroll', () => {
  const el = $('community-feed-scroll');
  if (!el || activeVendorId !== 'community' || communityLoading || !communityNextCursor) return;
  if (el.scrollTop + el.clientHeight >= el.scrollHeight - 100) {
    void loadCommunityFeed(false);
  }
});

$('hist-clear-btn')?.addEventListener('click', () => {
  if (confirm('确认清空全部生成历史？')) {
    histClear();
    mergeAndPersistHistory({ reset: true })
      .then(() => renderHistoryPanel())
      .catch(() => renderHistoryPanel());
  }
});

$('hist-load-more-btn')?.addEventListener('click', () => {
  void appendHistoryPage();
});
