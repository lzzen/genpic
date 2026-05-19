/* global AdminApp, Chart */
const AdminStats = (function () {
  let chartRateRank = null;
  let chartLatRank = null;
  let chartRateMulti = null;
  let chartRateSingle = null;
  let lastBuckets = [];
  let bound = false;

  function $(id) {
    return document.getElementById(id);
  }

  function chartTextColor() {
    const m = getComputedStyle(document.documentElement).getPropertyValue('--text').trim();
    return m || '#e5e7eb';
  }
  function chartMutedColor() {
    const m = getComputedStyle(document.documentElement).getPropertyValue('--muted').trim();
    return m || '#9ca3af';
  }
  function chartBorderColor() {
    const m = getComputedStyle(document.documentElement).getPropertyValue('--border').trim();
    return m || '#374151';
  }
  function chartAccentColor() {
    const m = getComputedStyle(document.documentElement).getPropertyValue('--accent').trim();
    return m || '#6366f1';
  }

  function destroyChart(ch) {
    if (ch) {
      try {
        ch.destroy();
      } catch (_) {}
    }
    return null;
  }

  function topFailureCode(fmap) {
    if (!fmap || typeof fmap !== 'object') return '—';
    let best = '';
    let n = 0;
    for (const k of Object.keys(fmap)) {
      if (fmap[k] > n) {
        n = fmap[k];
        best = k;
      }
    }
    return best ? best + ' (' + n + ')' : '—';
  }

  function fillModelSelectors(models) {
    const statsModels = $('stats-models');
    const statsSingleModel = $('stats-single-model');
    if (!statsModels || !statsSingleModel) return;
    const sorted = models.slice().sort(function (a, b) {
      return (b.total || 0) - (a.total || 0);
    });
    statsModels.innerHTML = '';
    statsSingleModel.innerHTML = '<option value="">— 请选择 —</option>';
    for (const m of sorted) {
      const id = m.model || '';
      const opt = document.createElement('option');
      opt.value = id;
      opt.textContent = id + ' · ' + (m.provider || '') + ' (n=' + (m.total || 0) + ')';
      statsModels.appendChild(opt);
    }
    for (const m of sorted) {
      const id = m.model || '';
      const o2 = document.createElement('option');
      o2.value = id;
      o2.textContent = id;
      statsSingleModel.appendChild(o2);
    }
    let i = 0;
    for (const opt of statsModels.options) {
      opt.selected = i < 5;
      i++;
    }
  }

  function renderStatsTable(models) {
    const statsTbody = $('stats-tbody');
    if (!statsTbody) return;
    statsTbody.innerHTML = '';
    if (!models || !models.length) {
      const tr = document.createElement('tr');
      tr.innerHTML = '<td colspan="8" style="padding:16px;color:var(--muted)">该窗口内无已结束任务</td>';
      statsTbody.appendChild(tr);
      return;
    }
    for (const m of models) {
      const tr = document.createElement('tr');
      const rate = m.total ? (100 * m.success_rate).toFixed(1) + '%' : '—';
      const low = m.total >= 3 && m.success_rate < 0.9;
      const avg = m.avg_processing_ms != null ? String(m.avg_processing_ms) : '—';
      const p95 = m.p95_processing_ms != null ? String(m.p95_processing_ms) : '—';
      const ins = m.sample_insufficient
        ? ' <span style="color:var(--muted);font-size:0.72rem">(样本&lt;3)</span>'
        : '';
      tr.innerHTML =
        '<td>' +
        AdminApp.escapeHtml(m.model || '') +
        ins +
        '</td>' +
        '<td>' +
        AdminApp.escapeHtml(m.provider || '') +
        '</td>' +
        '<td>' +
        (m.succeeded || 0) +
        '</td>' +
        '<td>' +
        (m.failed || 0) +
        '</td>' +
        '<td' +
        (low ? ' style="color:#f87171;font-weight:600"' : '') +
        '>' +
        rate +
        '</td>' +
        '<td>' +
        avg +
        '</td>' +
        '<td>' +
        p95 +
        '</td>' +
        '<td style="font-size:0.78rem">' +
        AdminApp.escapeHtml(topFailureCode(m.failures_by_code)) +
        '</td>';
      statsTbody.appendChild(tr);
    }
  }

  function renderRankBar(canvasId, rows, valueKey, labelSuffix, horizontal) {
    const el = $(canvasId);
    if (!el || typeof Chart === 'undefined') return null;
    if (!rows || !rows.length) return null;
    const labels = rows.map(function (r) {
      return (r.model || '') + ' · ' + (r.provider || '');
    });
    const data = rows.map(function (r) {
      return valueKey === 'success_rate' ? 100 * (r.success_rate || 0) : r.avg_processing_ms || 0;
    });
    const bg = rows.map(function (r) {
      if (r.sample_insufficient) return chartMutedColor();
      return chartAccentColor();
    });
    return new Chart(el, {
      type: 'bar',
      data: {
        labels: labels,
        datasets: [
          {
            label: labelSuffix,
            data: data,
            backgroundColor: bg,
            borderColor: chartBorderColor(),
            borderWidth: 1,
          },
        ],
      },
      options: {
        indexAxis: horizontal ? 'y' : 'x',
        responsive: true,
        maintainAspectRatio: false,
        plugins: { legend: { display: false } },
        scales: {
          x: { ticks: { color: chartMutedColor() }, grid: { color: chartBorderColor() } },
          y: { ticks: { color: chartMutedColor() }, grid: { color: chartBorderColor() } },
        },
      },
    });
  }

  function selectedModelsList() {
    const statsModels = $('stats-models');
    const out = [];
    if (!statsModels) return out;
    for (const o of statsModels.selectedOptions) {
      if (o.value) out.push(o.value);
    }
    return out;
  }

  function colorsForModels(n) {
    const base = ['#6366f1', '#22c55e', '#f97316', '#ec4899', '#14b8a6', '#eab308', '#a855f7', '#ef4444'];
    const out = [];
    for (let i = 0; i < n; i++) out.push(base[i % base.length]);
    return out;
  }

  function renderTimeseriesCharts(buckets, modelIds, singleId) {
    chartRateMulti = destroyChart(chartRateMulti);
    chartRateSingle = destroyChart(chartRateSingle);
    const labels = buckets.map(function (b) {
      return b.label || '';
    });

    const multiEl = $('chart-rate-multi');
    if (multiEl && typeof Chart !== 'undefined' && modelIds.length) {
      const ds = [];
      const cols = colorsForModels(modelIds.length);
      let ci = 0;
      for (const mid of modelIds) {
        const pts = buckets.map(function (b) {
          const row = (b.models || []).find(function (m) {
            return m.model === mid;
          });
          if (!row || !row.total) return null;
          return 100 * (row.success_rate || 0);
        });
        ds.push({
          label: mid,
          data: pts,
          borderColor: cols[ci % cols.length],
          backgroundColor: 'transparent',
          tension: 0.15,
          spanGaps: true,
        });
        ci++;
      }
      chartRateMulti = new Chart(multiEl, {
        type: 'line',
        data: { labels: labels, datasets: ds },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          plugins: { legend: { labels: { color: chartTextColor() } } },
          scales: {
            x: { ticks: { color: chartMutedColor() }, grid: { color: chartBorderColor() } },
            y: {
              min: 0,
              max: 100,
              ticks: { color: chartMutedColor() },
              grid: { color: chartBorderColor() },
            },
          },
        },
      });
    }

    const singleEl = $('chart-rate-single');
    if (singleEl && typeof Chart !== 'undefined' && singleId) {
      const pts = buckets.map(function (b) {
        const row = (b.models || []).find(function (m) {
          return m.model === singleId;
        });
        if (!row || !row.total) return null;
        return 100 * (row.success_rate || 0);
      });
      chartRateSingle = new Chart(singleEl, {
        type: 'line',
        data: {
          labels: labels,
          datasets: [
            {
              label: singleId,
              data: pts,
              borderColor: chartAccentColor(),
              backgroundColor: 'rgba(99,102,241,0.08)',
              fill: true,
              tension: 0.2,
              spanGaps: true,
            },
          ],
        },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          plugins: { legend: { labels: { color: chartTextColor() } } },
          scales: {
            x: { ticks: { color: chartMutedColor() }, grid: { color: chartBorderColor() } },
            y: { min: 0, max: 100, ticks: { color: chartMutedColor() }, grid: { color: chartBorderColor() } },
          },
        },
      });
    }
  }

  async function loadModelStats() {
    if (!AdminApp.isAdmin()) return;
    const statsMsg = $('stats-msg');
    if (statsMsg) {
      statsMsg.textContent = '';
      statsMsg.className = 'admin-msg';
    }
    const days = ($('stats-days') && $('stats-days').value) || '7';
    const gran = ($('stats-granularity') && $('stats-granularity').value) || 'day';
    if (gran === 'hour' && Number(days) > 7) {
      if (statsMsg) {
        statsMsg.textContent = '按小时统计时请将时间范围设为 7 天以内';
        statsMsg.classList.add('err');
      }
      return;
    }

    const qs = new URLSearchParams();
    qs.set('days', days);
    const { res, data } = await AdminApp.apiFetch('/api/admin/model-stats?' + qs.toString());
    if (!res.ok) {
      if (statsMsg) {
        statsMsg.textContent = AdminApp.errMessage(data);
        statsMsg.classList.add('err');
      }
      return;
    }
    const stats = data.stats || {};
    const models = stats.models || [];
    fillModelSelectors(models);
    renderStatsTable(models);

    chartRateRank = destroyChart(chartRateRank);
    chartLatRank = destroyChart(chartLatRank);
    const rateRows = models.slice().sort(function (a, b) {
      return (a.success_rate || 0) - (b.success_rate || 0);
    });
    const latRows = models
      .filter(function (m) {
        return m.avg_processing_ms != null;
      })
      .sort(function (a, b) {
        return (b.avg_processing_ms || 0) - (a.avg_processing_ms || 0);
      });
    chartRateRank = renderRankBar('chart-rate-rank', rateRows, 'success_rate', '成功率 %', true);
    chartLatRank = renderRankBar('chart-lat-rank', latRows, 'avg_processing_ms', '平均 ms', true);

    const q2 = new URLSearchParams();
    q2.set('days', days);
    q2.set('granularity', gran);
    const modelsParam = selectedModelsList().join(',');
    if (modelsParam) q2.set('models', modelsParam);

    const ts = await AdminApp.apiFetch('/api/admin/model-stats/timeseries?' + q2.toString());
    if (!ts.res.ok) {
      if (statsMsg) {
        statsMsg.textContent = AdminApp.errMessage(ts.data);
        statsMsg.classList.add('err');
      }
      chartRateMulti = destroyChart(chartRateMulti);
      chartRateSingle = destroyChart(chartRateSingle);
      return;
    }
    lastBuckets = (ts.data.stats && ts.data.stats.buckets) || [];
    renderTimeseriesCharts(lastBuckets, selectedModelsList(), ($('stats-single-model') && $('stats-single-model').value) || '');
    if (statsMsg) {
      statsMsg.textContent = '已加载 ' + models.length + ' 个模型 · ' + lastBuckets.length + ' 个时间桶';
      statsMsg.classList.add('ok');
    }
  }

  async function reloadTimeseriesOnly() {
    if (!AdminApp.isAdmin()) return;
    const statsMsg = $('stats-msg');
    const days = ($('stats-days') && $('stats-days').value) || '7';
    const gran = ($('stats-granularity') && $('stats-granularity').value) || 'day';
    if (gran === 'hour' && Number(days) > 7) {
      if (statsMsg) {
        statsMsg.textContent = '按小时统计时请将时间范围设为 7 天以内';
        statsMsg.classList.add('err');
      }
      return;
    }
    const q2 = new URLSearchParams();
    q2.set('days', days);
    q2.set('granularity', gran);
    const modelsParam = selectedModelsList().join(',');
    if (modelsParam) q2.set('models', modelsParam);
    const ts = await AdminApp.apiFetch('/api/admin/model-stats/timeseries?' + q2.toString());
    if (!ts.res.ok) {
      if (statsMsg) {
        statsMsg.textContent = AdminApp.errMessage(ts.data);
        statsMsg.classList.add('err');
      }
      return;
    }
    lastBuckets = (ts.data.stats && ts.data.stats.buckets) || [];
    renderTimeseriesCharts(lastBuckets, selectedModelsList(), ($('stats-single-model') && $('stats-single-model').value) || '');
  }

  function bind() {
    if (bound) return;
    bound = true;
    $('btn-stats-refresh') && $('btn-stats-refresh').addEventListener('click', loadModelStats);
    $('stats-days') && $('stats-days').addEventListener('change', loadModelStats);
    $('stats-granularity') && $('stats-granularity').addEventListener('change', loadModelStats);
    $('stats-models') && $('stats-models').addEventListener('change', reloadTimeseriesOnly);
    $('stats-single-model') &&
      $('stats-single-model').addEventListener('change', function () {
        renderTimeseriesCharts(
          lastBuckets,
          selectedModelsList(),
          ($('stats-single-model') && $('stats-single-model').value) || ''
        );
      });
  }

  return {
    init: function () {
      bind();
      if ($('btn-stats-refresh')) $('btn-stats-refresh').disabled = false;
      loadModelStats();
    },
  };
})();
