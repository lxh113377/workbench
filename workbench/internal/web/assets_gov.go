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
  // toast 反馈（2026-10-22 回归修复）：治理台此前只有 alert，本轮把采样失败提示改成
  // toast 时漏了本函数 —— 门禁 interact-governance 当场判红「toast is not a function」。
  // 三张页面（app.js / pool.js / gov.js）各自是独立脚本包，helper 必须**各留一份**，
  // 不能假设某一页的定义对另一页可见。
  const toast = (msg) => { const el = document.getElementById('toast'); if (!el) { console.log(msg); return; } el.textContent = msg; el.classList.add('show'); clearTimeout(el._t); el._t = setTimeout(() => el.classList.remove('show'), 2600); };
  const levelClass = (l) => (l === 'red' ? 'lv-red' : l === 'warn' ? 'lv-warn' : l === 'unverified' ? 'lv-unk' : 'lv-ok');
  const levelName = (l) => (l === 'red' ? '红区' : l === 'warn' ? '黄区' : l === 'unverified' ? '未验证' : '绿区');
  const card = (title, body) => '<section class="gov-card"><h3>' + esc(title) + '</h3>' + body + '</section>';
  // 2026-10-20 简化：明细默认收起。治理台一屏只留 hero（结论）+ 风险预警 + 考核校验，
  // 四个「取证明细」卡折进 <details>，要看再点开 —— 原来 7 张卡全铺开有 3500px 高。
  const foldCard = (title, body) => '<details class="gov-card foldcard"><summary>' + esc(title) + '</summary><div class="fold-body">' + body + '</div></details>';
  const kv = (k, v) => '<div class="gov-kv"><span>' + esc(k) + '</span><b>' + esc(v) + '</b></div>';

  // 首屏状态带：先给「结论 + 六个关键读数」，再往下才是取证明细。
  // 视觉层级靠尺度差建立（超大数字 vs 微型标签），不靠加框。
  function hero(sum) {
    const c = sum.contracts, v = sum.volume, a = sum.alerts, k = sum.checklist;
    const cells = [
      { k: '契约总量', v: c.total, u: '份' },
      { k: '在履约', v: c.active, u: '份' },
      { k: '合规均分', v: c.complianceAvg, u: '' },
      { k: '逾期', v: c.overdue, u: '份', tone: c.overdue > 0 ? 'danger' : '' },
      { k: '预警', v: a.total, u: '条', tone: a.critical > 0 ? 'danger' : (a.warn > 0 ? 'warn' : '') },
      { k: '体量采样', v: v.samples, u: '点' },
    ];
    return '<div class="gov-hero ' + levelClass(sum.level) + '">' +
      '<div class="gov-hero-main">' +
        '<div class="gov-hero-label">治理总等级</div>' +
        '<div class="gov-hero-level">' + esc(levelName(sum.level)) + '</div>' +
        '<div class="gov-hero-foot">考核 ' + k.pass + ' PASS / ' + k.fail + ' FAIL / ' + (k.unverified || 0) + ' 未验证' +
        ' ｜ 契约 ' + c.total + ' 份（在履约 ' + c.active + '）｜ 采样 ' + v.samples + ' 点</div>' +
      '</div>' +
      '<div class="gov-hero-cells">' + cells.map(function (x) {
        return '<div class="gov-cell' + (x.tone ? ' tone-' + x.tone : '') + '"><b>' + esc(x.v) + (x.u ? '<i>' + esc(x.u) + '</i>' : '') + '</b><span>' + esc(x.k) + '</span></div>';
      }).join('') + '</div>' +
    '</div>';
  }

  function render(sum) {
    const c = sum.contracts, v = sum.volume;
    stat.textContent = '等级 ' + levelName(sum.level) + ' · 契约 ' + c.total + ' 份 · 预警 ' + sum.alerts.total + ' 条';
    const parts = [];
    parts.push(hero(sum));
    // 2026-10-20 简化：原来 hero 下面还有一条 gov-banner 把同样的等级/契约/采样/考核
    // 再念一遍（重复信息 = 视觉噪音），已删除。
    // 首启无数据时不再铺 7 张全 0 的卡，先给一句「怎么让它有数据」。
    const noData = (v.samples || 0) === 0 && (c.total || 0) === 0;
    if (noData) {
      parts.push('<section class="gov-card"><h3>还没有治理数据</h3>' +
        '<div class="empty-hint">治理台要靠「采样」才有读数：点右上角<b>立即采样</b>，系统会把当前资源、任务吞吐、契约履约各量一遍，' +
        '再据此算红/黄/绿区与考核结论。没有采样时所有指标都是「未验证」，这是如实标注，不是坏了。</div></section>');
    }

    // 1) 契约全生命周期
    const stageRows = (sum.stageBuckets || []).map((b) => kv(b.name, b.count + ' 份')).join('');
    parts.push(foldCard('契约全生命周期', '<div class="gov-grid">' +
      kv('总量', c.total) + kv('签署中', c.signed) + kv('履约中', c.performing) + kv('已归档', c.archived) +
      kv('在履约', c.active) + kv('合规均分', c.complianceAvg) + kv('低合规', c.complianceLow) +
      kv('逾期', c.overdue) + kv('临期', c.expiringSoon) + kv('在履约金额', c.amountActive) +
      '</div><div class="gov-stages">' + stageRows + '</div>'));

    // 2) 体量三域
    const domainRows = (sum.domains || []).map((d) => '<div class="gov-row ' + levelClass(d.level) + '">' +
      '<b>' + esc(d.name) + '</b><span>' + levelName(d.level) + '</span>' +
      '<span>绿 ' + d.ok + ' / 黄 ' + d.warn + ' / 红 ' + d.red + ' / 未验证 ' + d.unknown + '</span>' +
      '<span>峰值占用 ' + Math.round(d.peakRatio * 100) + '%</span></div>').join('');
    parts.push(foldCard('体量治理三域', domainRows || '<p class="gov-dim">暂无采样数据（未验证）</p>'));

    // 3) 指标明细
    // 读数必须可扫（2026-10-22 走查实测）：原样拼接会把「比例 0.51」与「4000000 元」
    // 这类不可读形态直接铺到面板上。govNum 按单位语义换算：
    //   · 比例 → 百分数（0.51 → 51%），阈值同步换算（红线 0.1 → 10%）；
    //   · 元 / MiB 且 ≥1000 → 千分位分组（4000000 → 4,000,000）；
    //   · 其余保持原精度。
    const govNum = (v, unit) => {
      if (typeof v !== 'number' || !isFinite(v)) { return esc(String(v)); }
      if (unit === '比例') { return (v * 100).toFixed(Math.abs(v * 100) < 10 ? 1 : 0) + '%'; }
      if (Math.abs(v) >= 1000) { return v.toLocaleString('zh-CN', { maximumFractionDigits: 1 }); }
      return String(v);
    };
    const evalRows = (sum.evals || []).map((e) => '<div class="gov-row ' + levelClass(e.level) + '">' +
      '<b>' + esc(e.name) + '</b><span>' + govNum(e.value, e.unit) + (e.unit === '比例' ? '' : esc(e.unit)) + '</span>' +
        (e.note ? '<span class="gov-dim" style="display:block;font-size:11px;line-height:1.5;margin-top:2px">' + esc(e.note) + '</span>' : '') +
      '<span>警示 ' + govNum(e.warn, e.unit) + ' / 红线 ' + govNum(e.red, e.unit) + '</span><span>' + levelName(e.level) + '</span></div>').join('');
    parts.push(foldCard('量化指标（阈值与红线）', evalRows || '<p class="gov-dim">暂无采样数据（未验证）</p>'));

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
    parts.push(foldCard('自适应调整建议', adjRows || '<p class="gov-dim">无调整建议（样本不足或全绿区间）</p>'));

    // 6) 联动
    const lk = sum.linkage || {};
    const sigRows = (lk.signals || []).filter((s) => s.severity !== 'info').map((s) => '<div class="gov-row ' +
      levelClass(s.severity === 'critical' ? 'red' : s.severity) + '"><b>' + esc(s.title) + '</b><span>' +
      esc(s.detail) + '</span><span class="gov-dim">决策：' + esc(s.decision) + '</span></div>').join('');
    const allocRows = (lk.allocations || []).map((a) => '<div class="gov-row"><b>' + esc(a.contractId) + '</b><span>' +
      esc(a.title) + '</span><span>份额 ' + Math.round(a.share * 100) + '%</span><span>负载 ' + a.loadScore +
      '（' + a.rating + '）</span><span class="gov-dim">' + esc(a.suggestion) + '</span></div>').join('');
    parts.push(foldCard('契约 × 体量 联动分析',
      '<div class="gov-grid">' + kv('契约负载率', Math.round((lk.loadRatio || 0) * 100) + '%') +
      kv('负载×耗时相关', lk.corrCycle) + kv('负载×失败相关', lk.corrFailure) +
      kv('联动等级', levelName(lk.level)) + '</div>' + (sigRows || '<p class="gov-dim">无联动异常信号</p>') +
      (allocRows ? '<h4>体量风险分摊</h4>' + allocRows : '')));

    // 7) 考核校验
    // 考核校验：矩阵网格（一屏扫完 PASS/FAIL），详情压进 title，不再铺成长列表。
    const ckRows = (sum.checklist.items || []).map((it) => {
      // 三态来源 = 服务端 state（ok/fail/unverified）。旧版读不存在的 it.level ⇒ 全判 bad，
      // 与头部「N PASS / N FAIL / N 未验证」自相矛盾（2026-10-21 走查实测缺陷）。
      const raw = it.state || it.level || (it.pass ? 'ok' : 'fail');
      const lv = raw === 'ok' ? 'ok' : raw === 'unverified' ? 'unk' : 'bad';
      const st = lv === 'ok' ? 'PASS' : lv === 'unk' ? '未验证' : 'FAIL';
      const tip = '目标：' + it.target + '\n实际：' + it.actual + '\n证据：' + it.evidence;
      return '<div class="gov-check ' + lv + '" title="' + esc(tip) + '">' +
        '<span class="gov-check-code">' + esc(it.code) + '</span>' +
        '<b>' + esc(it.name) + '</b>' +
        '<span class="gov-check-state">' + st + '</span></div>';
    }).join('');
    parts.push(card('考核校验（' + sum.checklist.pass + ' PASS / ' + sum.checklist.fail + ' FAIL / ' + (sum.checklist.unverified || 0) + ' 未验证）',
      '<div class="gov-check-grid">' + ckRows + '</div>'));

    if (sum.dataGaps && sum.dataGaps.length) {
      parts.push(foldCard('数据缺口（必须显式声明）', '<ul>' + sum.dataGaps.map((g) => '<li>' + esc(g) + '</li>').join('') + '</ul>'));
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

  // 「更多」菜单：跨页入口收进一个按钮（与管理面板/模型池同构）。
  const btnMore = document.getElementById('btnMore');
  const topMore = document.getElementById('topMore');
  if (btnMore && topMore) {
    btnMore.addEventListener('click', function () {
      const willOpen = topMore.hidden;
      topMore.hidden = !willOpen;
      btnMore.setAttribute('aria-expanded', willOpen ? 'true' : 'false');
    });
    document.addEventListener('click', function (ev) {
      if (!topMore.hidden && ev.target !== btnMore && !topMore.contains(ev.target)) {
        topMore.hidden = true; btnMore.setAttribute('aria-expanded', 'false');
      }
    });
  }

  document.getElementById('govRefresh').addEventListener('click', load);
  // 采样按钮必须有 busy 反馈（2026-10-22 慢后端实测）：采样要跑一次真实量测，可能耗时数秒，
  // 此前按钮文案与 disabled 全程不变（实测 +0/+700/+3700ms 三帧读数完全一致）—— 用户点了
  // 没有任何变化，只会当成「没反应」再点一次。同时把 alert 换成 toast：
  // 原生 alert 阻塞界面且不属于本设计体系（面板其余反馈一律走 toast）。
  const govSampleBtn = document.getElementById('govSample');
  govSampleBtn.addEventListener('click', async () => {
    const label0 = govSampleBtn.textContent;
    govSampleBtn.disabled = true;
    govSampleBtn.textContent = '采样中…';
    try {
      const sum = await api('/api/gov/sample', { method: 'POST', body: JSON.stringify({}) });
      render(sum);
      toast('采样完成');
    } catch (e) {
      toast('采样失败：' + e.message);
    } finally {
      govSampleBtn.disabled = false;
      govSampleBtn.textContent = label0;
    }
  });
  load();
  setInterval(load, 30000);
})();
`
