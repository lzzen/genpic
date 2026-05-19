/* global AdminApp */
const AdminTemplates = (function () {
  let offset = 0;
  const limit = 30;
  let total = 0;

  function $(id) {
    return document.getElementById(id);
  }

  function setMsg(text, ok) {
    const el = $('tpl-msg');
    if (!el) return;
    el.textContent = text || '';
    el.className = 'admin-msg' + (text ? (ok ? ' ok' : ' err') : '');
  }

  function renderTemplateRows(rows) {
    const tb = $('tpl-tbody');
    if (!tb) return;
    tb.innerHTML = '';
    if (!rows || !rows.length) {
      const tr = document.createElement('tr');
      tr.innerHTML = '<td colspan="5" style="padding:16px;color:var(--muted)">暂无模板</td>';
      tb.appendChild(tr);
      return;
    }
    for (const row of rows) {
      const tr = document.createElement('tr');
      const vis = (row.visibility || 'private') === 'public';
      const badge = vis
        ? '<span class="badge-pub">公用</span>'
        : '<span class="badge-priv">私有</span>';
      const owner = (row.owner_email || row.user_id || '—').toString();
      const title = (row.title || '(无标题)').toString();
      const model = [row.provider, row.primary_model].filter(Boolean).join(' · ');
      const btns =
        '<button type="button" class="admin-btn secondary small tpl-vis" data-id="' +
        AdminApp.escapeAttr(row.id) +
        '" data-vis="public"' +
        (vis ? ' disabled' : '') +
        '>设为公用</button> ' +
        '<button type="button" class="admin-btn secondary small tpl-vis" data-id="' +
        AdminApp.escapeAttr(row.id) +
        '" data-vis="private"' +
        (!vis ? ' disabled' : '') +
        '>设为私有</button>';
      const thumbHtml = row.result_image_url
        ? '<img class="thumb" src="' + String(row.result_image_url).replace(/"/g, '') + '" alt="" loading="lazy" />'
        : '<div class="thumb"></div>';
      tr.innerHTML =
        '<td>' + thumbHtml + '</td>' +
        '<td><div style="font-weight:500">' + AdminApp.escapeHtml(title) + '</div>' +
        '<div style="color:var(--muted);font-size:0.78rem;margin-top:4px">' + AdminApp.escapeHtml(model) + '</div>' +
        '<div style="color:var(--muted);font-size:0.75rem;margin-top:4px;max-width:280px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis" title="' +
        AdminApp.escapeAttr(row.prompt_preview || '') + '">' + AdminApp.escapeHtml(row.prompt_preview || '') + '</div></td>' +
        '<td style="font-size:0.78rem">' + AdminApp.escapeHtml(owner) + '</td>' +
        '<td>' + badge + '</td>' +
        '<td style="white-space:nowrap">' + btns + '</td>';
      tb.appendChild(tr);

    }
    tb.querySelectorAll('button.tpl-vis').forEach(function (btn) {
      btn.addEventListener('click', onVisClick);
    });
  }

  async function onVisClick(ev) {
    const btn = ev.currentTarget;
    const id = btn.getAttribute('data-id');
    const vis = btn.getAttribute('data-vis');
    setMsg('');
    const { res, data } = await AdminApp.apiFetch(
      '/api/admin/templates/' + encodeURIComponent(id) + '/visibility',
      { method: 'PUT', body: JSON.stringify({ visibility: vis }) }
    );
    if (!res.ok) {
      setMsg(AdminApp.errMessage(data), false);
      return;
    }
    setMsg('已更新模板 ' + id.slice(0, 8) + '…', true);
    loadList(false);
  }

  async function loadList(resetOffset) {
    if (!AdminApp.isAdmin()) return;
    if (resetOffset) offset = 0;
    setMsg('');
    const q = new URLSearchParams();
    q.set('limit', String(limit));
    q.set('offset', String(offset));
    const vf = $('tpl-vis-filter') && $('tpl-vis-filter').value.trim();
    if (vf) q.set('visibility', vf);
    const { res, data } = await AdminApp.apiFetch('/api/admin/templates?' + q.toString());
    if (!res.ok) {
      setMsg(AdminApp.errMessage(data), false);
      if ($('tpl-tbody')) $('tpl-tbody').innerHTML = '';
      return;
    }
    total = typeof data.total === 'number' ? data.total : 0;
    renderTemplateRows(data.data || []);
    const meta = $('tpl-pager-meta');
    if (meta) {
      meta.textContent =
        '第 ' +
        (offset + 1) +
        '–' +
        Math.min(offset + limit, total) +
        ' 条，共 ' +
        total +
        ' 条';
    }
    if ($('btn-tpl-prev')) $('btn-tpl-prev').disabled = offset <= 0;
    if ($('btn-tpl-next')) $('btn-tpl-next').disabled = offset + limit >= total;
  }

  function bind() {
    $('btn-refresh-templates') &&
      $('btn-refresh-templates').addEventListener('click', function () {
        loadList(true);
      });
    $('tpl-vis-filter') &&
      $('tpl-vis-filter').addEventListener('change', function () {
        loadList(true);
      });
    $('btn-tpl-prev') &&
      $('btn-tpl-prev').addEventListener('click', function () {
        offset = Math.max(0, offset - limit);
        loadList(false);
      });
    $('btn-tpl-next') &&
      $('btn-tpl-next').addEventListener('click', function () {
        if (offset + limit < total) {
          offset += limit;
          loadList(false);
        }
      });
  }

  return {
    init: function () {
      bind();
      loadList(true);
    },
  };
})();
