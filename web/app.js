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

let jobDetailCtx = null;
let templatePreviewCtx = null;

async function copyTextToClipboard(text) {
  const t = String(text || '');
  if (!t) return false;
  try {
    await navigator.clipboard.writeText(t);
    return true;
  } catch {
    try {
      const ta = document.createElement('textarea');
      ta.value = t;
      ta.style.position = 'fixed';
      ta.style.left = '-9999px';
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      document.body.removeChild(ta);
      return true;
    } catch {
      return false;
    }
  }
}

let genpicToastTimer = null;
function showGenpicToast(message) {
  let el = $('genpic-toast');
  if (!el) {
    el = document.createElement('div');
    el.id = 'genpic-toast';
    el.className = 'genpic-toast';
    el.setAttribute('role', 'status');
    document.body.appendChild(el);
  }
  el.textContent = message;
  el.classList.add('genpic-toast--show');
  clearTimeout(genpicToastTimer);
  genpicToastTimer = setTimeout(() => {
    el.classList.remove('genpic-toast--show');
  }, 2200);
}

async function copyDescriptionWithToast(text) {
  const t = String(text || '').trim();
  if (!t) {
    showGenpicToast('暂无描述可复制');
    return;
  }
  const ok = await copyTextToClipboard(t);
  if (ok) showGenpicToast('已复制描述');
  else setStatus('error', '复制失败，请重试');
}

function statusZh(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'succeeded') return '成功';
  if (s === 'failed') return '失败';
  if (s === 'running') return '运行中';
  if (s === 'queued') return '排队中';
  return status || '未知';
}

function formatMsHuman(ms) {
  if (ms == null || !Number.isFinite(ms) || ms < 0) return '';
  const s = Math.floor(ms / 1000);
  if (s < 60) return s + ' 秒';
  const m = Math.floor(s / 60);
  return m + ' 分 ' + (s % 60) + ' 秒';
}

function normalizeJobDetailPayload(obj) {
  if (!obj) return {};
  const p = obj.params || {};
  const imgs = obj.images || obj.data || [];
  return {
    id: obj.id || '',
    model: obj.model || p.model || '',
    provider: obj.provider || '',
    prompt: obj.prompt || '',
    status: obj.status || '',
    errorMessage: obj.errorMessage || (obj.error && obj.error.message) || '',
    images: imgs,
    params: p,
    tokens_used: obj.tokens_used,
    upstream_request_id: obj.upstream_request_id || '',
    processing_ms: obj.processing_ms,
    created_at: obj.created_at,
    started_at: obj.started_at,
    finished_at: obj.finished_at,
    ts: obj.ts,
    reference_images: obj.reference_images,
  };
}

/** Same-origin artifact URL → companion JPEG preview path (lazy-generated on server). */
function genpicArtifactThumbFromUrl(url) {
  const u = String(url || '').trim();
  const m = u.match(/^\/api\/artifacts\/([a-f0-9]{32})\/(\d+)\.([a-z0-9]+)$/i);
  if (!m) return '';
  return '/api/artifacts/' + m[1] + '/' + m[2] + '_thumb.jpg';
}

/** Small image for lists/cards (preview); falls back to full URL or data URI. */
function genpicImgListSrc(img) {
  if (!img) return '';
  const t = String(img.thumb_url || '').trim();
  if (t) return t;
  const guess = genpicArtifactThumbFromUrl(img.url);
  if (guess) return guess;
  if (img.b64_json) return 'data:' + (img.mime_type || 'image/png') + ';base64,' + img.b64_json;
  if (img.url) return String(img.url).trim();
  return '';
}

/** Full-resolution source for modal / download. */
function genpicImgFullSrc(img) {
  if (!img) return '';
  if (img.url) return String(img.url).trim();
  if (img.b64_json) return 'data:' + (img.mime_type || 'image/png') + ';base64,' + img.b64_json;
  return '';
}

/** If preview URL fails (e.g. webp without server-side thumb), load full image once. */
function genpicBindPreviewImgFallback(el, img) {
  if (!el || !img) return;
  el.addEventListener('error', () => {
    const u = genpicImgFullSrc(img);
    if (u && el.src !== u) el.src = u;
  }, { once: true });
}

function resolveProcessingMs(ctx) {
  if (ctx.processing_ms != null && Number.isFinite(Number(ctx.processing_ms))) return Number(ctx.processing_ms);
  const fs = ctx.finished_at;
  const st = ctx.started_at;
  if (typeof fs === 'number' && typeof st === 'number' && fs > st) return (fs - st) * 1000;
  return null;
}

function buildJobDetailMetaHTML(ctx) {
  const proc = resolveProcessingMs(ctx);
  const procStr = proc != null ? formatMsHuman(proc) : '';
  const tokens = ctx.tokens_used;
  let computeStr = '未知';
  if (tokens != null && Number.isFinite(Number(tokens)) && Number(tokens) > 0) {
    computeStr = String(tokens) + '（上游 tokens；聚合站若按算力计费以控制台为准）';
  }
  const jobId = String(ctx.id || '').trim();

  let createdStr = '';
  if (typeof ctx.created_at === 'number' && ctx.created_at > 0) {
    try { createdStr = new Date(ctx.created_at * 1000).toLocaleString(); } catch { createdStr = ''; }
  }
  if (!createdStr && ctx.ts) {
    try { createdStr = new Date(ctx.ts).toLocaleString(); } catch { createdStr = ''; }
  }

  const st = ctx.status || '';
  let stClass = 'run';
  if (st === 'succeeded') stClass = 'ok';
  if (st === 'failed') stClass = 'fail';

  const idHtml = jobId ? `<code>${genpicEscape(jobId)}</code>` : '未知';

  const rows = [];
  rows.push(['状态', `<span class="status-tag ${stClass}">${genpicEscape(statusZh(st))}</span>`]);
  rows.push(['使用模型', genpicEscape(ctx.model || '未知')]);
  rows.push(['任务ID', idHtml]);
  rows.push(['处理耗时', genpicEscape(procStr || '未知')]);
  rows.push(['消耗算力', genpicEscape(computeStr)]);
  rows.push(['创作时间', genpicEscape(createdStr || '未知')]);

  return rows.map(([k, v]) => `<div><dt>${genpicEscape(k)}</dt><dd>${v}</dd></div>`).join('');
}

