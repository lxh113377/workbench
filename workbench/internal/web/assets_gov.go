package web

// govJS 是治理控制台的前端脚本（原生 JS，零依赖，随 EXE 内嵌）。
const govJS = `(() => {
  const TOKEN = window.__WB_TOKEN__ || '';
  const root = document.getElementById('govRoot');
  const stat = document.getElementById('govStat');
  const api = async (path, opts = {}) => {
    const res = await fetch(path, {
      ...opts,
      headers: { 'Content-Type': 'application/json', 'X-WB-Token': TOKEN, ...(opts.headers || {}) },
    });
    const text = await res.text();
    let data = null;
    try { data = text ? JSON.parse(text) : null; } catch (e) { data = { raw: text }; }
    if (!res.ok) throw new Error((data && data.error) || ('HTTP ' + res.status));
    return data;
  };
  const esc = (s) => String(s == null ? '' : s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  const levelClass = (l) => (l === 'red' ? 'lv-red' : l === 'warn' ? 'lv-warn' : l === 'unverified' ? 'lv-unk' : 'lv-ok');
  const levelName = (l) => (l === 'red' ? '红区' : l === 'warn' ? '黄区' : l === 'unverified' ? '未验证' : '绿区');
  const card = (title, body) => '<section class="gov-card"><h3>' + esc(title) + '</h3>' + body + '</section>';
  const kv = (k, v) => '<div class="gov-kv"><span>' + esc(k) + '</span><b>' + esc(v) + '</b></div>';

  function render(sum) {
    const c = sum.contracts, v = sum.volume;
    stat.textContent = '等级 ' + levelName(sum.level) + ' · 契约 ' + c.total + ' 份 · 预警 ' + sum.alerts.total + ' 条';
    const parts = [];
    parts.push('<div class="gov-banner ' + levelClass(sum.level) + '">治理总等级：' + esc(levelName(sum.level)) +
      ' ｜ 契约 ' + c.total + ' 份（在履约 ' + c.active + '）｜ 体量采样 ' + v.samples + ' 点 ｜ 考核 ' +
      sum.checklist.pass + ' PASS / ' + sum.checklist.fail + ' FAIL</div>');

    // 1) 契约全生命周期
    const stageRows = (sum.stageBuckets || []).map((b) => kv(b.name, b.count + ' 份')).join('');
    parts.push(card('契约全生命周期', '<div class="gov-grid">' +
      kv('总量', c.total) + kv('签署中', c.signed) + kv('履约中', c.performing) + kv('已归档', c.archived) +
      kv('在履约', c.active) + kv('合规均分', c.complianceAvg) + kv('低合规', c.complianceLow) +
      kv('逾期', c.overdue) + kv('临期', c.expiringSoon) + kv('在履约金额', c.amountActive) +
      '</div><div class="gov-stages">' + stageRows + '</div>'));

    // 2) 体量三域
    const domainRows = (sum.domains || []).map((d) => '<div class="gov-row ' + levelClass(d.level) + '">' +
      '<b>' + esc(d.name) + '</b><span>' + levelName(d.level) + '</span>' +
      '<span>绿 ' + d.ok + ' / 黄 ' + d.warn + ' / 红 ' + d.red + ' / 未验证 ' + d.unknown + '</span>' +
      '<span>峰值占用 ' + Math.round(d.peakRatio * 100) + '%</span></div>').join('');
    parts.push(card('体量治理三域', domainRows || '<p class="gov-dim">暂无采样数据（未验证）</p>'));

    // 3) 指标明细
    const evalRows = (sum.evals || []).map((e) => '<div class="gov-row ' + levelClass(e.level) + '">' +
      '<b>' + esc(e.name) + '</b><span>' + e.value + esc(e.unit) + '</span>' +
      '<span>警示 ' + e.warn + ' / 红线 ' + e.red + '</span><span>' + levelName(e.level) + '</span></div>').join('');
    parts.push(card('量化指标（阈值与红线）', evalRows || '<p class="gov-dim">暂无采样数据（未验证）</p>'));

    // 4) 预警
    const alertRows = (sum.alerts.top || []).map((a) => '<div class="gov-row ' + levelClass(a.severity === 'critical' ? 'red' : a.severity) + '">' +
      '<b>' + esc(a.title) + '</b><span>' + esc(a.detail) + '</span>' +
      '<span class="gov-dim">处置：' + esc(a.suggestion) + '</span></div>').join('');
    parts.push(card('风险预警（' + sum.alerts.total + ' 条：红 ' + sum.alerts.critical + ' / 黄 ' + sum.alerts.warn + '）',
      alertRows || '<p class="gov-dim">当前无预警</p>'));

    // 5) 自适应调整
    const adjRows = (sum.adjustments || []).map((a) => '<div class="gov-row">' +
      '<b>' + esc(a.metric) + '</b><span>' + esc(a.kind) + '</span><span>' + esc(a.action) + '</span>' +
      '<span class="gov-dim">' + esc(a.basis) + '（置信 ' + Math.round(a.confidence * 100) + '%）</span></div>').join('');
    parts.push(card('自适应调整建议', adjRows || '<p class="gov-dim">无调整建议（样本不足或全绿区间）</p>'));

    // 6) 联动
    const lk = sum.linkage || {};
    const sigRows = (lk.signals || []).filter((s) => s.severity !== 'info').map((s) => '<div class="gov-row ' +
      levelClass(s.severity === 'critical' ? 'red' : s.severity) + '"><b>' + esc(s.title) + '</b><span>' +
      esc(s.detail) + '</span><span class="gov-dim">决策：' + esc(s.decision) + '</span></div>').join('');
    const allocRows = (lk.allocations || []).map((a) => '<div class="gov-row"><b>' + esc(a.contractId) + '</b><span>' +
      esc(a.title) + '</span><span>份额 ' + Math.round(a.share * 100) + '%</span><span>负载 ' + a.loadScore +
      '（' + a.rating + '）</span><span class="gov-dim">' + esc(a.suggestion) + '</span></div>').join('');
    parts.push(card('契约 × 体量 联动分析',
      '<div class="gov-grid">' + kv('契约负载率', Math.round((lk.loadRatio || 0) * 100) + '%') +
      kv('负载×耗时相关', lk.corrCycle) + kv('负载×失败相关', lk.corrFailure) +
      kv('联动等级', levelName(lk.level)) + '</div>' + (sigRows || '<p class="gov-dim">无联动异常信号</p>') +
      (allocRows ? '<h4>体量风险分摊</h4>' + allocRows : '')));

    // 7) 考核校验
    const ckRows = (sum.checklist.items || []).map((it) => '<div class="gov-row ' + (it.level === 'ok' ? 'lv-ok' : it.level === 'unverified' ? 'lv-unk' : 'lv-warn') + '">' +
      '<b>' + esc(it.code) + ' ' + esc(it.name) + '</b><span>' + (it.level === 'ok' ? 'PASS' : it.level === 'unverified' ? '未验证' : 'FAIL') + '</span>' +
      '<span>目标：' + esc(it.target) + '</span><span>实际：' + esc(it.actual) + '</span>' +
      '<span class="gov-dim">' + esc(it.evidence) + '</span></div>').join('');
    parts.push(card('考核校验（' + sum.checklist.pass + ' PASS / ' + sum.checklist.fail + ' FAIL / ' + (sum.checklist.unverified || 0) + ' 未验证）', ckRows));

    if (sum.dataGaps && sum.dataGaps.length) {
      parts.push(card('数据缺口（必须显式声明）', '<ul>' + sum.dataGaps.map((g) => '<li>' + esc(g) + '</li>').join('') + '</ul>'));
    }
    root.innerHTML = parts.join('');
  }

  async function load() {
    try {
      const sum = await api('/api/gov/summary');
      render(sum);
    } catch (e) {
      root.innerHTML = '<p class="gov-dim">治理数据加载失败：' + esc(e.message) + '</p>';
      stat.textContent = '加载失败';
    }
  }

  document.getElementById('govRefresh').addEventListener('click', load);
  document.getElementById('govSample').addEventListener('click', async () => {
    try {
      const sum = await api('/api/gov/sample', { method: 'POST', body: JSON.stringify({}) });
      render(sum);
    } catch (e) {
      alert('采样失败：' + e.message);
    }
  });
  load();
  setInterval(load, 30000);
})();
`;