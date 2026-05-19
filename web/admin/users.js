/* global AdminApp */
const AdminUsers = (function () {
  let offset = 0;
  const limit = 30;
  let total = 0;
  let modalUser = null;

  function $(id) {
    return document.getElementById(id);
  }

  function setMsg(text, ok) {
    const el = $('users-msg');
    if (!el) return;
    el.textContent = text || '';
    el.className = 'admin-msg' + (text ? (ok ? ' ok' : ' err') : '');
  }

  async function loadList(resetOffset) {
    if (!AdminApp.isAdmin()) return;
    if (resetOffset) offset = 0;
    setMsg('');
    const q = new URLSearchParams();
    q.set('limit', String(limit));
    q.set('offset', String(offset));
    const search = ($('users-q') && $('users-q').value.trim()) || '';
    if (search) q.set('q', search);

    const { res, data } = await AdminApp.apiFetch('/api/admin/users/storage?' + q.toString());
    if (!res.ok) {
      setMsg(AdminApp.errMessage(data), false);
      const tb = $('users-tbody');
      if (tb) tb.innerHTML = '';
      return;
    }
    total = typeof data.total === 'number' ? data.total : 0;
    renderRows(data.data || []);
    const meta = $('users-pager-meta');
    if (meta) {
      meta.textContent =
        '第 ' +
        (total === 0 ? 0 : offset + 1) +
        '–' +
        Math.min(offset + limit, total) +
        ' 条，共 ' +
        total +
        ' 条';
    }
    if ($('btn-users-prev')) $('btn-users-prev').disabled = offset <= 0;
    if ($('btn-users-next')) $('btn-users-next').disabled = offset + limit >= total;
  }

  function renderRows(rows) {
    const tb = $('users-tbody');
    if (!tb) return;
    tb.innerHTML = '';
    if (!rows.length) {
      const tr = document.createElement('tr');
      tr.innerHTML = '<td colspan="7" style="padding:16px;color:var(--muted)">暂无用户</td>';
      tb.appendChild(tr);
      return;
    }
    for (const row of rows) {
      const used = row.used_bytes || 0;
      const quota = row.quota_bytes || 0;
      const rem = row.remaining_bytes != null ? row.remaining_bytes : Math.max(0, quota - used);
      const warn = used > 0 && rem === 0;
      const tr = document.createElement('tr');
      if (warn) tr.className = 'row-warn';
      const uid = row.user_id || '';
      const email = row.email || '';
      tr.innerHTML =
        '<td>' +
        AdminApp.escapeHtml(email) +
        '</td>' +
        '<td class="mono" title="' +
        AdminApp.escapeAttr(uid) +
        '">' +
        AdminApp.escapeHtml(uid) +
        '</td>' +
        '<td>' +
        AdminApp.escapeHtml(row.display_name || '—') +
        '</td>' +
        '<td>' +
        AdminApp.fmtBytes(used) +
        '</td>' +
        '<td>' +
        AdminApp.fmtBytes(quota) +
        '</td>' +
        '<td' +
        (warn ? ' style="color:#f87171;font-weight:600"' : '') +
        '>' +
        AdminApp.fmtBytes(rem) +
        '</td>' +
        '<td style="white-space:nowrap">' +
        '<button type="button" class="admin-btn secondary small btn-reset-pw" data-user-id="' +
        AdminApp.escapeAttr(uid) +
        '" data-email="' +
        AdminApp.escapeAttr(email) +
        '">重置密码</button> ' +
        '<button type="button" class="admin-btn secondary small btn-adjust-quota" data-user-id="' +
        AdminApp.escapeAttr(uid) +
        '" data-email="' +
        AdminApp.escapeAttr(email) +
        '" data-quota="' +
        quota +
        '">调整配额</button>' +
        '</td>';
      tb.appendChild(tr);
    }
    tb.querySelectorAll('.btn-reset-pw').forEach((btn) => {
      btn.addEventListener('click', () => openResetModal(btn.dataset.userId, btn.dataset.email));
    });
    tb.querySelectorAll('.btn-adjust-quota').forEach((btn) => {
      btn.addEventListener('click', () =>
        openQuotaModal(btn.dataset.userId, btn.dataset.email, Number(btn.dataset.quota))
      );
    });
  }

  function openModal(backdropId) {
    const el = $(backdropId);
    if (el) el.hidden = false;
  }

  function closeModal(backdropId) {
    const el = $(backdropId);
    if (el) el.hidden = true;
  }

  function openResetModal(userId, email) {
    modalUser = { userId, email };
    if ($('reset-modal-email')) $('reset-modal-email').textContent = email || userId;
    if ($('reset-modal-password')) $('reset-modal-password').value = '';
    if ($('reset-modal-msg')) $('reset-modal-msg').textContent = '';
    openModal('backdrop-reset-password');
  }

  function openQuotaModal(userId, email, quotaBytes) {
    modalUser = { userId, email, quotaBytes };
    if ($('quota-modal-email')) $('quota-modal-email').textContent = email || userId;
    if ($('quota-modal-current')) $('quota-modal-current').textContent = AdminApp.fmtBytes(quotaBytes);
    if ($('quota-mode-set')) $('quota-mode-set').checked = true;
    if ($('quota-value')) $('quota-value').value = String(Math.round(quotaBytes / (1024 * 1024)) || 512);
    if ($('quota-unit')) $('quota-unit').value = 'MB';
    if ($('quota-delta-value')) $('quota-delta-value').value = '100';
    if ($('quota-delta-unit')) $('quota-delta-unit').value = 'MB';
    if ($('quota-modal-msg')) $('quota-modal-msg').textContent = '';
    toggleQuotaFields();
    openModal('backdrop-adjust-quota');
  }

  function toggleQuotaFields() {
    const setMode = $('quota-mode-set') && $('quota-mode-set').checked;
    const setWrap = $('quota-set-fields');
    const deltaWrap = $('quota-delta-fields');
    if (setWrap) setWrap.style.display = setMode ? '' : 'none';
    if (deltaWrap) deltaWrap.style.display = setMode ? 'none' : '';
  }

  async function submitReset() {
    const msg = $('reset-modal-msg');
    if (msg) {
      msg.textContent = '';
      msg.className = 'admin-msg';
    }
    const pw = ($('reset-modal-password') && $('reset-modal-password').value) || '';
    if (!modalUser || !modalUser.userId) return;
    if (pw.length < 8) {
      if (msg) {
        msg.textContent = '新密码至少 8 位';
        msg.classList.add('err');
      }
      return;
    }
    const { res, data } = await AdminApp.apiFetch('/api/admin/users/reset-password', {
      method: 'POST',
      body: JSON.stringify({ user_id: modalUser.userId, new_password: pw }),
    });
    if (!res.ok) {
      if (msg) {
        msg.textContent = AdminApp.errMessage(data);
        msg.classList.add('err');
      }
      return;
    }
    if (msg) {
      msg.textContent = '已重置：' + (data.email || data.user_id);
      msg.classList.add('ok');
    }
    setTimeout(() => closeModal('backdrop-reset-password'), 800);
  }

  async function submitQuota() {
    const msg = $('quota-modal-msg');
    if (msg) {
      msg.textContent = '';
      msg.className = 'admin-msg';
    }
    if (!modalUser || !modalUser.userId) return;
    const setMode = $('quota-mode-set') && $('quota-mode-set').checked;
    let body = { user_id: modalUser.userId };
    if (setMode) {
      const bytes = AdminApp.bytesFromInput(
        $('quota-value') && $('quota-value').value,
        ($('quota-unit') && $('quota-unit').value) || 'MB'
      );
      if (bytes == null || bytes < 0) {
        if (msg) {
          msg.textContent = '请输入有效的配额数值';
          msg.classList.add('err');
        }
        return;
      }
      body.quota_bytes = bytes;
    } else {
      const delta = AdminApp.bytesFromInput(
        $('quota-delta-value') && $('quota-delta-value').value,
        ($('quota-delta-unit') && $('quota-delta-unit').value) || 'MB'
      );
      if (delta == null) {
        if (msg) {
          msg.textContent = '请输入有效的增减数值';
          msg.classList.add('err');
        }
        return;
      }
      body.delta_bytes = delta;
    }
    const { res, data } = await AdminApp.apiFetch('/api/admin/users/storage', {
      method: 'PATCH',
      body: JSON.stringify(body),
    });
    if (!res.ok) {
      if (msg) {
        msg.textContent = AdminApp.errMessage(data);
        msg.classList.add('err');
      }
      return;
    }
    closeModal('backdrop-adjust-quota');
    await loadList(false);
    setMsg('配额已更新', true);
  }

  function bind() {
    $('btn-users-refresh') &&
      $('btn-users-refresh').addEventListener('click', () => loadList(true));
    $('users-q') &&
      $('users-q').addEventListener('keydown', (e) => {
        if (e.key === 'Enter') loadList(true);
      });
    $('btn-users-search') &&
      $('btn-users-search').addEventListener('click', () => loadList(true));
    $('btn-users-prev') &&
      $('btn-users-prev').addEventListener('click', () => {
        offset = Math.max(0, offset - limit);
        loadList(false);
      });
    $('btn-users-next') &&
      $('btn-users-next').addEventListener('click', () => {
        if (offset + limit < total) {
          offset += limit;
          loadList(false);
        }
      });
    $('btn-reset-submit') && $('btn-reset-submit').addEventListener('click', submitReset);
    $('btn-reset-cancel') &&
      $('btn-reset-cancel').addEventListener('click', () => closeModal('backdrop-reset-password'));
    $('btn-quota-submit') && $('btn-quota-submit').addEventListener('click', submitQuota);
    $('btn-quota-cancel') &&
      $('btn-quota-cancel').addEventListener('click', () => closeModal('backdrop-adjust-quota'));
    $('quota-mode-set') && $('quota-mode-set').addEventListener('change', toggleQuotaFields);
    $('quota-mode-delta') && $('quota-mode-delta').addEventListener('change', toggleQuotaFields);
    ['backdrop-reset-password', 'backdrop-adjust-quota'].forEach((id) => {
      const el = $(id);
      if (el) {
        el.addEventListener('click', (e) => {
          if (e.target === el) closeModal(id);
        });
      }
    });
  }

  return {
    init() {
      bind();
      loadList(true);
    },
  };
})();