function openJobDetail(raw) {
  const ctx = normalizeJobDetailPayload(raw);
  jobDetailCtx = ctx;
  const mod = $('job-detail-modal');
  const wrap = $('job-detail-img-wrap');
  const img = $('job-detail-img');
  const pr = $('job-detail-prompt');
  const meta = $('job-detail-meta');
  const im0 = (ctx.images && ctx.images[0]) ? ctx.images[0] : {};
  const src = genpicImgFullSrc(im0);
  if (img) {
    img.src = src || '';
    img.hidden = !src;
    if (src) {
      img.dataset.fullSrc = src;
      img.removeAttribute('title');
    } else {
      img.removeAttribute('data-full-src');
    }
  }
  if (wrap) {
    wrap.hidden = !src;
    wrap.tabIndex = src ? 0 : -1;
  }
  if (pr) pr.textContent = ctx.prompt || '（无提示词）';
  if (meta) meta.innerHTML = buildJobDetailMetaHTML(ctx);
  const jobIdOk = typeof ctx.id === 'string' && /^[a-f0-9]{32}$/i.test(ctx.id);
  const canSave = !!(authUser && ctx.status === 'succeeded' && jobIdOk);
  const savePr = $('job-detail-save-template');
  if (savePr) savePr.hidden = !canSave;
  if (mod) mod.hidden = false;
  document.body.style.overflow = 'hidden';
}

function closeJobDetail() {
  const mod = $('job-detail-modal');
  const img = $('job-detail-img');
  const wrap = $('job-detail-img-wrap');
  if (img) {
    img.removeAttribute('data-full-src');
  }
  if (wrap) {
    wrap.hidden = true;
    wrap.tabIndex = -1;
  }
  if (mod) mod.hidden = true;
  jobDetailCtx = null;
  document.body.style.overflow = '';
}

async function openJobDetailFetchById(jobId) {
  if (!jobId) return;
  try {
    const r = await genpicFetch('/jobs/' + encodeURIComponent(jobId));
    const raw = await r.text();
    let j = {};
    if (raw) {
      try { j = JSON.parse(raw); } catch (_) {}
    }
    if (!r.ok) {
      setStatus('error', j?.error?.message || '无法加载任务详情');
      return;
    }
    openJobDetail(j);
  } catch (_) {
    setStatus('error', '网络错误');
  }
}

function histEntryToDetailPayload(entry) {
  if (!entry) return {};
  const ca = entry.created_at;
  return {
    id: entry.id,
    model: entry.model,
    provider: entry.provider,
    prompt: entry.prompt,
    status: entry.status || 'succeeded',
    errorMessage: entry.errorMessage,
    images: entry.images,
    params: entry.params,
    tokens_used: entry.tokens_used,
    upstream_request_id: entry.upstream_request_id,
    processing_ms: entry.processing_ms,
    created_at: typeof ca === 'number' ? ca : (entry.ts ? Math.floor(entry.ts / 1000) : undefined),
    started_at: entry.started_at,
    finished_at: entry.finished_at,
    ts: entry.ts,
    reference_images: entry.reference_images,
  };
}

function loadReferenceImagesForSimilar(refs) {
  return loadReferenceImagesForSimilarAsync(refs);
}

async function loadReferenceImagesForSimilarAsync(refs) {
  referenceEntries = [];
  if (!refs || !Array.isArray(refs)) {
    renderRefPreviews();
    return;
  }
  let n = 0;
  for (const r of refs) {
    if (n >= 6) break;
    const mime = (r.mime_type || 'image/png').trim();
    const url = String(r.url || '').trim();
    if (url) {
      try {
        const res = await fetch(url, { mode: 'cors', credentials: 'omit' });
        if (!res.ok) continue;
        const blob = await res.blob();
        const dataUrl = await blobToDataURL(blob);
        const comma = dataUrl.indexOf(',');
        const b64 = comma >= 0 ? dataUrl.slice(comma + 1).replace(/\s/g, '') : '';
        if (!b64) continue;
        referenceEntries.push({ mime, b64, thumb: dataUrl });
        n++;
      } catch (_) { /* CORS or network */ }
      continue;
    }
    const b64 = String(r.b64_json || '').trim().replace(/\s/g, '');
    if (!b64) continue;
    referenceEntries.push({ mime, b64, thumb: 'data:' + mime + ';base64,' + b64 });
    n++;
  }
  renderRefPreviews();
}

function blobToDataURL(blob) {
  return new Promise((resolve, reject) => {
    const fr = new FileReader();
    fr.onload = () => resolve(String(fr.result || ''));
    fr.onerror = () => reject(fr.error);
    fr.readAsDataURL(blob);
  });
}

/** Rebuild catalog model id (e.g. openai/gpt-image-2) from template API row (wire primary_model + provider). */
function genpicTemplateCatalogModelId(t) {
  if (!t) return '';
  const m = String(t.primary_model || '').trim();
  if (!m) return '';
  if (m.includes('/')) return m;
  const p = String(t.provider || '').trim().toLowerCase();
  if (p === 'openai' || p === 'gemini' || p === 'wan') return p + '/' + m;
  const fromParams = t.params && String(t.params.model || '').trim();
  if (fromParams && fromParams.includes('/')) return fromParams;
  return m;
}

async function applyGenpicTemplate(t) {
  if (!t) return;
  const catalog = genpicTemplateCatalogModelId(t);
  const p = { ...(t.params || {}) };
  if (catalog) p.model = catalog;
  const src = {
    id: t.id,
    model: catalog || (t.params && t.params.model) || '',
    prompt: t.prompt || '',
    status: 'succeeded',
    params: p,
    reference_images: t.reference_images,
  };
  await applyCreateSimilar(src);
}

function updateTemplateMoreVisibility() {
  const rail = $('template-rail');
  const more = $('btn-template-more');
  if (!rail || !more) return;
  const overflow = rail.scrollWidth > rail.clientWidth + 8;
  more.hidden = !overflow;
}

const TPL_PREVIEW_EYE_SVG = '<svg class="tpl-hit-eye" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.65" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M2 12s4.5-7 10-7 10 7 10 7-4.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3" fill="currentColor" stroke="none"/></svg>';

