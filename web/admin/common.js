/* global AdminUsers, AdminTemplates, AdminStats */
(function () {
  const PANELS = {
    users: { title: '用户列表', init: () => AdminUsers && AdminUsers.init() },
    templates: { title: '模板列表 · 公用 / 私有', init: () => AdminTemplates && AdminTemplates.init() },
    stats: { title: '模型稳定性 · 统计与图表', init: () => AdminStats && AdminStats.init() },
  };

  const state = {
    me: null,
    panel: 'users',
    panelInited: {},
  };

  function $(id) {
    return document.getElementById(id);
  }

  function escapeHtml(s) {
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;');
  }

  function escapeAttr(s) {
    return escapeHtml(s).replace(/"/g, '&quot;');
  }

  function fmtBytes(n) {
    const v = Number(n);
    if (!Number.isFinite(v) || v < 0) return '—';
    if (v < 1024) return v + ' B';
    if (v < 1024 * 1024) return (v / 1024).toFixed(1) + ' KB';
    if (v < 1024 * 1024 * 1024) return (v / (1024 * 1024)).toFixed(2) + ' MB';
    return (v / (1024 * 1024 * 1024)).toFixed(2) + ' GB';
  }

  function bytesFromInput(value, unit) {
    const n = Number(value);
    if (!Number.isFinite(n)) return null;
    switch (unit) {
      case 'GB':
        return Math.round(n * 1024 * 1024 * 1024);
      case 'MB':
        return Math.round(n * 1024 * 1024);
      case 'KB':
        return Math.round(n * 1024);
      default:
        return Math.round(n);
    }
  }

  async function apiFetch(path, opts) {
    const o = opts || {};
    const headers = Object.assign({ Accept: 'application/json' }, o.headers || {});
    if (o.body && typeof o.body === 'string' && !headers['Content-Type']) {
      headers['Content-Type'] = 'application/json';
    }
    const res = await fetch(path, Object.assign({}, o, { credentials: 'include', headers }));
    let data = null;
    const ct = res.headers.get('content-type') || '';
    if (ct.includes('application/json')) {
      try {
        data = await res.json();
      } catch (_) {}
    }
    return { res, data };
  }

  function errMessage(data) {
    if (!data) return '请求失败';
    const e = data.error || data;
    if (typeof e === 'string') return e;
    if (e.message) return e.message;
    if (e.code) return String(e.code);
    return '请求失败';
  }

  function showBanner(text, kind) {
    const banner = $('banner');
    if (!banner) return;
    if (!text) {
      banner.hidden = true;
      return;
    }
    banner.textContent = text;
    banner.classList.toggle('warn', kind === 'warn');
    banner.classList.toggle('ok', kind === 'ok');
    banner.hidden = false;
  }

  function setAdminEnabled(enabled) {
    document.querySelectorAll('.admin-nav a').forEach((a) => {
      a.style.pointerEvents = enabled ? '' : 'none';
      a.style.opacity = enabled ? '' : '0.5';
    });
  }

  function switchPanel(name) {
    const cfg = PANELS[name];
    if (!cfg) name = 'users';
    state.panel = name;
    document.querySelectorAll('.admin-nav a').forEach((a) => {
      a.classList.toggle('active', a.getAttribute('data-panel') === name);
    });
    document.querySelectorAll('.admin-panel').forEach((p) => {
      p.classList.toggle('active', p.id === 'panel-' + name);
    });
    const title = $('workspace-title');
    if (title) title.textContent = PANELS[name].title;
    if (location.hash !== '#' + name) {
      history.replaceState(null, '', '#' + name);
    }
    if (!state.panelInited[name] && state.me && state.me.is_admin) {
      state.panelInited[name] = true;
      PANELS[name].init();
    }
  }

  function parseHash() {
    const h = (location.hash || '').replace(/^#/, '').trim();
    return PANELS[h] ? h : 'users';
  }

  async function loadMe() {
    const { res, data } = await apiFetch('/api/auth/me');
    if (res.status === 401) {
      state.me = null;
      showBanner('未登录：请先在主站使用管理员账号登录，再回到本页。', 'warn');
      setAdminEnabled(false);
      return;
    }
    if (!res.ok) {
      state.me = null;
      showBanner(errMessage(data), 'warn');
      setAdminEnabled(false);
      return;
    }
    state.me = data;
    if (!data.is_admin) {
      showBanner('当前账号不是管理员（邮箱未在 admin_emails 列表中）。', 'warn');
      setAdminEnabled(false);
      return;
    }
    showBanner('已以管理员身份登录：' + (data.email || data.id), 'ok');
    setAdminEnabled(true);
    switchPanel(parseHash());
  }

  document.querySelectorAll('.admin-nav a').forEach((a) => {
    a.addEventListener('click', (e) => {
      e.preventDefault();
      const p = a.getAttribute('data-panel');
      if (p) switchPanel(p);
    });
  });

  window.addEventListener('hashchange', () => {
    if (state.me && state.me.is_admin) switchPanel(parseHash());
  });

  window.AdminApp = {
    get me() {
      return state.me;
    },
    isAdmin() {
      return !!(state.me && state.me.is_admin);
    },
    apiFetch,
    errMessage,
    escapeHtml,
    escapeAttr,
    fmtBytes,
    bytesFromInput,
    showBanner,
    switchPanel,
    refreshPanel(name) {
      state.panelInited[name] = false;
      if (state.panel === name) {
        state.panelInited[name] = true;
        PANELS[name].init();
      }
    },
  };

  loadMe();
})();
