package web

// poolJS 是模型池控制台的前端逻辑（原生 JS，无框架、无构建）。
const poolJS = `(() => {
  const TOKEN = window.__WB_TOKEN__ || '';
  // opts.timeoutMs 是**可选**的：不传就不建 AbortController，行为与改动前逐字节一致 ——
  // 因为 /api/pool/probe 这类接口是合法慢的（probeSpecs 串行遍历 base，每个 client 超时 10s，
  // 整体可达 N×10s）。给所有调用一刀切设超时会把能用的功能改坏，那比原缺陷更糟。
  // 本文件的调用方目前都不传它（gov/pool 端点经查为快速只读，无等锁问题），保留与改前一致的行为。
  const api = async (path, opts = {}) => {
    const timeoutMs = opts.timeoutMs;
    const ctl = timeoutMs ? new AbortController() : null;
    const timer = ctl ? setTimeout(() => ctl.abort(), timeoutMs) : null;
    let res, text;
    try {
      const rest = { ...opts };
      delete rest.timeoutMs;
      const fetchOpts = { ...rest, headers: { 'Content-Type': 'application/json', 'X-WB-Token': TOKEN, ...(opts.headers || {}) } };
      if (ctl) { fetchOpts.signal = ctl.signal; }
      res = await fetch(path, fetchOpts);
      text = await res.text();
    } finally {
      if (timer) { clearTimeout(timer); }
    }
    let data = null;
    try { data = text ? JSON.parse(text) : null; } catch (e) { data = { raw: text }; }
    if (!res.ok) throw new Error((data && (data.error || data.message)) || ('HTTP ' + res.status));
    return data;
  };
  const ESC = { '&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;' };
  ESC[String.fromCharCode(39)] = '&#39;';
  const esc = (s) => String(s == null ? '' : s).replace(/[&<>"']/g, c => ESC[c]);
  // 两段式就地确认（armed confirm）—— 取代原生 confirm()，理由见 app.js 同名注释。
  // 本包**自带**一份：app.js / pool.js / gov.js 是三个独立脚本包，顶层 const 不跨页可见。
  const ARM_MS = 4000;
  const disarmArm = (btn) => {
    if (!btn || btn.dataset.armed !== '1') { return; }
    clearTimeout(btn._armTimer);
    if (btn.dataset.label0 != null) { btn.textContent = btn.dataset.label0; }
    delete btn.dataset.armed;
    btn.classList.remove('armed');
  };
  const armConfirm = (btn, confirmText, run) => {
    // fail-closed：拿不到宿主元素就中止。早先这里写的是 run()，
    // 于是「⋯ 菜单漏传元素」会变成**跳过确认直接删除** —— 比改造前更危险。
    if (!btn) { return; }
    if (btn.dataset.armed === '1') { disarmArm(btn); run(); return; }
    document.querySelectorAll('[data-armed="1"]').forEach(disarmArm);
    btn.dataset.label0 = btn.textContent;
    btn.dataset.armed = '1';
    btn.textContent = confirmText;
    btn.classList.add('armed');
    btn._armTimer = setTimeout(() => disarmArm(btn), ARM_MS);
  };
  const toast = (msg) => { const el = document.getElementById('toast'); if (!el) { console.log(msg); return; } el.textContent = msg; el.classList.add('show'); clearTimeout(el._t); el._t = setTimeout(() => el.classList.remove('show'), 3000); };
  const fmtMs = (ms) => { if (!ms) return '—'; if (ms < 1000) return ms + ' ms'; return (ms/1000).toFixed(1) + ' s'; };
  const fmtTime = (ms) => ms ? new Date(ms).toLocaleString('zh-CN', {hour12:false}) : '—';
  const STATE_TEXT = { healthy:'可用', degraded:'降级', down:'下线', unknown:'未探测', disabled:'已停用' };
  // 调度策略显示名（老大 2026-10-20：英文看不懂）。值仍发英文给 API，只有展示层是中文。
  const STRAT_TEXT = { priority:'优先级（主用+兜底）', 'round-robin':'轮转（轮流用）', weighted:'加权（按权重抽签）', 'least-loaded':'最空闲（谁闲用谁）' };
  const stratName = (s) => STRAT_TEXT[s] || s || '优先级（主用+兜底）';
  let cache = null;

  const pill = (st) => '<span class="pill ' + esc(st||'unknown') + '">' + esc(STATE_TEXT[st] || st || '未知') + '</span>';
  const availBadge = (m) => {
    if (!m.enabled) return '<span class="pill disabled">已停用</span>';
    if (m.routable === false) return '<span class="pill unknown">仅网关</span>';
    return pill(m.state);
  };

  // ── 概览 ──────────────────────────────────────────────
  // 2026-10-20 简化：原来「总览 6 格 + 车道水位表 + 模型清单 9 列 + 添加表单 12 项 + 日志」
  // 五块平铺，一屏看到的全是数字。现在首屏只有一行结论 + 4 个关键读数，
  // 车道水位并进总览卡（它本来只是「这几条车道各几个模型」），明细一律折叠。
  // fold 默认折叠；poolFold 默认展开（模型清单要一屏看全，折叠只是为了收起噪音组）。
  const fold = (title, body) => '<details class="fold"><summary>' + esc(title) + '</summary><div class="fold-body">' + body + '</div></details>';
  const poolFold = (title, body, open) => '<details class="fold"' + (open ? ' open' : '') + '><summary>' + esc(title) + '</summary><div class="fold-body">' + body + '</div></details>';
  const renderOverview = (d) => {
    const g = d.gateway || {};
    const lanes = d.lanes || [];
    const laneRows = lanes.map(l =>
      '<tr>' +
      '<td data-label="车道">' + esc(l.label || l.lane) + '</td>' +
      '<td data-label="总数">' + esc(l.total) + '</td>' +
      '<td data-label="可用">' + esc(l.available) + '</td>' +
      '<td data-label="下线">' + esc(l.down) + '</td>' +
      '<td data-label="停用">' + esc(l.disabled) + '</td>' +
      '</tr>'
    ).join('');
    const laneBody = '<div class="pool-table-wrap"><table class="pool-table" aria-label="车道汇总（按运行环境）"><thead><tr><th>车道</th><th>总数</th><th>可用</th><th>下线</th><th>停用</th></tr></thead><tbody>' +
      (laneRows || '<tr><td colspan="5" class="empty">暂无模型</td></tr>') + '</tbody></table></div>';
    const strategies = ['priority','round-robin','weighted','least-loaded'];
    const stratSel = '<select id="strategySel" aria-label="模型调度策略" style="max-width:220px">' + strategies.map(s => '<option value="' + s + '"' + (d.strategy===s?' selected':'') + '>' + stratName(s) + '</option>').join('') + '</select>';
    const lp = d.lastProbe || {};
    const gw = g.base ? (g.hasKey ? '已连接' : '缺密钥') : '未发现';
    document.getElementById('poolOverview').innerHTML =
      '<div class="card"><h3>总览</h3>' +
      '<div class="status-line"><span class="dot ' + ((d.available||0) > 0 ? 'on' : 'off') + '" aria-hidden="true"></span>' +
        '可用 <b>' + esc(d.available||0) + ' / ' + esc(d.total||0) + '</b> 个模型' +
        '<span class="sep" aria-hidden="true">·</span>网关 <b>' + esc(gw) + '</b>' +
        '<span class="sep" aria-hidden="true">·</span>策略 <b>' + esc(stratName(d.strategy)) + '</b>' +
        (lp.at ? '<span class="sep" aria-hidden="true">·</span>上次探测 <b>' + esc(fmtTime(lp.at)) + '</b>' : '') +
      '</div>' +
      // 网关读数分三级用词（2026-10-20 老大实报「网关错误：端口未监听...」）：
      //   · 端口没监听 = DSH 桌面端起停/任务 HOME 轮换的正常环境态，非缺陷 ⇒ 中性提示；
      //   · 端口在监听但拒绝鉴权 = 真需要处理（密钥不匹配）⇒ 红色；
      //   · 其余归为错误。
      // 旧版一律红字「网关错误」+ 裸 dial 字符串，把可自愈的环境态渲染成故障。
      (g.error ? '<div class="muted" style="margin-top:10px;' + (g.reachable === false ? '' : 'color:var(--danger)') + '">' + (g.reachable === false ? '网关未监听：' : (g.reachable === true && g.authorized === false ? '网关鉴权失败：' : '网关错误：')) + esc(g.error) + '</div>' : '') +
      '<div style="margin-top:14px"><label>调度策略</label>' + stratSel + '</div>' +
      fold('车道水位（' + lanes.length + ' 条车道）', laneBody) +
      '</div>';
    const sel = document.getElementById('strategySel');
    if (sel) sel.onchange = async () => {
      try { await api('/api/pool/strategy', { method:'POST', body: JSON.stringify({ strategy: sel.value }) }); toast('策略已切换为：' + stratName(sel.value)); load(); }
      catch (e) { toast('切换失败：' + e.message); }
    };
  };

  // ── 车道 ──────────────────────────────────────────────
  // 车道水位已并入「总览」卡的折叠块；本函数保留车道表的单一实现，
  // 由 pool_touch_contract_test.go 继续钉住「每格必须带 data-label」这条窄屏契约。
  const renderLanes = (d) => {
    const lanes = d.lanes || [];
    const rows = lanes.map(l =>
      '<tr>' +
      '<td data-label="车道">' + esc(l.label || l.lane) + '</td>' +
      '<td data-label="总数">' + esc(l.total) + '</td>' +
      '<td data-label="可用">' + esc(l.available) + '</td>' +
      '<td data-label="下线">' + esc(l.down) + '</td>' +
      '<td data-label="停用">' + esc(l.disabled) + '</td>' +
      '</tr>'
    ).join('');
    return '<div class="pool-table-wrap"><table class="pool-table" aria-label="车道汇总（按运行环境）"><thead><tr><th>车道</th><th>总数</th><th>可用</th><th>下线</th><th>停用</th></tr></thead><tbody>' +
      (rows || '<tr><td colspan="5" class="empty">暂无模型</td></tr>') + '</tbody></table></div>';
  };

  // ── 模型清单（按分组分类管理） ────────────────────────
  // 2026-10-20 重做：原来 36+ 个模型平铺成一张 9 列表，一屏全是行，
  // 看不出「哪条车道（渠道）健康」——这正是老大说的「乱七八糟」。
  // 现在按 group 分组，每组一张卡片：
  //   · 卡片头显示该组聚合状态（可用/总数、成功/失败、平均延迟）；
  //   · 组内保留原有 6 列明细表，操作按钮原样不动；
  //   · 组卡片可折叠（默认全展开），组序号连续，不打断「越靠上越先用」的语义。
  // 分组定义来自 Spec.Group（服务端已按 lane 归好：OFM 免费 / Kilo 免费池 /
  // EAC 协付 / 聚合渠道 / 自定义），前端不再自己推断。
  // 老大 2026-10-22 令：模型池只留 eac/kilo/ofm 等免费车道 —— 「网关专属车道」
  // 已从服务端同步入口摘除（sync.go 不再纳管）+ 存量由 PurgeGatewayLanes 清理，
  // 这里不再列该分组；万一账本里还有残留，会落到「未分组」追加区并被下一次同步清掉。
  const GROUP_ORDER = ['OFM 免费', 'Kilo 免费池', 'EAC 协付', '聚合渠道', '自定义'];
  // 各免费池一键开关：分组名 → 车道值（后端 SetPoolEnabled 两者都收，
  // 这里带上 lane 让「一键停用 OFM 免费」这类整池操作语义明确、不受改名影响）。
  const GROUP_LANE = { 'OFM 免费': 'free', 'Kilo 免费池': 'kilo', 'EAC 协付': 'eac', '聚合渠道': 'aggregate', '自定义': 'custom' };
  const groupTone = (stats) => {
    if (stats.total === 0) return 'unknown';
    const ratio = stats.available / stats.total;
    if (ratio >= 0.6) return 'healthy';
    if (ratio >= 0.3) return 'degraded';
    return 'down';
  };
  // 行的渲染与改造前逐字相同（契约测试按列名与 data-label 钉住，不能动）。
  const poolRow = (m, i, h) => {
    const note = esc(h.lastErr || m.note || '');
    const lat = fmtMs(h.latencyMs || m.latencyMs);
    return '<tr data-id="' + esc(m.id) + '">' +
      '<td data-label="#">' + (i+1) + '</td>' +
      '<td data-label="模型"><div>' + esc(m.label || m.id) + '</div>' +
        '<div class="muted">' + esc(m.id) + (note ? ' · ' + note.slice(0, 60) : '') + (lat !== '—' ? ' · ' + esc(lat) : '') + '</div></td>' +
      '<td data-label="优先级/权重">' + esc(m.priority) + ' / ' + esc(m.weight) + '</td>' +
      '<td data-label="状态">' + availBadge(m) + '</td>' +
      '<td data-label="成功/失败">' + esc(h.success||0) + ' / ' + esc(h.failure||0) + '</td>' +
      '<td data-label="操作" class="pool-ops"><div class="pool-row-actions">' +
        '<button data-act="up" data-id="' + esc(m.id) + '">↑</button>' +
        '<button data-act="down" data-id="' + esc(m.id) + '">↓</button>' +
        '<button data-act="' + (m.enabled?'disable':'enable') + '" data-id="' + esc(m.id) + '">' + (m.enabled?'停用':'启用') + '</button>' +
        '<button data-act="edit" data-id="' + esc(m.id) + '">编辑</button>' +
        '<button data-act="more" data-id="' + esc(m.id) + '" title="更多操作">⋯</button>' +
      '</div></td></tr>';
  };
  const renderList = (d) => {
    // 重绘前保住「⋯」弹层：renderList 会整体替换 #poolList 的 children，
    // 若此刻弹层正开着（尤其删除项处于两段式确认的 armed 态），重绘会把它连同
    // armed 元素一起销毁 —— 用户第二击无处可点，删除功能实际不可用
    // （2026-10-22 CDP MutationObserver 实测：开层后 ~600ms 一次 load() 落地即 wiped）。
    const keepPop = document.getElementById('rowMore');
    const keepOpen = keepPop && !keepPop.hidden;
    const specs = d.specs || [];
    const health = {};
    (d.health || []).forEach(h => { health[h.id] = h; });
    const box = document.getElementById('poolList');
    if (!specs.length) {
      box.innerHTML = '<div class="card"><h3>模型清单</h3>' +
        '<div class="empty-hint">还没有模型。点上方「同步网关清单」，或在下方折叠区里手动添加一个。</div></div>';
      return;
    }
    // 按 group 聚拢，顺序固定（GROUP_ORDER 优先，未知分组按出现顺序追加）。
    const buckets = new Map();
    specs.forEach(m => {
      const key = m.group || m.lane || '未分组';
      if (!buckets.has(key)) buckets.set(key, []);
      buckets.get(key).push(m);
    });
    const orderedKeys = [...GROUP_ORDER.filter(k => buckets.has(k)),
      ...[...buckets.keys()].filter(k => !GROUP_ORDER.includes(k))];
    // 组内独立编号：每组都从 1 开始。跨组连续编号会让第 2 组第一行显示「14」，
    // 读起来像「全池第 14 个」，而实际语义是「本组内第几个」——分组管理的重点是组内次序。
    const cards = orderedKeys.map(key => {
      const list = buckets.get(key);
      const stats = { total: list.length, available: 0, down: 0, disabled: 0, success: 0, failure: 0, latSum: 0, latN: 0 };
      list.forEach(m => {
        const h = health[m.id] || {};
        stats.success += h.success || 0;
        stats.failure += h.failure || 0;
        const lat = h.latencyMs || m.latencyMs;
        if (lat > 0) { stats.latSum += lat; stats.latN++; }
        if (!m.enabled) { stats.disabled++; return; }
        if (m.state === 'down') { stats.down++; return; }
        if (m.routable === false) { stats.disabled++; return; }
        stats.available++;
      });
      const tone = groupTone(stats);
      const avgLat = stats.latN ? fmtMs(Math.round(stats.latSum / stats.latN)) : '—';
      const THEAD = '<thead><tr><th>#</th><th>模型</th><th>优先级/权重</th><th>状态</th><th>成功/失败</th><th>操作</th></tr></thead>';
      const tableOf = rows => '<div class="pool-table-wrap"><table class="pool-table" aria-label="模型明细（按分组）">' + THEAD + '<tbody>' + rows + '</tbody></table></div>';
      // 组内再分两层：能用的（可用/降级/未探测）直接展开；停用/下线的折在下面。
      // OFM 组 13 个里有 10 个是「上游判定不可用」的自动停用项，混在一起会把
      // 真正能用的 3 个埋掉——这正是「乱七八糟」的具体来源。
      const live = list.filter(m => m.enabled && m.state !== 'down' && m.routable !== false);
      const idle = list.filter(m => !live.includes(m));
      // 序号列此前双重加一（调用方 i+1、poolRow 内再 i+1）⇒ 组内从 2 开始，走查实测缺陷。
      const liveRows = live.map((m, i) => poolRow(m, i, health[m.id] || {})).join('');
      const idleRows = idle.map((m, i) => poolRow(m, i, health[m.id] || {})).join('');
      const body = (live.length
          ? tableOf(liveRows)
          : '<div class="empty-hint" style="padding:10px 2px">本组没有可调度的模型——全部停用或下线。</div>') +
        (idle.length
          ? poolFold('另 ' + idle.length + ' 个停用/下线（点开查看或恢复）', tableOf(idleRows), false)
          : '');
      // 一键启用/一键停用（老大 2026-10-22 令）：整池批量开关，落 /api/pool/lane。
      // 停用是危险动作（该组全部模型立刻退出调度），复用既有 armConfirm 二次确认；
      // 全组已停用/已启用时按钮禁用，避免「点了没反应」的假象。
      const allOn = stats.disabled === 0 && stats.total > 0;
      const allOff = stats.available === 0 && stats.down === 0 && stats.total > 0 && stats.disabled === stats.total;
      const laneVal = GROUP_LANE[key] || '';
      return '<div class="card pool-group">' +
        '<div class="pool-group-head">' +
          '<span class="pill ' + tone + '">' + esc(key) + '</span>' +
          '<span class="pool-group-stat">可用 <b>' + stats.available + '</b>/' + stats.total + '</span>' +
          (stats.down ? '<span class="pool-group-stat warn">下线 <b>' + stats.down + '</b></span>' : '') +
          (stats.disabled ? '<span class="pool-group-stat dim">停用 ' + stats.disabled + '</span>' : '') +
          '<span class="pool-group-stat dim">成功/失败 ' + stats.success + '/' + stats.failure + '</span>' +
          (avgLat === '—' ? '' : '<span class="pool-group-stat dim">均延迟 ' + esc(avgLat) + '</span>') +
          '<span class="pool-group-actions">' +
            '<button data-gact="genable" data-group="' + esc(key) + '" data-lane="' + esc(laneVal) + '"' + (allOn ? ' disabled' : '') + '>一键启用</button>' +
            '<button data-gact="gdisable" data-group="' + esc(key) + '" data-lane="' + esc(laneVal) + '"' + (allOff ? ' disabled' : '') + '>一键停用</button>' +
          '</span>' +
        '</div>' +
        poolFold('收起 ' + list.length + ' 个模型', body, true) +
        '</div>';
    }).join('');
    box.innerHTML = '<div class="card"><h3>模型清单（按渠道分组 · 同组内越靠上越先用）</h3>' +
      '<div class="muted" style="margin-bottom:4px">' + orderedKeys.length + ' 个分组 / 共 ' + specs.length + ' 个模型。每组头部是该组的聚合健康度，点开看组内明细。</div>' +
      '</div>' + cards;
    if (keepOpen) { box.appendChild(keepPop); } // 元素本体连同 armed 状态一起搬回来
    box.querySelectorAll('button[data-act]').forEach(btn => {
      btn.onclick = () => onRowAction(btn.getAttribute('data-act'), btn.getAttribute('data-id'), d, btn);
    });
    box.querySelectorAll('button[data-gact]').forEach(btn => {
      btn.onclick = () => onGroupAction(btn.getAttribute('data-gact'), btn.getAttribute('data-group'), btn.getAttribute('data-lane'), btn);
    });
  };

  // 整池一键开关。enabled=false 走 armConfirm 二次确认（一次点错会让整条渠道退出调度）。
  const onGroupAction = async (gact, group, lane, btn) => {
    const enabled = gact === 'genable';
    const run = async () => {
      try {
        const body = { enabled };
        if (lane) body.lane = lane;
        if (group) body.group = group;
        const r = await api('/api/pool/lane', { method:'POST', body: JSON.stringify(body) });
        const t = r.toggle || {};
        toast('已一键' + (enabled ? '启用' : '停用') + '「' + group + '」：' + ((t.affected && t.affected.length) || 0) + ' 个模型生效' + (t.skipped ? '（' + t.skipped + ' 个已是该状态）' : ''));
        load();
      } catch (e) { toast('操作失败：' + e.message); }
    };
    if (!enabled) { armConfirm(btn, '再点确认停用整池', run); return; }
    await run();
  };

  const onRowAction = async (act, id, d, btn) => {
    try {
      if (act === 'up' || act === 'down') {
        const ids = (d.specs || []).map(x => x.id);
        const i = ids.indexOf(id);
        const j = act === 'up' ? i - 1 : i + 1;
        if (i < 0 || j < 0 || j >= ids.length) return;
        const tmp = ids[i]; ids[i] = ids[j]; ids[j] = tmp;
        await api('/api/pool/reorder', { method:'POST', body: JSON.stringify({ ids }) });
        toast('优先级已调整');
      } else if (act === 'del') {
        armConfirm(btn, '再点确认删除', async () => {
          await api('/api/pool/models/' + encodeURIComponent(id), { method:'DELETE' });
          toast('已删除 ' + id);
          load();
        });
      } else if (act === 'edit') {
        openEditor(d.specs.find(x => x.id === id));
        return;
      } else if (act === 'more') {
        const m = d.specs.find(x => x.id === id) || {};
        const box = document.getElementById('poolList');
        let pop = document.getElementById('rowMore');
        if (!pop) { pop = document.createElement('div'); pop.id = 'rowMore'; pop.className = 'topmore'; box.appendChild(pop); }
        pop.hidden = false;
        pop.innerHTML = '<a href="javascript:void 0" data-more="restore">恢复该模型</a>' + (m.enabled ? '' : '<a href="javascript:void 0" data-more="enable">启用</a>') +
          '<a href="javascript:void 0" data-more="del" style="color:var(--danger)">删除模型</a>';
        pop.querySelectorAll('a[data-more]').forEach(a => {
          a.onclick = () => {
            const act = a.getAttribute('data-more');
            // 删除走两段式确认：第一次点击不能收起弹层，否则 armed 态无处可点
            if (act !== 'del') { pop.hidden = true; }
            onRowAction(act, id, d, a);
          };
        });
        setTimeout(() => { pop.hidden = true; }, 6000);
        return;
      } else {
        await api('/api/pool/models/' + encodeURIComponent(id) + '/' + act, { method:'POST', body: '{}' });
        toast('已' + ({enable:'启用',disable:'停用',restore:'恢复'} [act] || act) + '：' + id);
      }
      load();
    } catch (e) { toast('操作失败：' + e.message); }
  };

  // ── 添加/编辑（就地展开，不弹窗） ──────────────────────
  // 2026-10-20 简化：12 个字段的表单原来是常显的，占掉整整一屏。
  // 改为「一个按钮 + 折叠表单」：默认收起；高级参数（权重/失败阈值/冷却）再收一层。
  let editing = null;
  let addOpen = false;
  // 内联编辑器的键盘可达性（2026-10-22 走查实测缺口）：此前只有鼠标点「取消」
  // 能退出，键盘用户按 Escape 无反应、且关闭后焦点丢失（a11y 上「可关闭」与「焦点回退」
  // 是成对要求）。这里记下触发元素，Escape 关闭后把焦点还回去。
  let editorReturnFocus = null;
  const openEditor = (m) => { editing = m || null; addOpen = true; editorReturnFocus = document.activeElement; renderAdd(); const el = document.getElementById('poolAdd'); if (el) el.scrollIntoView({behavior:'smooth'}); };
  const renderAdd = () => {
    const box = document.getElementById('poolAdd');
    if (!box) { return; }
    if (!addOpen && !editing) {
      box.innerHTML = '<div class="card"><h3>添加模型</h3>' +
        '<div class="status-line">只要是 OpenAI / Anthropic 兼容的模型接口，都能接进来 —— 填地址和密钥即可。</div>' +
        '<div class="toolbar" style="margin-top:12px"><button class="btn primary" id="btnAddOpen">添加一个模型接口</button></div></div>';
      const b = document.getElementById('btnAddOpen');
      if (b) b.onclick = () => { addOpen = true; renderAdd(); };
      return;
    }
    const m = editing || { format:'openai-chat', tier:'balanced', lane:'custom', priority:100, weight:50, maxFail:3, cooldownSec:120, enabled:true, routable:true };
    const isEdit = !!editing;
    const baseFields = '<div class="pool-inline">' +
        '<label>模型 ID（唯一）<input id="fId" value="' + esc(m.id||'') + '" ' + (isEdit?'disabled':'') + ' placeholder="如 my-endpoint/llama-3"></label>' +
        '<label>显示名<input id="fLabel" value="' + esc(m.label||'') + '" placeholder="可空"></label>' +
        '<label>上游模型名<input id="fModel" value="' + esc(m.model||'') + '" placeholder="如 gpt-4o-mini"></label>' +
        '<label>接口地址（BaseURL）<input id="fBase" value="' + esc(m.baseUrl||'') + '" placeholder="https://api.example.com/v1（留空 = 用本机内置网关）"></label>' +
        '<label>API Key<input id="fKey" type="password" placeholder="' + (isEdit && m.hasKey ? '已配置 ' + esc(m.keyHint||'') + '，留空不修改' : '自定义端点必填') + '"></label>' +
        '<label>接口类型<select id="fFormat">' +
          [['openai-chat','OpenAI 标准对话（最常见）'],['openai-responses','OpenAI Responses（新版）'],['anthropic-messages','Anthropic 格式（Claude 系）']].map(f => '<option value="' + f[0] + '"' + (m.format===f[0]?' selected':'') + '>' + f[1] + '</option>').join('') +
        '</select></label>' +
      '</div>';
    const advFields = '<div class="pool-inline">' +
        '<label>线路来源<select id="fLane">' +
          [['custom','我自己填的接口'],['free','工作台自带的免费通道'],['kilo','Kilo 通道'],['eac','EAC 通道'],['aggregate','聚合通道']].map(x => '<option value="' + x[0] + '"' + (m.lane===x[0]?' selected':'') + '>' + x[1] + '</option>').join('') +
        '</select></label>' +
        '<label>用途偏好<select id="fTier">' +
          [['quality','质量优先（慢一点，写得好）'],['balanced','均衡（默认）'],['fast','速度优先（快，短任务用）']].map(t => '<option value="' + t + '"' + (m.tier===t?' selected':'') + '>' + t[1] + '</option>').join('') +
        '</select></label>' +
        '<label>优先级（1=最高）<input id="fPri" type="number" min="1" value="' + esc(m.priority||100) + '"></label>' +
        '<label>权重<input id="fWeight" type="number" min="1" value="' + esc(m.weight||50) + '"></label>' +
        '<label>连续失败阈值<input id="fMaxFail" type="number" min="1" value="' + esc(m.maxFail||3) + '"></label>' +
        '<label>冷却秒数<input id="fCool" type="number" min="10" value="' + esc(m.cooldownSec||120) + '"></label>' +
      '</div>';
    box.innerHTML = '<div class="card"><h3>' + (isEdit ? '编辑模型 · ' + esc(m.id) : '添加模型') + '</h3>' +
      baseFields +
      '<div style="margin-top:14px">' + fold('高级参数（线路 / 用途偏好 / 优先级 / 权重 / 阈值）', advFields) + '</div>' +
      '<div class="pool-row-actions" style="margin-top:14px">' +
        '<button class="btn primary" id="btnSave">' + (isEdit ? '保存' : '添加') + '</button>' +
        '<button class="btn ghost" id="btnCancel">取消</button>' +
        '<span class="muted">密钥只写本机，不对外回显。</span>' +
      '</div></div>';
    document.getElementById('btnCancel').onclick = () => { editing = null; addOpen = false; renderAdd(); };
    document.getElementById('btnSave').onclick = async () => {
      const body = {
        id: document.getElementById('fId').value.trim(),
        label: document.getElementById('fLabel').value.trim(),
        model: document.getElementById('fModel').value.trim(),
        baseUrl: document.getElementById('fBase').value.trim(),
        apiKey: document.getElementById('fKey').value,
        format: document.getElementById('fFormat').value,
        lane: document.getElementById('fLane').value,
        tier: document.getElementById('fTier').value,
        priority: Number(document.getElementById('fPri').value) || 100,
        weight: Number(document.getElementById('fWeight').value) || 50,
        maxFail: Number(document.getElementById('fMaxFail').value) || 3,
        cooldownSec: Number(document.getElementById('fCool').value) || 120,
        enabled: true, routable: true,
      };
      try {
        if (isEdit) await api('/api/pool/models/' + encodeURIComponent(body.id), { method:'PUT', body: JSON.stringify(body) });
        else await api('/api/pool/models', { method:'POST', body: JSON.stringify(body) });
        toast(isEdit ? '已保存' : '已添加模型 ' + body.id);
        editing = null; addOpen = false; load();
      } catch (e) { toast('保存失败：' + e.message); }
    };
  };

  // ── 状态日志 ──────────────────────────────────────────
  const renderLog = (d) => {
    const lines = d.statusLog || [];
    const body = lines.length ? lines.slice().reverse().map(l => {
      let rec = null; try { rec = JSON.parse(l); } catch (e) { return esc(l); }
      const t = fmtTime(rec.at);
      const kind = rec.kind || '';
      const who = rec.model || rec.state || '';
      const fromTo = rec.from || rec.to ? ('  ' + (rec.from||'?') + ' → ' + (rec.to||'?')) : '';
      return '<div>' + esc(t) + '  [' + esc(kind) + '] ' + esc(who) + esc(fromTo) + (rec.reason ? '  ' + esc(rec.reason) : '') + '</div>';
    }).join('') : '<div class="muted">暂无状态日志</div>';
    document.getElementById('poolLog').innerHTML =
      '<div class="card"><h3>状态日志</h3>' +
      '<div class="muted">探测、故障转移、恢复、增删改都会留一行，便于排查「刚才为什么换了模型」。</div>' +
      '<div style="margin-top:10px">' + fold('最近 ' + lines.length + ' 条记录（新 → 旧）', '<div class="logbox">' + body + '</div>') + '</div></div>';
  };

  // ── 加载与动作 ────────────────────────────────────────
  const load = async () => {
    try {
      const d = await api('/api/pool');
      cache = d;
      renderOverview(d); renderList(d); renderAdd(); renderLog(d);
      const g = d.gateway || {};
      document.getElementById('poolStat').textContent =
        '可用 ' + (d.available||0) + '/' + (d.total||0) + ' ｜ 策略 ' + stratName(d.strategy) + ' ｜ 网关 ' + (g.base ? '在线' : '未连接');
    } catch (e) {
      document.getElementById('poolStat').textContent = '加载失败：' + e.message;
      toast('加载失败：' + e.message);
    }
  };

  document.getElementById('btnRefresh').onclick = () => load();
  document.getElementById('btnSync').onclick = async () => {
    const btn = document.getElementById('btnSync'); btn.disabled = true; btn.textContent = '同步中…';
    try {
      const r = await api('/api/pool/sync', { method:'POST', body:'{}' });
      const s = r.sync || {};
      toast('同步完成：新增 ' + (s.added||0) + ' / 更新 ' + (s.updated||0) + ' / 跳过 ' + (s.skipped||0));
      load();
    } catch (e) { toast('同步失败：' + e.message); }
    finally { btn.disabled = false; btn.textContent = '同步网关清单'; }
  };
  document.getElementById('btnProbe').onclick = async () => {
    const btn = document.getElementById('btnProbe'); btn.disabled = true; btn.textContent = '探测中…';
    try {
      const r = await api('/api/pool/probe', { method:'POST', body:'{}' });
      const p = r.probe || {};
      toast('探测完成：可用 ' + (p.ok||0) + ' / 下线 ' + (p.down||0) + ' / 未知 ' + (p.unknown||0));
      load();
    } catch (e) { toast('探测失败：' + e.message); }
    finally { btn.disabled = false; btn.textContent = '立即探测'; }
  };

  // 「更多」菜单（跨页入口收纳）
  const btnMore = document.getElementById('btnMore');
  const topMore = document.getElementById('topMore');
  if (btnMore && topMore) {
    btnMore.onclick = function () {
      const willOpen = topMore.hidden;
      topMore.hidden = !willOpen;
      btnMore.setAttribute('aria-expanded', willOpen ? 'true' : 'false');
    };
    document.addEventListener('click', function (ev) {
      if (!topMore.hidden && ev.target !== btnMore && !topMore.contains(ev.target)) {
        topMore.hidden = true; btnMore.setAttribute('aria-expanded', 'false');
      }
    });
    // Escape 必须能关掉菜单：键盘用户用 Enter 打开后，鼠标外点逻辑对他们不存在。
    // （2026-10-24 补齐：appJS 早已有这段，本份漏抄 —— 三份内嵌脚本是各自独立的副本，
    //   一份里修的另两份不会自动跟上；同型缺陷见 esc 单引号映射那次。）
    document.addEventListener('keydown', function (ev) {
      if (ev.key === 'Escape' && !topMore.hidden) {
        topMore.hidden = true; btnMore.setAttribute('aria-expanded', 'false'); btnMore.focus();
      }
    });
  }

  // Escape 关闭池内联编辑器（新增/编辑同一入口），并把焦点退回触发按钮。
  // 与顶栏「更多」菜单的 Escape 处理同型：键盘用户没有「点空白处关闭」这条路径。
  const closePoolEditor = () => {
    editing = null; addOpen = false; renderAdd();
    const back = editorReturnFocus;
    editorReturnFocus = null;
    if (back && document.contains(back) && typeof back.focus === 'function') {
      try { back.focus(); } catch (e) { /* 忽略：已从文档移除 */ }
    }
  };
  document.addEventListener('keydown', function (ev) {
    if (ev.key !== 'Escape') { return; }
    if (!editing && !addOpen) { return; }
    ev.preventDefault();
    closePoolEditor();
  });

  // Escape 与「点别处」都要能否定待确认态：原生 confirm 只有两个按钮，
  // 两段式若不给退出通道，误点一次就会留下一个 4 秒的「假按钮」。
  document.addEventListener('keydown', function (ev) {
    if (ev.key === 'Escape') { document.querySelectorAll('[data-armed="1"]').forEach(disarmArm); }
  });
  document.addEventListener('click', function (ev) {
    const armed = document.querySelector('[data-armed="1"]');
    if (armed && armed !== ev.target && !armed.contains(ev.target)) { disarmArm(armed); }
  }, true);

  load();
  setInterval(load, 15000);
})();
`