function templatePreviewModelChipsHTML(t) {
  if (!t) return '';
  const cat = genpicTemplateCatalogModelId(t);
  const wire = String(t.primary_model || '').trim();
  const chips = [];
  if (cat && cat.includes('/')) {
    const short = cat.split('/').pop();
    if (short) chips.push('<span class="tpl-preview-chip tpl-preview-chip-a">' + genpicEscape(short) + '</span>');
  }
  if (wire && (!cat || !cat.endsWith(wire))) {
    chips.push('<span class="tpl-preview-chip tpl-preview-chip-b">' + genpicEscape(wire) + '</span>');
  }
  if (!chips.length && cat) chips.push('<span class="tpl-preview-chip tpl-preview-chip-a">' + genpicEscape(cat) + '</span>');
  return chips.join('');
}

function openTemplatePreview(t) {
  if (!t) return;
  templatePreviewCtx = t;
  const mod = $('template-preview-modal');
  const wrap = $('template-preview-img-wrap');
  const img = $('template-preview-img');
  const titleEl = $('template-preview-title');
  const visStrip = $('template-preview-visibility');
  const modelRow = $('template-preview-model-row');
  const prodLine = $('template-preview-product-line');
  const promptBox = $('template-preview-prompt-box');
  const refInfo = $('template-preview-ref-info');
  const refsEl = $('template-preview-refs');
  const tagsEl = $('template-preview-tags');
  const fullSrc = genpicImgFullSrc({ url: String(t.result_image_url || '').trim() });
  if (img) {
    img.onload = null;
    img.onerror = null;
    if (fullSrc) {
      img.dataset.fullSrc = fullSrc;
      img.title = '在新窗口打开大图';
      img.hidden = false;
      img.src = fullSrc;
    } else {
      img.removeAttribute('data-full-src');
      img.title = '';
      img.src = '';
      img.hidden = true;
    }
  }
  if (wrap) {
    wrap.hidden = !fullSrc;
    wrap.tabIndex = fullSrc ? 0 : -1;
  }
  const title = String(t.title || '').trim();
  if (titleEl) titleEl.textContent = title || '模板预览';
  if (visStrip) {
    const pub = String(t.visibility || '').toLowerCase() === 'public';
    visStrip.textContent = pub ? '公用模板 · 全站用户可见' : '我的模板 · 仅自己可见';
    visStrip.className = 'tpl-preview-visibility' + (pub ? ' tpl-preview-visibility--public' : ' tpl-preview-visibility--private');
  }
  if (modelRow) modelRow.innerHTML = templatePreviewModelChipsHTML(t);
  const refs = Array.isArray(t.reference_images) ? t.reference_images : [];
  const modelsArr = Array.isArray(t.models) ? t.models : [];
  let nProducts = modelsArr.filter((m) => String(m || '').trim()).length;
  if (nProducts < 1) nProducts = 1; // 至少含原始模型自身，与 DB models_json 条数一致
  if (prodLine) {
    prodLine.textContent = '使用产品：' + nProducts + ' 个';
  }
  if (promptBox) promptBox.textContent = t.prompt || '（无提示词）';
  if (refInfo) refInfo.textContent = refs.length ? (refs.length + ' 个') : '';
  if (refsEl) {
    refsEl.innerHTML = '';
    if (!refs.length) {
      const p = document.createElement('p');
      p.className = 'tpl-preview-refs-empty';
      p.style.margin = '0';
      p.textContent = '无参考图';
      refsEl.appendChild(p);
    } else {
      let n = 0;
      for (const r of refs) {
        if (n >= 6) break;
        const mime = (r.mime_type || 'image/png').trim();
        const url = String(r.url || '').trim();
        if (url) {
          const im = document.createElement('img');
          im.className = 'tpl-preview-ref-thumb';
          im.alt = '参考图';
          im.loading = 'lazy';
          im.referrerPolicy = 'no-referrer';
          im.src = url;
          refsEl.appendChild(im);
          n++;
          continue;
        }
        const b64 = String(r.b64_json || '').trim().replace(/\s/g, '');
        if (!b64) continue;
        const im = document.createElement('img');
        im.className = 'tpl-preview-ref-thumb';
        im.alt = '参考图';
        im.loading = 'lazy';
        im.src = 'data:' + mime + ';base64,' + b64;
        refsEl.appendChild(im);
        n++;
      }
      if (!refsEl.querySelector('img')) {
        const p2 = document.createElement('p');
        p2.className = 'tpl-preview-refs-empty';
        p2.style.margin = '0';
        p2.textContent = '无参考图';
        refsEl.appendChild(p2);
      }
    }
  }
  if (tagsEl) {
    tagsEl.innerHTML = '';
    const rawTags = t.tags;
    if (Array.isArray(rawTags) && rawTags.length) {
      for (const tag of rawTags) {
        const s = String(tag || '').trim();
        if (!s) continue;
        const pill = document.createElement('span');
        pill.className = 'tpl-preview-tag-pill';
        pill.textContent = s;
        tagsEl.appendChild(pill);
      }
    }
    if (!tagsEl.childElementCount) {
      const em = document.createElement('span');
      em.className = 'tpl-preview-tags-empty';
      em.textContent = '暂无标签';
      tagsEl.appendChild(em);
    }
  }
  if (mod) mod.hidden = false;
  document.body.style.overflow = 'hidden';
}

function closeTemplatePreview() {
  const mod = $('template-preview-modal');
  const wrap = $('template-preview-img-wrap');
  const img = $('template-preview-img');
  if (img) {
    img.onload = null;
    img.onerror = null;
    img.removeAttribute('data-full-src');
    img.title = '';
  }
  if (wrap) {
    wrap.hidden = true;
    wrap.tabIndex = -1;
  }
  if (mod) mod.hidden = true;
  templatePreviewCtx = null;
  document.body.style.overflow = '';
}

async function refreshTemplateStrip() {
  const rail = $('template-rail');
  const cntEl = $('template-count');
  if (!rail) return;
  const model = ($('model-select') && $('model-select').value) || activeModel || '';
  if (!model) {
    rail.innerHTML = '<div class="template-rail-empty">请选择模型后查看模板</div>';
    if (cntEl) cntEl.textContent = '0 个模板';
    return;
  }
  try {
    const r = await genpicFetch('/api/templates?' + new URLSearchParams({ model }));
    const j = await r.json().catch(() => ({}));
    const list = (j && Array.isArray(j.data)) ? j.data : [];
    if (!r.ok) {
      rail.innerHTML = '<div class="template-rail-empty">模板加载失败</div>';
      if (cntEl) cntEl.textContent = '0 个模板';
      return;
    }
    if (cntEl) cntEl.textContent = list.length + ' 个模板';
    rail.innerHTML = '';
    if (!list.length) {
      rail.innerHTML = '<div class="template-rail-empty">暂无模板 · 在生成详情中可将成功作品保存为模板</div>';
      updateTemplateMoreVisibility();
      return;
    }
    for (const t of list) {
      const card = document.createElement('div');
      card.className = 'template-card';
      const wrap = document.createElement('div');
      wrap.className = 'template-card-img-wrap';
      const im = document.createElement('img');
      im.className = 'template-card-img';
      im.alt = '';
      const u = String(t.result_image_url || '').trim();
      const thumb = genpicArtifactThumbFromUrl(u) || u;
      im.src = thumb || u;
      genpicBindPreviewImgFallback(im, { url: u, thumb_url: thumb, mime_type: 'image/png' });
      wrap.appendChild(im);
      const hit = document.createElement('button');
      hit.type = 'button';
      hit.className = 'template-card-preview-hit';
      hit.setAttribute('aria-label', '点击预览模板；Ctrl 或 ⌘ 点击在新窗口打开原图');
      hit.innerHTML = TPL_PREVIEW_EYE_SVG + '<span class="tpl-hit-txt">点击预览</span>';
      hit.addEventListener('click', (ev) => {
        ev.preventDefault();
        ev.stopPropagation();
        if (ev.ctrlKey || ev.metaKey) {
          const fu = genpicImgFullSrc({ url: u });
          if (fu) window.open(fu, '_blank', 'noopener,noreferrer');
          return;
        }
        openTemplatePreview(t);
      });
      wrap.appendChild(hit);
      if (t.visibility === 'public') {
        const b = document.createElement('span');
        b.className = 'template-card-badge';
        b.textContent = '公用';
        wrap.appendChild(b);
      } else {
        const b = document.createElement('span');
        b.className = 'template-card-badge template-card-badge-mine';
        b.textContent = '我的';
        wrap.appendChild(b);
      }
      card.appendChild(wrap);
      const title = (t.title || '').trim();
      if (title) {
        const te = document.createElement('div');
        te.className = 'template-card-title';
        te.textContent = title;
        card.appendChild(te);
      }
      const use = document.createElement('button');
      use.type = 'button';
      use.className = 'template-card-use';
      use.textContent = '立即使用';
      use.addEventListener('click', () => { void applyGenpicTemplate(t); });
      card.appendChild(use);
      rail.appendChild(card);
    }
    requestAnimationFrame(() => updateTemplateMoreVisibility());
  } catch (_) {
    rail.innerHTML = '<div class="template-rail-empty">模板加载失败</div>';
    if (cntEl) cntEl.textContent = '0 个模板';
  }
}

async function saveJobAsTemplatePayload(payload, visibility) {
  if (!authUser) {
    alert('请先登录');
    openAuthModal('login');
    return;
  }
  const jobId = String(payload.id || '').trim();
  if (!jobId || jobId.length !== 32) {
    alert('无法保存：缺少有效任务 ID');
    return;
  }
  const refs = payload.reference_images || [];
  const body = {
    job_id: jobId,
    visibility: visibility === 'public' ? 'public' : 'private',
    title: '',
    reference_images: Array.isArray(refs) ? refs : [],
  };
  try {
    const r = await genpicFetch('/api/templates', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    const data = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(data?.error?.message || data?.message || '保存失败');
    showGenpicToast(visibility === 'public' ? '已保存为公用模板' : '已保存到我的模板');
    void refreshTemplateStrip();
  } catch (e) {
    alert(e.message || String(e));
  }
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

/** Single character for the header user chip (38×38); prefer display_name then email local part. */
function userChromeInitial(u) {
  if (!u || !u.email) return '';
  const nick = (u.display_name || '').trim();
  const local = (u.email.split('@')[0] || '').trim();
  const src = nick || local;
  const ch = src ? [...src][0] : '';
  if (!ch) return '?';
  return /[a-z]/i.test(ch) ? ch.toUpperCase() : ch;
}

function updateAuthChrome() {
  const btn = $('btn-auth');
  const em = $('auth-dropdown-email');
  if (!btn) return;
  if (authUser && authUser.email) {
    btn.textContent = userChromeInitial(authUser);
    const nick = (authUser.display_name || '').trim();
    btn.title = nick ? `${nick} · ${authUser.email}` : authUser.email;
    btn.setAttribute('aria-label', `账号菜单 · ${authUser.email}`);
  } else {
    btn.textContent = '登录';
    btn.removeAttribute('title');
    btn.setAttribute('aria-label', '登录');
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
          addImages(images, meta.model || effectiveModelId(), meta.provider || activeProvider, meta.prompt || '');
          saveToHistory(images, meta.model || effectiveModelId(), meta.provider || activeProvider, prompt, jobId, j);
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
  const list0 = genpicImgListSrc(img0);
  if (img0 && list0) {
    const im = document.createElement('img');
    im.className = 'comm-card-img';
    im.alt = '';
    im.loading = 'lazy';
    im.src = genpicImgListSrc(img0);
    genpicBindPreviewImgFallback(im, img0);
    im.style.cursor = 'pointer';
    im.addEventListener('click', () => {
      if (job.id) void openJobDetailFetchById(job.id);
      else openJobDetail(job);
    });
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
  const mkBtn = (label, fn) => {
    const b = document.createElement('button');
    b.type = 'button';
    b.textContent = label;
    b.addEventListener('click', fn);
    return b;
  };
  act.appendChild(mkBtn('复制描述', () => void copyDescriptionWithToast(job.prompt || '')));
  act.appendChild(mkBtn('详情', () => {
    if (job.id) void openJobDetailFetchById(job.id);
    else openJobDetail(job);
  }));
  act.appendChild(mkBtn('创作同款', () => { void applyCreateSimilar(job); }));
  body.appendChild(act);
  card.appendChild(body);
  return card;
}

async function applyCreateSimilar(source) {
  if (!authUser) {
    alert('登录后可查看完整提示词并创作同款');
    openAuthModal('login');
    return;
  }
  const job = normalizeJobDetailPayload(source);
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
  if (p.quality && $('gpt-quality')) $('gpt-quality').value = p.quality;
  if (p.style && $('gpt-style')) $('gpt-style').value = p.style;
  if (p.response_format && $('gpt-format')) $('gpt-format').value = p.response_format;
  if (activeProvider === 'gemini' && $('gem-thinking')) {
    const g = $('gem-thinking');
    if (p.thinking_budget !== undefined && p.thinking_budget !== null && !Number.isNaN(Number(p.thinking_budget))) {
      g.value = String(Number(p.thinking_budget));
      g.dispatchEvent(new Event('input'));
    }
  }
  if (activeProvider === 'wan' && $('wan-edit-type')) {
    $('wan-edit-type').value = p.wan_edit_type || '';
    $('wan-edit-type').dispatchEvent(new Event('change'));
    const bboxSec = $('wan-bbox-section');
    if (bboxSec) bboxSec.style.display = ($('wan-edit-type').value === 'inpaint') ? 'block' : 'none';
    const list = $('wan-bbox-list');
    if (list && Array.isArray(p.wan_bbox_list) && p.wan_bbox_list.length) {
      list.innerHTML = '';
      for (const b of p.wan_bbox_list) {
        addWanBboxRow();
        const rows = list.querySelectorAll('.bbox-row');
        const row = rows[rows.length - 1];
        const nums = row.querySelectorAll('input[type=number]');
        if (nums[0]) nums[0].value = b.x1 != null ? String(b.x1) : '';
        if (nums[1]) nums[1].value = b.y1 != null ? String(b.y1) : '';
        if (nums[2]) nums[2].value = b.x2 != null ? String(b.x2) : '';
        if (nums[3]) nums[3].value = b.y2 != null ? String(b.y2) : '';
      }
    }
    if ($('wan-thinking')) $('wan-thinking').checked = !!p.thinking_mode;
  }
  await loadReferenceImagesForSimilar(source.reference_images || p.reference_images);
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
const galleryCardExtras = new WeakMap();

function setActiveModel(modelId, provider) {
  activeModel = modelId;
  activeProvider = provider;

  const ms = $('model-select');
  if (ms && ms.value !== modelId) ms.value = modelId;

  activeVendorId = provider;
  $('btn-community')?.classList.remove('active');
  document.querySelectorAll('#vendor-rail .vendor-btn').forEach((b) => {
    b.classList.toggle('active', b.dataset.vendor === provider);
  });

  ['openai', 'gemini', 'wan'].forEach((p) => {
    $('params-' + p)?.classList.toggle('show', p === provider);
    $('more-extra-' + p)?.classList.toggle('show', p === provider);
  });

  // Show thinking field only for thinking-capable Gemini models.
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
  void refreshTemplateStrip();
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

function syncVendorRailToggleUI() {
  const app = $('app');
  const btn = $('btn-vendor-rail-toggle');
  const icon = btn?.querySelector('.vendor-rail-toggle-icon');
  if (!btn || !app) return;
  const hidden = app.classList.contains('vendor-rail-hidden');
  btn.title = hidden ? '展开厂商栏' : '收起厂商栏';
  btn.setAttribute('aria-expanded', hidden ? 'false' : 'true');
  if (icon) icon.classList.toggle('vendor-rail-toggle-icon--collapsed', hidden);
}

function applyVendorRailHiddenFromStorage() {
  const app = $('app');
  if (!app) return;
  if (load('vendor-rail-hidden', '') === '1') app.classList.add('vendor-rail-hidden');
  else app.classList.remove('vendor-rail-hidden');
  syncVendorRailToggleUI();
}

let uiCatalog = null;

/** If stored active-model is a server-only 4K route id, map back to the public catalog key. */
function scrubStoredModelIfGemini4KRoute(catalog, modelId) {
  const raw = catalog?.gemini_image_size_4k_model_map;
  if (!raw || typeof raw !== 'object' || !modelId) return modelId;
  const mid = String(modelId).trim();
  for (const [k, v] of Object.entries(raw)) {
    if (String(v).trim() === mid) return String(k).trim();
  }
  return modelId;
}

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
  const rail = $('vendor-rail-buttons');
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
}

function getModelsForVendor(vid) {
  const v = (uiCatalog?.vendors || []).find((x) => x.id === vid);
  return Array.isArray(v?.models) ? v.models : [];
}

function selectVendor(vid) {
  const sidebar = $('sidebar');
  const genArea = $('generate-area');
  const btnComm = $('btn-community');
  if (vid === 'community') {
    activeVendorId = 'community';
    save('active-vendor', 'community');
    document.querySelectorAll('#vendor-rail .vendor-btn').forEach((b) => b.classList.remove('active'));
    btnComm?.classList.add('active');
    if (sidebar) sidebar.hidden = true;
    if (genArea) genArea.dataset.view = 'community';
    void loadCommunityFeed(true);
    return;
  }
  btnComm?.classList.remove('active');
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
  let sm = load('active-model', null);
  if (sm) sm = scrubStoredModelIfGemini4KRoute(uiCatalog, sm);
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

applyVendorRailHiddenFromStorage();

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
  .then(() => refreshTemplateStrip())
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
  if (e.key !== 'Escape') return;
  if (!$('template-preview-modal')?.hidden) {
    closeTemplatePreview();
    return;
  }
  if (!$('job-detail-modal')?.hidden) {
    closeJobDetail();
    return;
  }
  if (!$('hist-drawer')?.hidden) {
    closeHistDrawer();
    return;
  }
  closeCredDialog();
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

$('btn-community')?.addEventListener('click', () => selectVendor('community'));

$('btn-vendor-rail-toggle')?.addEventListener('click', () => {
  $('app')?.classList.toggle('vendor-rail-hidden');
  const hidden = $('app')?.classList.contains('vendor-rail-hidden');
  save('vendor-rail-hidden', hidden ? '1' : '');
  syncVendorRailToggleUI();
});

$('btn-hist-drawer')?.addEventListener('click', () => openHistDrawer());
$('hist-drawer-close')?.addEventListener('click', () => closeHistDrawer());
$('hist-drawer-backdrop')?.addEventListener('click', () => closeHistDrawer());

$('job-detail-close')?.addEventListener('click', () => closeJobDetail());
$('job-detail-modal')?.addEventListener('click', (e) => {
  if (e.target.id === 'job-detail-modal') closeJobDetail();
});
function openJobDetailImageNewTab() {
  const u = $('job-detail-img')?.getAttribute('data-full-src');
  if (u) window.open(u, '_blank', 'noopener,noreferrer');
}
$('job-detail-img-wrap')?.addEventListener('click', (e) => {
  e.stopPropagation();
  openJobDetailImageNewTab();
});
$('job-detail-img-wrap')?.addEventListener('keydown', (e) => {
  if (e.key !== 'Enter' && e.key !== ' ') return;
  e.preventDefault();
  e.stopPropagation();
  openJobDetailImageNewTab();
});
$('job-detail-copy-prompt')?.addEventListener('click', () => {
  if (!jobDetailCtx) return;
  void copyDescriptionWithToast(jobDetailCtx.prompt || '');
});
$('job-detail-create-similar')?.addEventListener('click', () => {
  if (!jobDetailCtx) return;
  closeJobDetail();
  void applyCreateSimilar(jobDetailCtx);
});
$('job-detail-save-template')?.addEventListener('click', () => {
  if (!jobDetailCtx) return;
  void saveJobAsTemplatePayload(jobDetailCtx, 'private');
});

$('template-rail-next')?.addEventListener('click', () => {
  const rail = $('template-rail');
  if (rail) rail.scrollBy({ left: 200, behavior: 'smooth' });
});
$('btn-template-more')?.addEventListener('click', () => {
  const rail = $('template-rail');
  if (rail) rail.scrollBy({ left: 260, behavior: 'smooth' });
});
$('template-rail')?.addEventListener('scroll', () => updateTemplateMoreVisibility(), { passive: true });

$('template-preview-close')?.addEventListener('click', () => closeTemplatePreview());
$('template-preview-modal')?.addEventListener('click', (e) => {
  if (e.target.id === 'template-preview-modal') closeTemplatePreview();
  else if (e.target.closest('#template-preview-img-wrap')) {
    const u = $('template-preview-img')?.getAttribute('data-full-src');
    if (u) window.open(u, '_blank', 'noopener,noreferrer');
  }
});
$('template-preview-img-wrap')?.addEventListener('keydown', (e) => {
  if (e.key !== 'Enter' && e.key !== ' ') return;
  e.preventDefault();
  e.stopPropagation();
  const u = $('template-preview-img')?.getAttribute('data-full-src');
  if (u) window.open(u, '_blank', 'noopener,noreferrer');
});
$('template-preview-copy')?.addEventListener('click', () => {
  if (!templatePreviewCtx) return;
  void copyDescriptionWithToast(templatePreviewCtx.prompt || '');
});
$('template-preview-apply')?.addEventListener('click', () => {
  if (!templatePreviewCtx) return;
  const t = templatePreviewCtx;
  closeTemplatePreview();
  void applyGenpicTemplate(t);
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

function clipboardDataHasPlainOrHtmlText(dataTransfer) {
  if (!dataTransfer?.items) return false;
  for (const it of dataTransfer.items) {
    if (it.type === 'text/plain' || it.type === 'text/html') return true;
  }
  return false;
}

function collectClipboardImageFiles(dataTransfer) {
  const out = [];
  if (!dataTransfer) return out;
  if (dataTransfer.items?.length) {
    for (const it of dataTransfer.items) {
      if (it.kind === 'file' && it.type.startsWith('image/')) {
        const f = it.getAsFile();
        if (f) out.push(f);
      }
    }
  }
  if (!out.length && dataTransfer.files?.length) {
    for (const f of dataTransfer.files) {
      if (f.type.startsWith('image/')) out.push(f);
    }
  }
  return out;
}

/** True when paste-to-reference should run (not in modals / text fields where paste means text). */
function shouldPasteClipboardImagesToRefs(activeEl) {
  if (!activeEl) return true;
  if (activeEl.closest?.('#job-detail-modal, #auth-modal, #cred-panel, #hist-drawer, #template-preview-modal, #privacy-modal')) {
    return false;
  }
  if (activeEl.isContentEditable || activeEl.closest?.('[contenteditable="true"]')) {
    return false;
  }
  const tag = activeEl.tagName;
  if (tag === 'SELECT') return false;
  if (tag === 'TEXTAREA') return activeEl.id === 'prompt';
  if (tag === 'INPUT') {
    const t = (activeEl.type || '').toLowerCase();
    if (['text', 'url', 'password', 'email', 'search', 'tel'].includes(t)) return false;
  }
  return true;
}

function onDocumentPasteReferenceImages(e) {
  const cd = e.clipboardData;
  if (!cd) return;
  const el = document.activeElement;
  if (!shouldPasteClipboardImagesToRefs(el)) return;
  const files = collectClipboardImageFiles(cd);
  if (!files.length) return;
  if (el?.id === 'prompt' && clipboardDataHasPlainOrHtmlText(cd)) return;
  e.preventDefault();
  for (const f of files) {
    addReferenceFromFile(f);
  }
  showGenpicToast('已从剪贴板添加参考图');
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

  document.addEventListener('paste', onDocumentPasteReferenceImages);
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
function addImages(images, model, provider, prompt = '') {
  $('empty').style.display = 'none';
  const gallery = $('gallery');
  const pr = (prompt || '').trim();
  let bodySnap = null;
  try { bodySnap = buildBody(); } catch (_) { bodySnap = null; }
  const refSnap = captureRefSnapshotForHistory();

  images.forEach(img => {
    const card = document.createElement('div');
    card.className = 'img-card';
    galleryCardExtras.set(card, { body: bodySnap, refs: refSnap });

    const image = document.createElement('img');
    const listSrc = genpicImgListSrc(img);
    if (listSrc) { image.src = listSrc; }
    genpicBindPreviewImgFallback(image, img);
    image.alt = model;
    image.loading = 'lazy';
    image.style.cursor = 'pointer';
    image.addEventListener('click', () => {
      const gx = galleryCardExtras.get(card);
      openJobDetail({
        model,
        provider,
        prompt: pr,
        status: 'succeeded',
        images: [{ url: img.url, thumb_url: img.thumb_url, mime_type: img.mime_type, b64_json: img.b64_json }],
        params: gx?.body || {},
        created_at: Math.floor(Date.now() / 1000),
      });
    });
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
      a.href = genpicImgFullSrc(img);
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

    const act = document.createElement('div');
    act.className = 'card-actions';
    const mkBtn = (label, fn) => {
      const b = document.createElement('button');
      b.type = 'button';
      b.textContent = label;
      b.addEventListener('click', fn);
      return b;
    };
    act.appendChild(mkBtn('复制描述', () => void copyDescriptionWithToast(pr)));
    act.appendChild(mkBtn('详情', () => {
      const gx = galleryCardExtras.get(card);
      openJobDetail({
        model,
        provider,
        prompt: pr,
        status: 'succeeded',
        images: [{ url: img.url, thumb_url: img.thumb_url, mime_type: img.mime_type, b64_json: img.b64_json }],
        params: gx?.body || {},
        created_at: Math.floor(Date.now() / 1000),
      });
    }));
    act.appendChild(mkBtn('创作同款', () => {
      const gx = galleryCardExtras.get(card);
      void applyCreateSimilar({
        model,
        provider,
        prompt: pr,
        params: gx?.body || { model },
        reference_images: gx?.refs,
      });
    }));
    card.appendChild(act);

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
    addImages(images, effectiveModelId(), activeProvider, prompt);
    saveToHistory(images, effectiveModelId(), activeProvider, prompt, null, null, captureRefSnapshotForHistory());
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
      thumb_url: img.thumb_url || undefined,
      mime_type: img.mime_type || undefined,
      revised_prompt: img.revised_prompt || undefined,
      b64_json: (img.b64_json && img.b64_json.length <= MAX_B64) ? img.b64_json : undefined,
    })),
  };
  if (entry.reference_images && Array.isArray(entry.reference_images)) {
    safe.reference_images = entry.reference_images.map((r) => {
      const url = String(r.url || '').trim();
      if (url) return { mime_type: r.mime_type || 'image/png', url };
      const b64 = r.b64_json && r.b64_json.length <= MAX_B64 ? r.b64_json : undefined;
      return { mime_type: r.mime_type, b64_json: b64 };
    }).filter((r) => r.url || r.b64_json);
  }
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
    thumb_url: img.thumb_url,
    b64_json: img.b64_json,
    mime_type: img.mime_type,
    revised_prompt: img.revised_prompt,
  }));
  const ts = (typeof job.created_at === 'number' ? job.created_at : 0) * 1000;
  let errorMessage = '';
  if (job.error && job.error.message) errorMessage = job.error.message;
  const out = {
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
    params: job.params,
  };
  if (typeof job.created_at === 'number') out.created_at = job.created_at;
  if (job.started_at != null) out.started_at = job.started_at;
  if (job.finished_at != null) out.finished_at = job.finished_at;
  if (job.processing_ms != null) out.processing_ms = job.processing_ms;
  if (job.tokens_used != null) out.tokens_used = job.tokens_used;
  if (job.upstream_request_id) out.upstream_request_id = job.upstream_request_id;
  const ra = job.params && Array.isArray(job.params.reference_assets) ? job.params.reference_assets : null;
  if (ra && ra.length) {
    out.reference_images = ra.map((a) => ({
      mime_type: a.mime_type || 'image/png',
      url: String(a.url || '').trim(),
    })).filter((x) => x.url);
  }
  return out;
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

function captureRefSnapshotForHistory() {
  if (!referenceEntries.length) return undefined;
  return referenceEntries.map((e) => ({ mime_type: e.mime, b64_json: e.b64 }));
}

function saveToHistory(images, model, provider, prompt, serverJobId, jobPoll, refSnapOverride) {
  if (!images || !images.length) return;
  const id = (serverJobId && String(serverJobId).trim())
    ? String(serverJobId).trim()
    : (Date.now() + '-' + Math.random().toString(36).slice(2, 7));
  const entry = {
    id,
    ts: Date.now(),
    model,
    provider,
    prompt,
    source: serverJobId ? 'server' : 'local',
    images: images.map(img => ({
      url: img.url,
      thumb_url: img.thumb_url,
      b64_json: img.b64_json,
      mime_type: img.mime_type,
      revised_prompt: img.revised_prompt,
    })),
  };
  if (jobPoll) {
    if (jobPoll.params) entry.params = jobPoll.params;
    if (jobPoll.tokens_used != null) entry.tokens_used = jobPoll.tokens_used;
    if (jobPoll.upstream_request_id) entry.upstream_request_id = jobPoll.upstream_request_id;
    if (jobPoll.processing_ms != null) entry.processing_ms = jobPoll.processing_ms;
    if (jobPoll.started_at != null) entry.started_at = jobPoll.started_at;
    if (jobPoll.finished_at != null) entry.finished_at = jobPoll.finished_at;
    if (jobPoll.created_at != null) entry.created_at = jobPoll.created_at;
  }
  const snap = refSnapOverride !== undefined ? refSnapOverride : captureRefSnapshotForHistory();
  if (snap && snap.length) entry.reference_images = snap;
  histPush(entry);
  void mergeAndPersistHistory({ reset: true }).catch(() => {});
}

function fmtHistTime(ts) {
  try { return new Date(ts).toLocaleString(); } catch { return ''; }
}

function openHistEntryDetail(entry) {
  if (!entry) return;
  const id = entry.id;
  if (entry.source === 'server' && /^[0-9a-f]{32}$/i.test(String(id))) {
    void openJobDetailFetchById(id);
    return;
  }
  openJobDetail(histEntryToDetailPayload(entry));
}

function buildHistListImageItem(imgRec, entry, itemClassExtra) {
  const item = document.createElement('div');
  item.className = 'hist-img-item' + (itemClassExtra ? ' ' + itemClassExtra : '');
  const el = document.createElement('img');
  el.alt = '';
  el.loading = 'lazy';
  if (imgRec.b64_json) {
    el.src = 'data:' + (imgRec.mime_type || 'image/png') + ';base64,' + imgRec.b64_json;
  } else {
    const ls = genpicImgListSrc(imgRec);
    if (ls) el.src = ls;
  }
  genpicBindPreviewImgFallback(el, imgRec);
  el.style.cursor = 'pointer';
  el.addEventListener('click', () => openHistEntryDetail(entry));
  item.appendChild(el);
  const a = document.createElement('a');
  if (imgRec.b64_json) {
    a.href = genpicImgFullSrc(imgRec);
    a.download = 'genpic-' + entry.ts + '.png';
    a.textContent = '下载';
  } else if (imgRec.url) {
    a.href = imgRec.url;
    a.target = '_blank';
    a.rel = 'noopener';
    a.textContent = '原图 ↗';
  }
  if (a.href) item.appendChild(a);
  return item;
}

/** History list: prompt longer than this shows 展开 / 收起. */
const HIST_PROMPT_EXPAND_THRESHOLD = 100;

function appendHistExpandablePrompt(parent, promptText, compact) {
  if (!parent || !promptText) return;
  const wrap = document.createElement('div');
  wrap.className = 'hist-prompt-expand' + (compact ? ' hist-prompt-expand--compact' : '');
  const inner = document.createElement('div');
  inner.className = 'hist-prompt-inner';
  inner.textContent = promptText;
  const long = promptText.length > HIST_PROMPT_EXPAND_THRESHOLD;
  if (long) inner.classList.add('hist-prompt-inner--clamped');

  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'hist-prompt-expand-btn';
  btn.textContent = '展开';
  btn.hidden = !long;
  btn.addEventListener('click', (e) => {
    e.preventDefault();
    e.stopPropagation();
    const on = wrap.dataset.expanded === '1';
    if (on) {
      wrap.removeAttribute('data-expanded');
      btn.textContent = '展开';
    } else {
      wrap.dataset.expanded = '1';
      btn.textContent = '收起';
    }
  });

  wrap.appendChild(inner);
  wrap.appendChild(btn);
  parent.appendChild(wrap);
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
    const fullModel = (entry.model || '').trim();
    modelSpan.textContent = fullModel;
    if (fullModel) modelSpan.title = fullModel;

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

    const imgs = entry.images || [];
    const isCompact = entry.status === 'succeeded' && imgs.length > 0;
    if (isCompact) card.classList.add('hist-entry--compact');

    const appendPromptSection = (parent, compact) => {
      if (entry.prompt) {
        appendHistExpandablePrompt(parent, entry.prompt, compact);
      } else if (entry.source === 'server') {
        const p = document.createElement('div');
        p.className = compact ? 'hist-row-prompt hist-prompt-locked' : 'hist-prompt hist-prompt-locked';
        p.textContent = '登录后可见完整提示词';
        parent.appendChild(p);
      }
    };

    if (isCompact) {
      const mainRow = document.createElement('div');
      mainRow.className = 'hist-main-row';
      const thumbCol = document.createElement('div');
      thumbCol.className = 'hist-row-thumb';
      thumbCol.appendChild(buildHistListImageItem(imgs[0], entry, 'hist-img-item--inline'));
      mainRow.appendChild(thumbCol);
      const promptWrap = document.createElement('div');
      promptWrap.className = 'hist-row-prompt-wrap';
      appendPromptSection(promptWrap, true);
      mainRow.appendChild(promptWrap);
      card.appendChild(mainRow);
      if (imgs.length > 1) {
        const extra = document.createElement('div');
        extra.className = 'hist-images hist-images--extra';
        for (let i = 1; i < imgs.length; i++) {
          extra.appendChild(buildHistListImageItem(imgs[i], entry, 'hist-img-item--extra'));
        }
        card.appendChild(extra);
      }
    } else {
      appendPromptSection(card, false);
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
      imgs.forEach((imgRec) => {
        imgsEl.appendChild(buildHistListImageItem(imgRec, entry, ''));
      });
      card.appendChild(imgsEl);
    }

    const actRow = document.createElement('div');
    actRow.className = 'card-actions';
    const mkH = (label, fn) => {
      const b = document.createElement('button');
      b.type = 'button';
      b.textContent = label;
      b.addEventListener('click', fn);
      return b;
    };
    actRow.appendChild(mkH('复制描述', () => void copyDescriptionWithToast(entry.prompt || '')));
    actRow.appendChild(mkH('详情', () => openHistEntryDetail(entry)));
    actRow.appendChild(mkH('创作同款', () => { void applyCreateSimilar(histEntryToDetailPayload(entry)); }));
    const hid = String(entry.id || '').trim();
    if (authUser && entry.status === 'succeeded' && /^[a-f0-9]{32}$/i.test(hid)) {
      actRow.appendChild(mkH('存为模板', () => void saveJobAsTemplatePayload(histEntryToDetailPayload(entry), 'private')));
    }
    card.appendChild(actRow);

    listEl.appendChild(card);
  });
  updateHistLoadMoreUI();
}

function openAuthModal(tab) {
  const modal = $('auth-modal');
  if (!modal) return;
  closeHistDrawer();
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

/** 在邮箱/密码框内按 Enter 提交（未使用 <form> 时部分浏览器/输入法下用户会误以为无响应） */
$('auth-modal')?.addEventListener('keydown', (e) => {
  if (e.key !== 'Enter') return;
  const t = e.target;
  if (!t || t.tagName !== 'INPUT') return;
  const id = t.getAttribute('id') || '';
  if (!id.startsWith('auth-')) return;
  e.preventDefault();
  $('auth-modal-submit')?.click();
});

async function submitAuthModal() {
  const submitBtn = $('auth-modal-submit');
  if (submitBtn?.disabled) return;
  const activeTab = document.querySelector('#auth-tabs .modal-tab.active');
  const mode = activeTab?.dataset.tab === 'register' ? 'register' : 'login';
  const errEl = $('auth-modal-error');
  if (errEl) errEl.style.display = 'none';
  const prevLabel = submitBtn ? submitBtn.textContent : '';
  if (submitBtn) {
    submitBtn.disabled = true;
    submitBtn.textContent = '处理中…';
  }
  try {
    if (mode === 'login') {
      const email = ($('auth-email')?.value ?? '').trim();
      const password = $('auth-password')?.value ?? '';
      const r = await genpicFetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password }),
      });
      const data = await r.json().catch(() => ({}));
      if (!r.ok) throw new Error(data?.error?.message || data?.message || '登录失败');
    } else {
      const email = ($('auth-reg-email')?.value ?? '').trim();
      const password = $('auth-reg-password')?.value ?? '';
      const display_name = ($('auth-reg-name')?.value ?? '').trim();
      const r = await genpicFetch('/api/auth/register', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password, display_name }),
      });
      const data = await r.json().catch(() => ({}));
      if (!r.ok) throw new Error(data?.error?.message || data?.message || '注册失败');
    }
    const modal = $('auth-modal');
    if (modal) modal.hidden = true;
    await refreshAuthUser();
    await refreshTemplateStrip();
    await mergeAndPersistHistory({ reset: true });
    renderHistoryPanel();
  } catch (err) {
    if (errEl) {
      errEl.textContent = err.message || String(err);
      errEl.style.display = 'block';
    }
  } finally {
    if (submitBtn) {
      submitBtn.disabled = false;
      submitBtn.textContent = prevLabel;
    }
  }
}

$('auth-modal-submit')?.addEventListener('click', () => {
  void submitAuthModal();
});

$('btn-auth-logout')?.addEventListener('click', async () => {
  closeAuthDropdown();
  try {
    await genpicFetch('/api/auth/logout', { method: 'POST' });
  } catch (_) {}
  authUser = null;
  updateAuthChrome();
  void refreshTemplateStrip();
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
