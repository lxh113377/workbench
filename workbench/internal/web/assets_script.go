package web

// appJS 是管理面板与首页的前端脚本（零框架、零构建，随 EXE 内嵌）。
// 约定：Go 原始字符串内不得出现反引号；需要匹配反引号时用 String.fromCharCode(96)。
const appJS = `(() => {
  const TOKEN = window.__WB_TOKEN__ || '';
  const isAdmin = document.body.classList.contains('admin');
  // opts.timeoutMs 是**可选**的：不传就不建 AbortController，行为与改动前逐字节一致 ——
  // 因为 /api/pool/probe 这类接口是合法慢的（probeSpecs 串行遍历 base，每个 client 超时 10s，
  // 整体可达 N×10s）。给所有调用一刀切设超时会把能用的功能改坏，那比原缺陷更糟。
  // 目前只有两个高频轮询器（ping / refreshTop）显式启用它。
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
  // 两段式就地确认（armed confirm）—— 取代原生 confirm()。
  // 为什么换（2026-10-22 走查）：项目偏好是「禁弹窗、就地展开」，而原生 confirm 是系统灰框，
  // 在暗色 HUD 界面里既阻塞界面又脱离设计体系；同时它不可样式化、不可自动化。
  // 交互范式：第一次点击 → 按钮自身变成「再点一次确认」态（文案 + danger 配色），
  // 第二次点击才真正执行；4s 无操作自动回退，Escape 也可回退（键盘可达性成对要求）。
  const ARM_MS = 4000;
  const disarmArm = (btn) => {
    if (!btn || btn.dataset.armed !== '1') { return; }
    clearTimeout(btn._armTimer);
    if (btn.dataset.label0 != null) { btn.textContent = btn.dataset.label0; }
    delete btn.dataset.armed;
    btn.classList.remove('armed');
  };
  const armConfirm = (btn, confirmText, run) => {
    if (!btn) { return; }
    if (btn.dataset.armed === '1') { disarmArm(btn); run(); return; }
    document.querySelectorAll('[data-armed="1"]').forEach(disarmArm); // 同时只允许一个待确认
    btn.dataset.label0 = btn.textContent;
    btn.dataset.armed = '1';
    btn.textContent = confirmText;
    btn.classList.add('armed');
    btn._armTimer = setTimeout(() => disarmArm(btn), ARM_MS);
  };
  const toast = (msg) => { const el = document.getElementById('toast'); if (!el) { console.log(msg); return; } el.textContent = msg; el.classList.add('show'); clearTimeout(el._t); el._t = setTimeout(() => el.classList.remove('show'), 3000); };
  const stateCls = (s) => 'state-' + String(s || '');
  const STATE_TEXT = {candidate:'候选',evaluating:'评估中',rejected:'已拒',approved:'已批',queued:'排队',running:'执行中',done:'已完成',failed:'失败',timeout:'超时',cancelled:'已取消',interrupted:'中断'};
  const kpi = (l, v) => '<div class="kpi"><div class="v">' + esc(v) + '</div><div class="l">' + esc(l) + '</div></div>';
  // 领域列中文化：表格不再裸显 code/content 这类内部枚举（老大不是技术人员，行话回潮判红）。
  const DOMAIN_TEXT = { content: '内容创作', code: '代码生成', knowledge: '知识整理', data: '数据处理', solution: '方案撰写', report: '报告生成', quality: '工程质量', frontend: '前端工程' };
  const domainText = (d) => DOMAIN_TEXT[d] || d || '-';
  const fmtTime = (ms) => ms ? new Date(ms).toLocaleString('zh-CN', {hour12:false}) : '—';
  const fmtMin = (ms) => ms ? (ms/60000).toFixed(1) + ' 分钟' : '—';
  const fmtBytes = (b) => { if(!b) return '0 B'; const u=['B','KB','MB','GB']; let i=0,v=b; while(v>=1024&&i<u.length-1){v/=1024;i++;} return v.toFixed(i?1:0)+' '+u[i]; };

  // ── 首页：一个自动按钮（自动加载 + 自动响应 + 状态反馈）──
  if (!isAdmin) {
    const btn = document.getElementById('startBtn');
    if (!btn) { return; }
    const label = btn.querySelector('.btn-label');
    const live = document.getElementById('homeStatus');
    // 文案必须与 Go 侧 phaseLabel/phaseHint 逐字一致（服务端首帧渲染的就是那一套；
    // 不一致会出现「首帧写工作台、脚本一跑变成运行中」的闪变，读数也就跟着漂）。
    const PHASES = {
      idle:    { text: '开始',   hint: '待启动，点击开始',         busy: false, lock: false },
      // busy 不锁按钮（点击吞没取证）：disabled 后 click 根本不派发，启动窗口内点它＝白点。
      // 防重入由 inflight + pendingEnter 承担。
      busy:    { text: '启动中', hint: '正在启动引擎',             busy: true,  lock: false },
      // lock:false —— 按钮是活的，且点击即进入主功能子界面。
      running: { text: '工作台', hint: '工作台运行中，点击进入',   busy: false, lock: false },
      paused:  { text: '已暂停', hint: '已暂停，点击继续并进入',   busy: false, lock: false },
      stopped: { text: '重启', hint: '已停止，点击重启', busy: false, lock: false },
      error:   { text: '重试',     hint: '启动失败，可重试', busy: false, lock: false },
    };
    let current = btn.getAttribute('data-state') || 'idle';
    let inflight = false;
    // 在途点击的「进入」意图：启动请求还在路上时点按钮不能白点，
    // 记下意图，等启动收敛后补一次进入（吞请求不吞意图）。
    let pendingEnter = false;
    const paint = (state) => {
      const p = PHASES[state] || PHASES.error;
      current = state;
      btn.setAttribute('data-state', state);
      btn.setAttribute('aria-busy', p.busy ? 'true' : 'false');
      btn.disabled = p.lock;
      btn.title = p.hint;
      if (label) { label.textContent = p.text; }
      if (live) { live.textContent = p.hint; }
    };
    // 暂停/继续：点完立刻反映到按钮（乐观更新），不等 4s 轮询 ——
    // CDP 实测过：只发请求不 paint，按钮会在「已暂停」上多停 2 秒以上，看起来像没反应。
    // 失败则回滚成真实阶段，避免出现「按钮说已暂停、引擎其实还在跑」的假状态。
    const pauseHome = async () => {
      paint('paused');
      try { await api('/api/engine/pause', { method: 'POST', body: '{}' }); toast('已暂停'); }
      catch (e) { paint('running'); toast('暂停失败：' + e.message); }
    };
    const resumeHome = async () => {
      paint('running');
      try { await api('/api/engine/resume', { method: 'POST', body: '{}' }); toast('已继续'); }
      catch (e) { paint('paused'); toast('继续失败：' + e.message); }
    };
    let fails = 0;
    // 重入守卫：ping 每 4s 一次且同样可能排队（见 refreshTop 的说明）。守卫必须与
    // api() 的超时**配套** —— 只加守卫不加超时，一次永久挂起就会让轮询永久停摆（比现状更糟）。
    let pingBusy = false;
    const ping = async () => {
      if (pingBusy) { return; }
      pingBusy = true;
      try {
        const st = await api('/api/status', { timeoutMs: 8000 });
        fails = 0;
        const phase = (st && st.phase) || 'idle';
        paint(PHASES[phase] ? phase : 'idle');
      } catch (e) {
        fails += 1;
        if (current !== 'busy' && current !== 'error') { paint(fails >= 3 ? 'error' : current); }
      }
      finally { pingBusy = false; }
    };
    const start = async (enter) => {
      if (inflight) {
        // 在途点击：不吞意图，启动成功后补一次进入。
        if (enter) { pendingEnter = true; }
        return;
      }
      inflight = true;
      paint('busy');
      try {
        await api('/api/engine/start', { method: 'POST', body: '{}' });
        paint('running');
        toast('引擎已启动，正在自动产出');
        if (enter || pendingEnter) { pendingEnter = false; enterPanel(); }
      } catch (e) {
        pendingEnter = false;
        paint('error');
        toast('启动失败：' + e.message);
        setTimeout(() => { if (current === 'error') { ping(); } }, 2500);
      } finally {
        inflight = false;
      }
    };
    // 交互语义（目标要求 1）：点它就进主功能子界面。
    //   - 未运行：启动引擎，成功后自动打开管理面板（唯一一次自动进入）；
    //   - 已运行：直接打开管理面板；
    //   - 已暂停：先继续再进。
    // 实测教训（本轮返工）：运行/暂停态曾被锁成 disabled，按钮写着「运行中/已暂停」却永远点不动。
    let entered = false;
    const enterPanel = () => {
      if (entered) { return; }
      entered = true;
      // 延迟 2.2s 才真正跳转：一是让「启动中 → 运行中」的状态反馈能被看见与截图，
      // 二是给确定性探针留出取数窗口（实测教训：立刻跳走会让点按钮的探针全部 unverified）。
      setTimeout(function () { window.location.href = '/admin?token=' + encodeURIComponent(TOKEN); }, 2200);
    };
    const clickHandler = () => {
      const st = btn.getAttribute('data-state');
      if (st === 'running') { enterPanel(); return; }
      if (st === 'paused') { resumeHome(); enterPanel(); return; }
      start(true);
    };
    btn.addEventListener('click', clickHandler);
    // 自动加载：脚本一执行就进入加载态（不等任何网络返回）；
    // 随后由 /api/status 自动收敛到真实阶段。
    paint('busy');
    ping();
    setInterval(ping, 4000);
    // 自动触发：开屏发现引擎未在运行即自动启动一次（仅一次，失败不循环）。
    // 注意 start(false)：自动启动**不**跳子界面 —— 「点击后进入」是人的动作结果，
    // 页面一加载就把人甩进管理面板属于越权，也会让首页的确定性探针全变成未测成。
    setTimeout(() => { if (current === 'idle' || current === 'stopped') { start(false); } }, 900);
    return;
  }

  // ── 管理面板 ──────────────────────────────────────────
  // 信息架构（2026-10-20 简化）：管理面板只留 5 个页签 —— 总览 / 任务 / 产出 / 模板 / 设置。
  // 次级信息（模型健康度、运行时指标、失败分布、评估权重、工作目录、审计日志）
  // 一律收进 <details class="fold"> 折叠块：默认只给「结论 + 关键读数」，
  // 用户点开才铺明细。原 10 页签把 40+ 个数字平铺在一屏，是老大说「乱七八糟」的主因。
  const panes = {
    overview: document.getElementById('tab-overview'),
    queue: document.getElementById('tab-queue'),
    outputs: document.getElementById('tab-outputs'),
    templates: document.getElementById('tab-templates'),
    settings: document.getElementById('tab-settings'),
  };
  const cache = { status: null, tasks: [], templates: [], config: null };

  // 折叠块：标题行就是「这一块是什么」，展开才看明细。
  const fold = (title, body) => '<details class="fold"><summary>' + esc(title) + '</summary><div class="fold-body">' + body + '</div></details>';
  const emptyHint = (txt) => '<div class="empty-hint">' + esc(txt) + '</div>';

  let overviewAnimated = true; // 首帧播 KPI 入场，渲染一次后置 false（SSE 刷新不再重放）
  async function renderOverview() {
    const st = await api('/api/status');
    cache.status = st;
    const s = st.stats || {};
    const phaseText = { running: '运行中', paused: '已暂停', stopped: '已停止', idle: '待启动' }[st.phase] || st.phase;
    const models = st.models || [];

    // 首屏只放 4 个「用户真的关心」的读数：今天产出多少、还有多少在排队、成功率、总共完成多少。
    // 入场错峰动画只属于「开机那一帧」：SSE 每来一条事件都会重建这段 DOM，
    // 若动画照播，读数就会随事件流反复淡出淡入（走查实测：截图抓到半透明 KPI）。
    const head = '<div class="grid c4' + (overviewAnimated ? '' : ' no-anim') + '">' +
      kpi('今日产出', s.todayDone || 0) +
      kpi('排队中', s.queuedTasks || 0) +
      // 成功率：分母（完成任务数）为 0 时显示「—」而非 0.0% —— 后者把「还没跑过」读成「全失败」。
      kpi('成功率', s.doneTasks ? (s.successRate || 0).toFixed(1) + '%' : '—') +
      kpi('累计完成', s.doneTasks || 0) +
      '</div>';

    const modelBody = models.length
      ? '<table aria-label="模型健康度明细"><thead><tr><th>模型</th><th>层级</th><th>成功/失败</th><th>状态</th></tr></thead><tbody>' +
        models.map(function (m) {
          const cd = m.cooldownTo && m.cooldownTo > Date.now();
          const badge = cd ? '<span class="tag">冷却中</span>' : '<span class="state-done">可用</span>';
          return '<tr><td data-label="模型">' + esc(m.model) + '</td><td data-label="层级">' + esc(m.tier || '-') +
            '</td><td>' + (m.success || 0) + ' / ' + (m.failure || 0) + '</td><td>' + badge + '</td></tr>';
        }).join('') + '</tbody></table>'
      : emptyHint('还没有可用模型。到「更多 → 模型池」里同步或添加一个。');

    // 失败类别中文标签（round 66 补全）：此前手抄 10 项，真实枚举已 17 项 —— 缺的那些在面板上
    // 裸显英文原值（下一行的 kindText[b.kind] || b.kind 兜底），运维看到的是 rate_limited/no_artifact
    // 这种半中半英的桶名。对齐口径由 TestFailureLabelsCoverBaselineKinds 机器守护（漏一个判红）。
    const kindText = { timeout: '超时', upstream_failed: '上游失败', bad_exit: '异常退出', stalled: '卡死', unknown: '未知',
      missing_credential: '缺凭据', model_unavailable: '模型下线', boot_failed: '启动失败', cancelled: '已取消', done: '成功',
      not_served: '出口不服务', no_artifact: '无合规产物', no_lane: '池内无可用车道', rate_limited: '上游限流',
	quota_exhausted: '免费额度耗尽',
      transport_failed: '网络传输故障', stream_cut: '流被切断', pool_wait_capped: '环境等待封顶判死',
      panic: '引擎内异常', transition_failed: '状态迁移失败',
      crash_loop: '崩溃循环判死', verify_failed: '验收未通过待返工', replan: '返工用尽转重规划' };
    const failRes = await api('/api/failures').catch(function () { return { buckets: [], total: 0, empty: true }; });
    const failBody = failRes.empty
      ? emptyHint('没有失败任务 —— 保持这样最好。')
      : '<table aria-label="失败原因分布"><thead><tr><th>原因</th><th>次数</th><th>占比</th></tr></thead><tbody>' +
        (failRes.buckets || []).map(function (b) {
          const pct = Math.round(100 * b.count / Math.max(1, failRes.total));
          return '<tr><td data-label="原因">' + esc(kindText[b.kind] || b.kind) + '</td><td data-label="次数">' + b.count + '</td><td data-label="占比" class="num">' + pct + '%</td></tr>';
        }).join('') + '</tbody></table>';

    const metRes = await api('/api/metrics').catch(function () { return null; });
    const m = metRes || {};
    const metBody = '<div class="kv">' +
      '<div class="item"><div class="v">' + (m.heapMB || 0).toFixed(1) + ' MB</div><div class="l">内存</div></div>' +
      '<div class="item"><div class="v">' + (m.goroutine || 0) + '</div><div class="l">协程</div></div>' +
      '<div class="item"><div class="v">' + (m.handles || 0) + '</div><div class="l">句柄</div></div>' +
      '<div class="item"><div class="v">' + (m.numGC || 0) + '</div><div class="l">GC 次数</div></div>' +
      '</div>';

    const statusLine = '<div class="status-line"><span class="dot ' + (st.phase === 'running' ? 'on' : 'off') + '" aria-hidden="true"></span>' +
      '引擎 <b>' + esc(phaseText) + '</b><span class="sep" aria-hidden="true">·</span>正在执行 <b>' + (s.runningTasks || 0) + '</b>' +
      '<span class="sep" aria-hidden="true">·</span>平均耗时 <b>' + (s.avgSeconds || 0).toFixed(0) + ' 秒</b>' +
      '<span class="sep" aria-hidden="true">·</span>验收率 <b>' + (s.doneTasks ? (100 * (s.verifiedTasks || 0) / s.doneTasks).toFixed(1) : '0.0') + '%</b></div>';

    // 停摆自证（2026-10-21 现场）：用户看到的是「引擎运行中 / 今日产出 0 / 排队 0」，
    // 三个读数互相矛盾却说不出原因。服务端已把原因归纳成稳定原因码（stallReason），
    // 这里把它顶到状态行上方 —— 有产出时这一行不出现，不制造噪音。
    const stall = st.stallReason || {};
    const stallBanner = stall.code
      ? '<div class="stall-banner"><span class="stall-icon" aria-hidden="true">!</span><div><b>' + esc(stall.text || '当前没有产出') + '</b>' +
        (stall.detail ? '<div class="muted">' + esc(stall.detail) + '</div>' : '') + '</div></div>'
      : '';

    panes.overview.innerHTML = head +
      '<div class="card" style="margin-top:16px"><h3>系统状态</h3>' + stallBanner + statusLine +
        fold('模型健康度（' + models.length + ' 个）', modelBody) +
        fold('运行时资源', metBody) +
        fold('失败原因（最近 ' + (failRes.total || 0) + ' 条）', failBody) +
        (st.lastError ? fold('最近一次错误', '<div class="muted">' + esc(st.lastError) + '</div>') : '') +
      '</div>';
    overviewAnimated = false;
  }

  // 任务页（2026-10-20 二轮反馈：三张卡里两张只有「没有」，页面空得像坏了）。
  // 新结构：一行状态摘要 + 只有真有事才出现的表 + 一个明确的行动入口。
  // 全空时不再摆两张空卡，而是把「自己加一个任务」的表单直接摊开 —— 那是此刻唯一能做的事。
  async function renderQueue() {
    // 与产出页同型（2026-10-22 规模实测）：原来拉 limit=600 再前端过滤 queued/running/approved。
    // 当前窗口还能捞到 12 条 queued 属侥幸；总量再涨或短时大量 rejected 时同样会被挤出，
    // 界面会显示「没有任务」而库存里明明有。改按状态分别取（后端 handleTasks 本就支持）。
    // 同时取一份 status（2026-10-22 现场缺陷）：本页「今日产出」读的是 cache.status.stats.todayDone，
    // 而 cache.status 只在 renderOverview() 里赋值 —— 直接从 #queue 进页面时 overview 从未渲染，
    // cache.status 永远 undefined，于是「今日产出」恒显示 0（实测实际 8）。本页自己取，不依赖别的页。
    const [queueRes, statusRes, queuedRes, runningRes, approvedRes] = await Promise.all([
      api('/api/queue'),
      api('/api/status').catch(function () { return null; }),
      api('/api/tasks?state=queued&limit=600').catch(function () { return { tasks: [] }; }),
      api('/api/tasks?state=running&limit=600').catch(function () { return { tasks: [] }; }),
      api('/api/tasks?state=approved&limit=600').catch(function () { return { tasks: [] }; }),
    ]);
    if (statusRes) { cache.status = statusRes; }

    const active = (queuedRes.tasks || []).concat(runningRes.tasks || [], approvedRes.tasks || []);
    cache.tasks = active;
    const q = queueRes.tasks || [];
    const running = runningRes.tasks || [];
    const nothing = q.length === 0 && active.length === 0;

    const rows = q.map(function (t) {
      const badge = '<span class="' + stateCls(t.state) + '">' + (STATE_TEXT[t.state] || t.state) + '</span>';
      return '<tr><td data-label="标题">' + esc(t.title) + '</td><td data-label="领域">' + esc(domainText(t.domain)) + '</td><td class="num" data-label="优先级">' + (t.priority || 0) + '</td><td data-label="状态">' + badge + '</td>' +
        '<td class="ops"><button class="btn sm" onclick="WB.priority(&apos;' + t.id + '&apos;)">置顶</button>' +
        '<button class="btn sm" onclick="WB.cancel(&apos;' + t.id + '&apos;)">终止</button></td></tr>';
    }).join('');
    const activeRows = active.slice(0, 80).map(function (t) {
      const badge = '<span class="' + stateCls(t.state) + '">' + (STATE_TEXT[t.state] || t.state) + '</span>';
      const ops = (t.state === 'running' ? '<button class="btn sm" onclick="WB.cancel(&apos;' + t.id + '&apos;)">终止</button>' : '') +
        '<button class="btn sm" onclick="WB.openTask(&apos;' + t.id + '&apos;)">详情</button>';
      return '<tr><td data-label="标题">' + esc(t.title) + '</td><td data-label="状态">' + badge + '</td><td data-label="更新时间">' + fmtTime(t.startedAt || t.updatedAt) + '</td><td data-label="操作" class="ops">' + ops + '</td></tr>';
    }).join('');

    const addBody = '<label>标题</label><input id="mTitle" placeholder="例如：整理本周 AI 工具评测">' +
      '<label>任务描述</label><textarea id="mPrompt" rows="4" placeholder="描述要产出的成果、格式与要求"></textarea>' +
      '<label for="mDomain">领域</label><select id="mDomain"><option value="content">内容创作</option><option value="code">代码生成</option><option value="knowledge">知识整理</option><option value="data">数据处理</option><option value="solution">方案撰写</option><option value="report">报告生成</option></select>' +
      '<div class="toolbar" style="margin-top:12px"><button class="btn primary" onclick="WB.manual()">评估并加入队列</button></div>';

    // 状态摘要：一行看完「现在有没有活、干到哪」
    const head = '<div class="status-line"><span class="dot ' + (running.length ? 'on' : 'off') + '" aria-hidden="true"></span>' +
      '排队 <b>' + q.length + '</b><span class="sep" aria-hidden="true">·</span>执行中 <b>' + running.length + '</b>' +
      '<span class="sep" aria-hidden="true">·</span>今日产出 <b>' + ((cache.status && cache.status.stats && cache.status.stats.todayDone) || 0) + '</b></div>';

    let body;
    if (nothing) {
      // 全空：一张卡说清楚「为什么空」+ 直接把能做的事摊开，不再叠两张空卡
      body = '<div class="card"><h3>当前没有任务</h3>' + head +
        '<div class="empty-hint" style="margin-top:14px">引擎运行时会自动从模板生成任务，不需要你手动加。<br>' +
        '如果你想马上让它做一件具体的事，在下面写清楚要什么，点一下就入队。</div>' + addBody + '</div>';
    } else {
      body = '<div class="card"><h3>任务</h3>' + head +
        (rows ? '<div class="table-scroll" style="margin-top:14px"><table aria-label="排队任务列表"><thead><tr><th>标题</th><th>领域</th><th>优先级</th><th>状态</th><th>操作</th></tr></thead><tbody>' + rows + '</tbody></table></div>'
          : '<div class="muted" style="margin-top:12px">排队为空 —— 引擎会继续从模板生成。</div>') +
        (activeRows ? '<h4 style="margin-top:20px">执行中 / 待执行</h4><div class="table-scroll"><table aria-label="执行中与待执行任务"><thead><tr><th>标题</th><th>状态</th><th>更新时间</th><th>操作</th></tr></thead><tbody>' + activeRows + '</tbody></table></div>' : '') +
        '<div style="margin-top:16px">' + fold('自己加一个任务', addBody) + '</div></div>';
    }
    // 详情容器（2026-10-23 全链路走查缺陷）：openTask 里「拿到 box 为空就 return」是静默返回，
    // 而 #taskDetail 此前只在产出页渲染 —— 队列页那 8 个「详情」按钮点了**毫无反应、
    // 也没有任何提示**（CDP 实测：点击后 taskDetail=null、paneLen 不变、零运行时错误）。
    // 修：队列页也渲染容器；并把已展开的详情带过来（重渲染不打断阅读）。
    const openedDetail = document.getElementById('taskDetail');
    const keepDetail = openedDetail && openedDetail.innerHTML ? openedDetail.innerHTML : '';
    panes.queue.innerHTML = body + '<div id="taskDetail">' + keepDetail + '</div>';
  }

  // P3 验收徽章：只有真实执行验收通过的产出才算「可运行」。
  // 历史任务无验收证据、降级任务验收失败，都必须如实区分，不能一律标成功。
  const verifyBadge = (t) => {
    if (t.verified) return '<span class="state-done" title="verify 命令全部通过">已验收</span>';
    if (t.state === 'degraded') return '<span class="state-failed" title="验收未通过">验收未过</span>';
    if (t.verify) return '<span class="state-failed">未通过</span>';
    return '<span class="muted" title="改造前的历史任务，无验收证据">未验收</span>';
  };
  const repairBadge = (t) => {
    const n = t.repairRounds || 0;
    return n > 0 ? '<span class="tag">返工 ' + n + ' 轮</span>' : '—';
  };

  async function renderOutputs() {
    // 按状态分别取（2026-10-22 数据规模实测缺陷）：原来拉 /api/tasks?limit=3000 再在前端
    // filter(state==='done')。任务总量 33728 时，最新 3000 条被 2919 条 rejected 挤满，
    // 窗口内 done=0 —— 产出页显示「还没有产出」，而库存实际有 86 份完成品，用户会以为系统坏了。
    // 后端本来就支持 ?state= 过滤（handleTasks），按状态取既准确又快（86 条 54ms vs 3000 条 15ms）。
    const [doneRes, degRes] = await Promise.all([
      api('/api/tasks?state=done&limit=3000').catch(function () { return { tasks: [] }; }),
      api('/api/tasks?state=degraded&limit=3000').catch(function () { return { tasks: [] }; }),
    ]);
    const done = doneRes.tasks || [];
    const degraded = degRes.tasks || [];
    cache.tasks = done.concat(degraded);
    const verifiedN = done.filter(function (t) { return t.verified; }).length;
    const rowOf = function (t) {
      const q = t.critique ? t.critique.quality + '/10' : '—';
      return '<tr><td data-label="标题">' + esc(t.title) + '</td><td data-label="领域">' + esc(domainText(t.domain)) + '</td><td class="num" data-label="耗时">' + fmtMin(t.elapsedMs) + '</td>' +
        '<td class="num" data-label="自评">' + q + '</td><td data-label="验收">' + verifyBadge(t) + '</td><td data-label="返工">' + repairBadge(t) + '</td><td class="num" data-label="大小">' + fmtBytes(t.outputBytes) + '</td>' +
        '<td class="ops" data-label="操作"><button class="btn sm" onclick="WB.openTask(&apos;' + t.id + '&apos;)">查看</button>' +
        (t.state === 'done' ? '<button class="btn sm" onclick="WB.rate(&apos;' + t.id + '&apos;,5)">★5</button>' : '') + '</td></tr>';
    };
    const head = '<table aria-label="已完成产出"><thead><tr><th>标题</th><th>领域</th><th>耗时</th><th>自评</th><th>验收</th><th>返工</th><th>大小</th><th>操作</th></tr></thead><tbody>';
    const exportBody = '<div class="toolbar">' +
      '<button class="btn" onclick="WB.exportAll(&apos;md&apos;)">导出全部 Markdown</button>' +
      '<button class="btn" onclick="WB.exportAll(&apos;txt&apos;)">导出全部 TXT</button>' +
      '<button class="btn" onclick="WB.exportAll(&apos;docx&apos;)">导出全部 Word</button></div>';
    const doneTable = done.length ? head + done.map(rowOf).join('') + '</tbody></table>' : emptyHint('还没有产出。引擎跑起来后，成果会出现在这里。');
    panes.outputs.innerHTML =
      '<div class="card"><h3>已完成产出</h3>' +
      '<div class="status-line">共 <b>' + done.length + '</b> 份产出<span class="sep" aria-hidden="true">·</span>其中 <b>' + verifiedN + '</b> 份通过验收</div>' +
      '<div class="table-scroll" style="margin-top:12px">' + doneTable + '</div>' +
      '<div style="margin-top:14px">' + fold('导出这些产出', exportBody) + '</div>' +
      '</div>' +
      (degraded.length ? '<div class="card"><h3>验收未通过的产出（' + degraded.length + '）</h3><div class="muted">保留供查看，但不计入今日产出、不触发后继任务。</div>' +
        '<div class="table-scroll" style="margin-top:12px">' + head + degraded.map(rowOf).join('') + '</tbody></table></div></div>' : '') +
      '<div id="taskDetail"></div>';
  }

  // 模板库：24 个模板平铺是「页面又长又乱」的最后一块。
  // 加搜索 + 默认只看前 8 个（按价值先验降序，最该用的排前面），要全看再展开。
  let tplQuery = '';
  let tplShowAll = false;
  async function renderTemplates() {
    const res = await api('/api/templates');
    cache.templates = res.templates || [];
    const all = cache.templates.slice().sort(function (x, y) { return (y.valuePrior || 0) - (x.valuePrior || 0); });
    const q = tplQuery.trim().toLowerCase();
    const hit = q ? all.filter(function (t) { return (t.name || '').toLowerCase().indexOf(q) >= 0 || (t.domain || '').toLowerCase().indexOf(q) >= 0 || (DOMAIN_TEXT[t.domain] || '').indexOf(q) >= 0; }) : all;
    const shown = tplShowAll || q ? hit : hit.slice(0, 8);
    const rows = shown.map(function (t) {
      const enabled = t.enabled ? '<span class="state-done">启用</span>' : '<span class="muted">停用</span>';
      return '<tr><td data-label="名称">' + esc(t.name) + '</td><td data-label="领域">' + esc(domainText(t.domain)) + '</td><td class="num" data-label="优先级">' + t.priorityWeight + '</td><td class="num" data-label="价值先验">' + (t.valuePrior || 0) + '</td><td data-label="状态">' + enabled + '</td>' +
        '<td class="ops" data-label="操作"><button class="btn sm" onclick="WB.editTemplate(&apos;' + t.id + '&apos;)">编辑</button>' +
        '<button class="btn sm" onclick="WB.deleteTemplate(&apos;' + t.id + '&apos;,this)">删除</button></td></tr>';
    }).join('');
    const more = (!q && !tplShowAll && hit.length > shown.length)
      ? '<div class="toolbar" style="margin-top:12px"><button class="btn ghost" onclick="WB.tplMore()">还有 ' + (hit.length - shown.length) + ' 个模板，全部展开</button></div>'
      : '';
    panes.templates.innerHTML =
      '<div class="card"><h3>任务模板库</h3>' +
      '<div class="status-line">共 <b>' + cache.templates.length + '</b> 个模板<span class="sep" aria-hidden="true">·</span>引擎按它们的权重轮换生成任务<span class="sep" aria-hidden="true">·</span>按价值先验从高到低排</div>' +
      '<div class="toolbar" style="margin-top:12px"><input id="tplSearch" placeholder="搜模板名或领域…" value="' + esc(tplQuery) + '" style="max-width:320px"></div>' +
      '<div class="table-scroll" style="margin-top:12px"><table aria-label="任务模板库"><thead><tr><th>名称</th><th>领域</th><th>优先级</th><th>价值先验</th><th>状态</th><th>操作</th></tr></thead><tbody>' +
      (rows || '<tr><td colspan="6" class="empty">' + (q ? '没有匹配「' + esc(tplQuery) + '」的模板' : '还没有模板') + '</td></tr>') + '</tbody></table></div>' +
      more +
      '<div class="toolbar" style="margin-top:14px"><button class="btn primary" onclick="WB.newTemplate()">新建模板</button>' +
      '<button class="btn" onclick="WB.resetTemplates(this)">恢复默认</button></div>' +
      '</div>' +
      '<div id="templateEditor"></div>';
    const inp = document.getElementById('tplSearch');
    if (inp) {
      // 输入即过滤（250ms 去抖，避免每个字符都重排一整张表）
      inp.oninput = function () {
        clearTimeout(inp._t);
        inp._t = setTimeout(function () { tplQuery = inp.value; tplShowAll = false; renderTemplates().then(function () { const el = document.getElementById('tplSearch'); if (el) { el.focus(); el.setSelectionRange(el.value.length, el.value.length); } }); }, 250);
      };
    }
  }

  // 设置页：常用项直接可改，低频项（评估权重/工作目录/审计日志）折叠。
  async function renderSettings() {
    const cfgRes = await api('/api/settings');
    const autoRes = await api('/api/autostart');
    const c = cfgRes.config || {};
    const ex = c.executor || {};
    const ch = c.chain || {};
    cache.config = c;

    const evalRes = await api('/api/eval/config').catch(function () { return { eval: {} }; });
    const e = evalRes.eval || {};
    const w = e.weights || {};
    const evalBody = '<div class="grid c2">' +
      '<div><label>产出价值权重</label><input id="wV" type="number" step="0.05" min="0" max="1" value="' + (w.value || 0.5) + '">' +
      '<label>资源成本权重</label><input id="wC" type="number" step="0.05" min="0" max="1" value="' + (w.cost || 0.2) + '">' +
      '<label>可行性权重</label><input id="wF" type="number" step="0.05" min="0" max="1" value="' + (w.feasibility || 0.3) + '"></div>' +
      '<div><label>自动批准阈值（0-100）</label><input id="th" type="number" min="1" max="100" value="' + (e.threshold || 60) + '">' +
      '<label>每日任务上限</label><input id="cap" type="number" min="1" value="' + (e.dailyCap || 200) + '">' +
      '<label>相似任务去重阈值（0-1）</label><input id="sim" type="number" step="0.01" min="0.5" max="1" value="' + (e.similarityCap || 0.9) + '"></div></div>' +
      '<div class="row" style="margin-top:12px">' +
      '<label style="flex:0 0 auto;margin:0"><input id="semantic" type="checkbox" style="width:auto;margin-right:8px" ' + (e.semanticOn ? 'checked' : '') + '>启用语义判分（LLM）</label>' +
      '<label style="flex:0 0 auto;margin:0"><input id="repeat" type="checkbox" style="width:auto;margin-right:8px" ' + (e.repeatGate ? 'checked' : '') + '>重复任务硬门禁</label></div>' +
      '<div class="toolbar" style="margin-top:14px"><button class="btn" onclick="WB.saveEval()">保存评估配置</button></div>';

    panes.settings.innerHTML =
      '<div class="card"><h3>执行与并发</h3><div class="grid c2">' +
      '<div><label>同时执行几个任务（1-6）</label><input id="conc" type="number" min="1" max="6" value="' + (ex.concurrency || 2) + '">' +
      '<label>单个任务超时（分钟）</label><input id="timeout" type="number" min="1" max="120" value="' + (ex.defaultTimeout || 30) + '">' +
      '<label>失败重试次数</label><input id="retries" type="number" min="0" max="10" value="' + (ex.maxRetries || 3) + '"></div>' +
      '<div><label>链式任务最大深度</label><input id="chainDepth" type="number" min="1" max="20" value="' + (ch.maxDepth || 5) + '">' +
      '<label>每个任务最多衍生几个后续</label><input id="chainFan" type="number" min="0" max="4" value="' + (ch.fanout || 2) + '">' +
      '<label>低于多少分不再衍生</label><input id="chainQ" type="number" min="1" max="10" value="' + (ch.minQuality || 6) + '"></div></div>' +
      '<div class="toolbar" style="margin-top:14px"><button class="btn primary" onclick="WB.saveSettings()">保存设置</button></div></div>' +
      '<div class="card"><h3>评估与自动批准</h3><div class="muted">决定「哪些任务值得做」的打分规则。默认值经过实测调优，不确定就别改。</div>' +
      '<div style="margin-top:10px">' + fold('展开调整权重与阈值', evalBody) + '</div></div>' +
      '<div class="card"><h3>工作目录</h3><div id="wsRoot" data-ws="workspace"><div class="muted">读取中…</div></div></div>' +
      '<div class="card"><h3>运行信息</h3><table aria-label="运行信息"><tbody>' +
      '<tr><td style="width:150px">面板端口</td><td>' + esc(cfgRes.port) + '</td></tr>' +
      '<tr><td>运行配置</td><td>' + esc(ex.profileName || 'web-desktop') + '（一般不用改）</td></tr>' +
      '<tr><td>开机自启</td><td>' + (autoRes.enabled ? '已开启' : '未开启') + (autoRes.error ? '（' + esc(autoRes.error) + '）' : '') + '</td></tr>' +
      '</tbody></table>' +
      '<div class="row" style="margin-top:12px">' +
      '<label style="flex:0 0 auto;margin:0"><input id="autostart" type="checkbox" style="width:auto;margin-right:8px" ' + (autoRes.enabled ? 'checked' : '') + '>随 Windows 启动（后台静默，恢复上次运行状态）</label>' +
      '<button class="btn" style="flex:0 0 auto" onclick="WB.saveAutostart()">保存</button></div>' +
      '<div class="toolbar" style="margin-top:14px"><button class="btn" onclick="WB.diagnose()">打印自检报告</button>' +
      '<button class="btn danger" onclick="WB.stopEngine()">停止引擎</button></div><pre id="diagOut" style="margin-top:12px;display:none"></pre></div>' +
      '<div class="card"><h3>审计日志</h3><div id="logRoot"><div class="muted">读取中…</div></div></div>';

    fillWorkspace();
    fillLogs();
  }

  // 工作目录：当前目录 + 「换个目录」折叠 + 访问审计折叠。
  async function fillWorkspace() {
    const root = document.getElementById('wsRoot');
    if (!root) { return; }
    let view;
    try { view = await api('/api/workspace'); } catch (err) {
      root.innerHTML = '<div class="muted">工作目录接口不可用：' + esc(err.message) + '</div>';
      return;
    }
    wsState.view = view;
    const snap = view.snapshot || {};
    const cfg = view.config || {};
    const val = snap.validation;
    const audit = snap.audit || {};
    const external = view.mode === 'external';
    const modeBadge = external
      ? '<span class="state-done">外部目录模式（AI 直接在你的项目文件夹里开发）</span>'
      : '<span class="state-queued">项目模式（默认，任务在独立沙箱里跑）</span>';
    const pickBody = '<div class="muted">可以填任意本地绝对路径，也可以用下面的选择器逐层点进去。切换前必须先通过校验：目录存在、可读写、不是系统目录。</div>' +
      '<div class="toolbar" style="margin-top:10px">' +
      '<input id="wsPath" style="flex:1" placeholder="D:\projects\my-app" value="' + esc(wsState.path || '') + '">' +
      '<button class="btn" onclick="WB.wsBrowse(document.getElementById(&apos;wsPath&apos;).value)">浏览</button>' +
      '<button class="btn" onclick="WB.wsValidate()">校验</button>' +
      '<button class="btn primary" onclick="WB.wsSelect()">切换到此目录</button></div>' +
      '<div id="wsValidateBox" style="margin-top:10px"></div>' +
      '<div id="wsBrowser" style="margin-top:12px"><h4>目录选择器</h4><div class="muted" id="wsBrowseBody">点「浏览」开始（不填路径则列出本机盘符）。</div></div>' +
      (val ? '<div class="muted" style="margin-top:10px">最近校验：' + (val.allowed ? '<span class="state-done">通过</span>' : '<span class="state-failed">未通过</span>') +
        ' ｜ ' + esc(val.canonical || val.input) + ' ｜ 可读 ' + (val.readable ? '✓' : '✗') + ' ｜ 可写 ' + (val.writable ? '✓' : '✗') + '</div>' : '');
    const auditBody = '<div class="muted">每一次路径校验与读写裁决都会落盘到 <code>' + esc(snap.auditPath || '-') + '</code>，越权尝试可追溯。</div>' +
      '<div class="muted" style="margin-top:6px">允许 ' + (audit.allowed || 0) + ' 次 / 拒绝 ' + (audit.denied || 0) + ' 次' +
      (audit.lastDeny ? '，最近拒绝：' + esc(audit.lastDeny) : '') + '</div>' +
      '<div class="toolbar" style="margin-top:10px"><button class="btn ghost" onclick="WB.wsAudit()">查看审计明细</button></div>' +
      '<pre id="wsAuditOut" style="display:none;margin-top:10px;max-height:260px;overflow:auto"></pre>';
    root.innerHTML = '<div class="status-line">当前：<code>' + esc(view.root || '-') + '</code><span class="sep" aria-hidden="true">·</span>' + modeBadge + '</div>' +
      (external ? '<div class="toolbar" style="margin-top:10px"><button class="btn ghost" onclick="WB.wsReset(this)">切回项目模式</button></div>' : '') +
      '<div style="margin-top:12px">' + fold('换个工作目录', pickBody) + '</div>' +
      '<div style="margin-top:10px">' + fold('访问审计（允许 ' + (audit.allowed || 0) + ' / 拒绝 ' + (audit.denied || 0) + '）', auditBody) + '</div>';
    if (wsState.browse) { renderBrowseBox(wsState.browse); }
  }

  async function fillLogs() {
    const root = document.getElementById('logRoot');
    if (!root) { return; }
    let logs = { lines: [] }, tasksRes = { tasks: [] };
    try { logs = await api('/api/logs?tail=200&file=audit.jsonl'); } catch (e) { /* 日志读不到不该让整页报错 */ }
    try { tasksRes = await api('/api/tasks?limit=80'); } catch (e) { /* 同上 */ }
    const rows = (logs.lines || []).slice().reverse().map(function (l) {
      try {
        const o = JSON.parse(l);
        const ev = (o.from ? o.from + ' → ' : '') + (o.to || o.kind || '');
        return '<tr><td>' + fmtTime(o.at) + '</td><td>' + esc(o.actor || '') + '</td><td>' + esc(ev) + '</td><td>' + esc(o.message || '') + '</td></tr>';
      } catch (e2) { return '<tr><td colspan="4" class="muted">' + esc(l) + '</td></tr>'; }
    }).join('');
    const failed = (tasksRes.tasks || []).filter(function (t) { return t.state === 'failed' || t.state === 'timeout'; }).map(function (t) {
      return '<tr><td>' + esc(t.title) + '</td><td>' + esc(t.state) + '</td><td class="muted">' + esc((t.error || '').slice(0, 160)) + '</td>' +
        '<td><button class="btn sm" onclick="WB.retry(&apos;' + t.id + '&apos;)">重试</button></td></tr>';
    }).join('');
    root.innerHTML = fold('最近 200 条操作流水', rows
      ? '<div class="table-scroll"><table aria-label="操作审计流水"><thead><tr><th>时间</th><th>操作者</th><th>事件</th><th>说明</th></tr></thead><tbody>' + rows + '</tbody></table></div>'
      : emptyHint('还没有审计记录。引擎跑起来后，每一次审批、执行、验收都会留痕在这里。')) +
      fold('失败任务（' + (failed ? (tasksRes.tasks || []).filter(function (t) { return t.state === 'failed' || t.state === 'timeout'; }).length : 0) + '）',
        failed ? '<div class="table-scroll"><table aria-label="失败任务列表"><thead><tr><th>标题</th><th>状态</th><th>错误</th><th>操作</th></tr></thead><tbody>' + failed + '</tbody></table></div>'
          : emptyHint('没有失败任务 —— 保持这样最好。'));
  }

  // ── 交互动作 ─────────────────────────────────────────
  const WB = {
    async start() { await api('/api/engine/start', { method: 'POST', body: '{}' }); toast('引擎已启动'); renderOverview(); },
    async stopEngine() { await api('/api/engine/stop', { method: 'POST', body: '{}' }); toast('正在停止引擎'); },
    async pause() { try { await api('/api/engine/pause', { method: 'POST', body: '{}' }); toast('已暂停'); refreshTop(); } catch (e) { toast(e.message); } },
    async resume() { try { await api('/api/engine/resume', { method: 'POST', body: '{}' }); toast('已继续'); refreshTop(); } catch (e) { toast(e.message); } },
    async cancel(id) { try { await api('/api/tasks/' + id + '/cancel', { method: 'POST', body: '{}' }); toast('已请求终止'); renderQueue(); } catch (e) { toast(e.message); } },
    async retry(id) { try { await api('/api/tasks/' + id + '/retry', { method: 'POST', body: '{}' }); toast('已重新入队'); fillLogs(); } catch (e) { toast(e.message); } },
    async priority(id) { try { await api('/api/tasks/' + id + '/priority', { method: 'POST', body: JSON.stringify({ priority: 5, pinned: true }) }); toast('已置顶'); renderQueue(); } catch (e) { toast(e.message); } },
    async rate(id, r) { try { await api('/api/tasks/' + id + '/rate', { method: 'POST', body: JSON.stringify({ rating: r }) }); toast('已记录评分'); } catch (e) { toast(e.message); } },
    async manual() {
      const title = (document.getElementById('mTitle').value || '').trim();
      const prompt = (document.getElementById('mPrompt').value || '').trim();
      const domain = document.getElementById('mDomain').value;
      if (!title || !prompt) { toast('标题与描述都要填'); return; }
      try {
        const r = await api('/api/tasks/manual', { method: 'POST', body: JSON.stringify({ title: title, prompt: prompt, domain: domain, priority: 3 }) });
        if (r.warning) { toast(r.warning); } else { toast('已入队'); document.getElementById('mTitle').value = ''; document.getElementById('mPrompt').value = ''; }
        renderQueue();
      } catch (e) { toast(e.message); }
    },
    async openTask(id) {
      try {
        const res = await api('/api/tasks/' + id);
        const t = res.task;
        const box = document.getElementById('taskDetail');
        if (!box) {
          // 静默 return 会让「按钮点了没反应」成为不可诊断故障（本轮实测踩到）。
          // 容器缺失属渲染契约违背，必须明确报出来。
          toast('详情面板缺失（渲染契约错误）：#taskDetail 未挂载');
          return;
        }
        const q = t.critique ? ' ｜ 自评：' + t.critique.quality + '/10' : '';
        // 详情必须落在卡壳里（与模板编辑器同型）：裸 h3+doc 浮在页底破坏层级（2026-10-21 走查）。
        box.innerHTML = '<div class="card"><h3>' + esc(t.title) + '</h3>' +
          '<div class="muted" style="margin-bottom:10px">状态：' + esc(STATE_TEXT[t.state] || t.state) + ' ｜ 领域：' + esc(domainText(t.domain)) +
          ' ｜ 模型：' + esc(t.model || '-') + ' ｜ 耗时：' + fmtMin(t.elapsedMs) + q + '</div>' +
          '<div class="doc">' + mdToHtml(res.content || '（暂无产出）') + '</div>' +
          '<div style="margin-top:12px" class="toolbar"><button class="btn sm" onclick="WB.rate(&apos;' + t.id + '&apos;,5)">★★★★★</button>' +
          '<button class="btn sm" onclick="WB.rate(&apos;' + t.id + '&apos;,4)">★★★★</button>' +
          '<button class="btn sm" onclick="WB.rate(&apos;' + t.id + '&apos;,3)">★★★</button></div></div>';
        box.scrollIntoView({ behavior: 'smooth' });
      } catch (e) { toast(e.message); }
    },
    async exportAll(format) {
      try {
        const r = await api('/api/export', { method: 'POST', body: JSON.stringify({ format: format || 'md', scope: 'all' }) });
        toast('已导出 ' + r.count + ' 篇 → ' + r.name);
      } catch (e) { toast(e.message); }
    },
    async saveEval() {
      try {
        const cur = await api('/api/eval/config');
        const merged = Object.assign({}, cur.eval, {
          weights: {
            value: parseFloat(document.getElementById('wV').value) || 0.5,
            cost: parseFloat(document.getElementById('wC').value) || 0.2,
            feasibility: parseFloat(document.getElementById('wF').value) || 0.3,
          },
          threshold: parseInt(document.getElementById('th').value) || 60,
          dailyCap: parseInt(document.getElementById('cap').value) || 200,
          similarityCap: parseFloat(document.getElementById('sim').value) || 0.9,
          semanticOn: document.getElementById('semantic').checked,
          repeatGate: document.getElementById('repeat').checked,
        });
        await api('/api/eval/config', { method: 'PUT', body: JSON.stringify(merged) });
        toast('评估配置已保存');
      } catch (e) { toast(e.message); }
    },
    async previewEval() {
      const title = (document.getElementById('pTitle').value || '').trim();
      const prompt = (document.getElementById('pPrompt').value || '').trim();
      const domain = document.getElementById('pDomain').value;
      if (!title || !prompt) { toast('请填写标题和描述'); return; }
      try {
        const r = await api('/api/eval/preview', { method: 'POST', body: JSON.stringify({ title: title, prompt: prompt, domain: domain }) });
        const s = r.scores;
        let verdict;
        if (s.gates && s.gates.length) { verdict = '<span class="state-failed">' + esc(s.gates.join('；')) + '</span>'; }
        else if (s.total >= s.threshold) { verdict = '<span class="state-done">通过（阈值 ' + s.threshold + '）</span>'; }
        else { verdict = '<span class="state-failed">未过阈值 ' + s.threshold + '</span>'; }
        document.getElementById('previewResult').innerHTML =
          '<div class="grid c4">' + kpi('综合分', s.total) + kpi('价值', s.value) + kpi('成本', s.cost) + kpi('可行性', s.feasibility) + '</div>' +
          '<div class="muted" style="margin-top:10px">判定：' + verdict + '</div>' +
          '<div class="muted" style="margin-top:6px">' + esc((s.reasons || []).join(' ｜ ')) + '</div>';
      } catch (e) { toast(e.message); }
    },
    async saveSettings() {
      try {
        const cur = await api('/api/settings');
        const c = cur.config;
        c.executor.concurrency = parseInt(document.getElementById('conc').value) || 2;
        c.executor.defaultTimeout = parseInt(document.getElementById('timeout').value) || 30;
        c.executor.maxRetries = parseInt(document.getElementById('retries').value) || 3;
        c.chain.maxDepth = parseInt(document.getElementById('chainDepth').value) || 5;
        c.chain.fanout = parseInt(document.getElementById('chainFan').value) || 2;
        c.chain.minQuality = parseInt(document.getElementById('chainQ').value) || 6;
        await api('/api/settings', { method: 'PUT', body: JSON.stringify(c) });
        toast('设置已保存');
      } catch (e) { toast(e.message); }
    },
    async saveAutostart() {
      try {
        const enabled = document.getElementById('autostart').checked;
        await api('/api/autostart', { method: 'POST', body: JSON.stringify({ enabled: enabled }) });
        toast('开机自启已' + (enabled ? '开启' : '关闭'));
        renderSettings();
      } catch (e) { toast(e.message); }
    },
    async diagnose() {
      try {
        const r = await api('/api/diagnose');
        const el = document.getElementById('diagOut');
        if (el) { el.style.display = 'block'; el.textContent = JSON.stringify(r, null, 2); }
        toast('自检完成');
      } catch (e) { toast(e.message); }
    },
    newTemplate() {
      const box = document.getElementById('templateEditor');
      // 键盘可达性（2026-10-22 走查实测缺口）：内联编辑器此前只能靠鼠标点「保存」离开，
      // 既没有「取消」入口也没有 Escape 关闭 —— 键盘用户进得去出不来。
      // 这里记下触发元素 + 提供取消按钮 + Escape（见文末 keydown 绑定）。
      box._returnFocus = document.activeElement;
      box.innerHTML = '<div class="card"><h3>新建模板</h3>' +
        '<label>ID（英文唯一）</label><input id="tId" placeholder="my-template">' +
        '<label>名称</label><input id="tName" placeholder="我的模板">' +
        '<label for="tDomain">领域</label><select id="tDomain"><option value="content">内容创作</option><option value="code">代码生成</option><option value="knowledge">知识整理</option><option value="data">数据处理</option><option value="solution">方案撰写</option><option value="report">报告生成</option></select>' +
        '<label>任务描述</label><input id="tDesc" placeholder="这个模板产出什么">' +
        '<label>Prompt 模板（必须含 {topic}）</label><textarea id="tPrompt" rows="6" placeholder="请围绕「{topic}」产出……"></textarea>' +
        '<label>优先级权重（1-5）</label><input id="tPriority" type="number" min="1" max="5" value="3">' +
        '<label>价值先验（0-100）</label><input id="tValue" type="number" min="0" max="100" value="75">' +
        '<div class="toolbar" style="margin-top:12px"><button class="btn primary" onclick="WB.saveTemplate()">保存模板</button>' +
        '<button class="btn ghost" onclick="WB.closeTemplateEditor()">取消</button></div></div>';
    },
    // closeTemplateEditor 清空内联编辑器并把焦点退回触发按钮（取消按钮与 Escape 共用）。
    closeTemplateEditor() {
      const box = document.getElementById('templateEditor');
      if (!box) { return; }
      const back = box._returnFocus;
      box._returnFocus = null;
      box.innerHTML = '';
      if (back && document.contains(back) && typeof back.focus === 'function') {
        try { back.focus(); } catch (e) { /* 已从文档移除 */ }
      }
    },
    editTemplate(id) {
      const t = cache.templates.find(function (x) { return x.id === id; });
      if (!t) { return; }
      const box = document.getElementById('templateEditor');
      box._returnFocus = document.activeElement;
      box.innerHTML = '<div class="card"><h3>编辑模板：' + esc(t.name) + '</h3>' +
        '<label>名称</label><input id="tName" value="' + esc(t.name) + '">' +
        '<label>任务描述</label><input id="tDesc" value="' + esc(t.description || '') + '">' +
        '<label>Prompt 模板</label><textarea id="tPrompt" rows="6">' + esc(t.promptTemplate || '') + '</textarea>' +
        '<label>优先级权重（1-5）</label><input id="tPriority" type="number" min="1" max="5" value="' + (t.priorityWeight || 3) + '">' +
        '<label>价值先验（0-100）</label><input id="tValue" type="number" min="0" max="100" value="' + (t.valuePrior || 75) + '">' +
        '<label for="tEnabled">是否启用</label><select id="tEnabled"><option value="1"' + (t.enabled ? ' selected' : '') + '>启用</option><option value="0"' + (t.enabled ? '' : ' selected') + '>停用</option></select>' +
        '<div class="toolbar" style="margin-top:12px"><button class="btn primary" onclick="WB.saveTemplate(&apos;' + id + '&apos;)">保存修改</button>' +
        '<button class="btn ghost" onclick="WB.closeTemplateEditor()">取消</button></div></div>';
      box.scrollIntoView({ behavior: 'smooth' });
    },
    async saveTemplate(id) {
      const name = (document.getElementById('tName').value || '').trim();
      const desc = (document.getElementById('tDesc').value || '').trim();
      const prompt = (document.getElementById('tPrompt').value || '').trim();
      const priority = parseInt(document.getElementById('tPriority').value) || 3;
      const value = parseInt(document.getElementById('tValue').value) || 75;
      const enabledEl = document.getElementById('tEnabled');
      const enabled = enabledEl ? enabledEl.value === '1' : true;
      let tpl;
      if (id) {
        tpl = JSON.parse(JSON.stringify(cache.templates.find(function (x) { return x.id === id; })));
        tpl.name = name; tpl.description = desc; tpl.promptTemplate = prompt;
        tpl.priorityWeight = priority; tpl.valuePrior = value; tpl.enabled = enabled;
      } else {
        const tid = (document.getElementById('tId').value || '').trim();
        const domain = document.getElementById('tDomain').value;
        if (!tid || !name || !prompt) { toast('ID、名称、Prompt 都要填'); return; }
        tpl = {
          id: tid, name: name, domain: domain, description: desc, promptTemplate: prompt,
          priorityWeight: priority, valuePrior: value, costPrior: 40, cooldownMinutes: 60,
          diversityGroup: domain, enabled: true, builtin: false,
          params: { modelTier: 'balanced', maxMinutes: 30, outputFormat: 'md', webAllowed: false, tags: [] },
          outputSpec: { format: 'md', sections: [], minWords: 500, minBytes: 1500 },
          chain: { successorTemplates: [], maxDepth: 5, fanout: 1 },
        };
      }
      try {
        await api('/api/templates', { method: 'POST', body: JSON.stringify(tpl) });
        toast('模板已保存');
        renderTemplates();
      } catch (e) { toast(e.message); }
    },
    async deleteTemplate(id, btn) {
      armConfirm(btn, '再点一次确认删除', async () => {
        try { await api('/api/templates/' + id, { method: 'DELETE' }); toast('已删除该模板'); renderTemplates(); } catch (e) { toast(e.message); }
      });
    },
    async resetTemplates(btn) {
      armConfirm(btn, '再点确认恢复默认', async () => {
        try { await api('/api/templates/reset', { method: 'POST', body: '{}' }); toast('已恢复默认'); renderTemplates(); } catch (e) { toast(e.message); }
      });
    },
    // ── 工作目录（AI 开发功能扩展） ──────────────────────
    async wsBrowse(path) {
      wsState.path = path || '';
      try {
        const r = await api('/api/workspace/browse?path=' + encodeURIComponent(wsState.path));
        wsState.browse = r.browse;
        renderBrowseBox(r.browse);
        if (!r.ok) { toast('无法浏览：' + (r.error || '未知错误')); }
      } catch (e) { toast('浏览失败：' + e.message); }
    },
    wsUseBrowse() {
      const b = wsState.browse;
      if (!b || !b.path) { toast('请先进入一个目录'); return; }
      const el = document.getElementById('wsPath');
      if (el) { el.value = b.path; }
      wsState.path = b.path;
      toast('已填入路径，请点「校验」');
    },
    async wsValidate() {
      const el = document.getElementById('wsPath');
      const path = ((el && el.value) || '').trim();
      if (!path) { toast('请先填写或选择目录'); return; }
      wsState.path = path;
      try {
        const r = await api('/api/workspace/validate', { method: 'POST', body: JSON.stringify({ path: path }) });
        wsState.validation = r.validation;
        renderValidation(r.validation, r.ok ? null : r.error);
      } catch (e) { renderValidation(null, e.message); }
    },
    async wsSelect() {
      const el = document.getElementById('wsPath');
      const path = ((el && el.value) || '').trim();
      if (!path) { toast('请先填写或选择目录'); return; }
      wsState.path = path;
      try {
        const r = await api('/api/workspace/select', { method: 'POST', body: JSON.stringify({ path: path }) });
        if (!r.ok) {
          renderValidation(r.validation, r.error);
          toast('切换失败：' + (r.error || '校验未通过'));
          return;
        }
        renderValidation(r.validation, null);
        toast('已切换到外部目录：' + path);
        await fillWorkspace();
      } catch (e) { toast('切换失败：' + e.message); }
    },
    async wsReset(btn) {
      armConfirm(btn, '再点确认切回', async () => {
        try {
          await api('/api/workspace/reset', { method: 'POST', body: '{}' });
          toast('已切回项目模式');
          await fillWorkspace();
        } catch (e) { toast('切换失败：' + e.message); }
      });
    },
    async wsAudit() {
      try {
        const r = await api('/api/workspace/audit?tail=80');
        const el = document.getElementById('wsAuditOut');
        if (!el) { return; }
        el.style.display = 'block';
        el.textContent = JSON.stringify(r, null, 2);
      } catch (e) { toast('读取审计失败：' + e.message); }
    },
  };
  window.WB = WB;

  // Markdown 极简渲染
  function mdToHtml(md) {
    const lines = String(md || '').split('\n');
    let html = '', inList = false, inCode = false;
    for (let i = 0; i < lines.length; i++) {
      const raw = lines[i];
      if (raw.trim().slice(0, 3) === String.fromCharCode(96, 96, 96)) { inCode = !inCode; html += inCode ? '<pre>' : '</pre>'; continue; }
      if (inCode) { html += esc(raw) + '\n'; continue; }
      const line = esc(raw);
      if (/^###\s+/.test(line)) { if (inList) { html += '</ul>'; inList = false; } html += '<h3>' + line.replace(/^###\s+/, '') + '</h3>'; }
      else if (/^##\s+/.test(line)) { if (inList) { html += '</ul>'; inList = false; } html += '<h2>' + line.replace(/^##\s+/, '') + '</h2>'; }
      else if (/^#\s+/.test(line)) { if (inList) { html += '</ul>'; inList = false; } html += '<h1>' + line.replace(/^#\s+/, '') + '</h1>'; }
      else if (/^[-*]\s+/.test(line)) { if (!inList) { html += '<ul>'; inList = true; } html += '<li>' + line.replace(/^[-*]\s+/, '').replace(/\*\*(.+?)\*\*/g, '<b>$1</b>') + '</li>'; }
      else if (line.trim() === '') { if (inList) { html += '</ul>'; inList = false; } }
      else { if (inList) { html += '</ul>'; inList = false; } html += '<p>' + line.replace(/\*\*(.+?)\*\*/g, '<b>$1</b>') + '</p>'; }
    }
    if (inList) { html += '</ul>'; }
    if (inCode) { html += '</pre>'; }
    return html;
  }

  // ── 工作目录页签用到的两个渲染小件（跟随设置页的折叠块） ──
  const wsState = { path: '', browse: null, validation: null, view: null };

  function renderBrowseBox(b) {
    const box = document.getElementById('wsBrowseBody');
    if (!box) { return; }
    const drives = (b.drives || []).map(function (d) {
      return '<button class="btn sm" style="margin:2px" onclick="WB.wsBrowse(\'' + esc(d) + '\')">' + esc(d) + '</button>';
    }).join(' ');
    const items = (b.entries || []).map(function (e) {
      return '<button class="btn sm" style="margin:2px" onclick="WB.wsBrowse(\'' + esc(e.path) + '\')">📁 ' + esc(e.name) + '</button>';
    }).join('');
    box.innerHTML =
      '<div class="muted">当前位置：<code>' + esc(b.path || '（盘符列表）') + '</code></div>' +
      '<div class="row" style="margin:8px 0">' +
        (b.parent ? '<button class="btn sm" onclick="WB.wsBrowse(\'' + esc(b.parent) + '\')">⬆ 上一级</button>' : '') +
        '<button class="btn sm" onclick="WB.wsUseBrowse()">用此目录</button></div>' +
      (drives ? '<div style="margin-bottom:8px">' + drives + '</div>' : '') +
      '<div style="max-height:240px;overflow:auto">' + (items || '<span class="muted">没有子目录</span>') + '</div>' +
      (b.reason ? '<div class="muted" style="color:var(--danger,#e5534b)">' + esc(b.reason) + '</div>' : '');
  }

  function renderValidation(v, err) {
    const box = document.getElementById('wsValidateBox');
    if (!box) { return; }
    if (!v) {
      box.innerHTML = '<div class="muted" style="color:var(--danger,#e5534b)">' + esc(err || '校验失败') + '</div>';
      return;
    }
    const okFlag = v.allowed && !err;
    box.innerHTML = '<div class="muted">' + (okFlag ? '<span class="state-done">✔ 可以切换</span>' : '<span class="state-failed">✘ 不能切换</span>') +
      ' ｜ ' + esc(v.canonical || v.input || '') + '</div>' +
      (v.reason ? '<div class="muted" style="margin-top:4px;color:var(--danger,#e5534b)">' + esc(v.reason) + '</div>' : '') +
      (v.suggestion ? '<div class="muted" style="margin-top:4px">建议：' + esc(v.suggestion) + '</div>' : '') +
      '<div class="muted" style="margin-top:4px">存在 ' + (v.exists ? '✓' : '✗') + ' ｜ 目录 ' + (v.isDir ? '✓' : '✗') +
      ' ｜ 可读 ' + (v.readable ? '✓' : '✗') + ' ｜ 可写 ' + (v.writable ? '✓' : '✗') +
      ' ｜ 入口 ' + (v.entries || 0) + ' 项</div>';
  }

  // Tab 切换（含 hash 深链）：点页签写 hash；浏览器返回/前进由 hashchange 收敛。
  // 「状态可回溯」的实现要点：hash 是唯一真源 —— 刷新/返回都从 hash 重建激活态，
  // 非法 hash（如 #nope，或已合并掉的旧页签 #logs）一律回落 overview，
  // 且不改写 hash（避免把错误值固化进历史）。
  // 已合并页签的旧深链做一次映射，老书签不至于直接掉回总览。
  const LEGACY_TABS = { logs: 'settings', evaluate: 'settings', workspace: 'settings', autonomy: 'overview', governance: 'overview' };
  const tabs = document.querySelectorAll('.tab');
  const TAB_NAMES = Array.prototype.map.call(tabs, function (t) { return t.getAttribute('data-tab'); });
  let pendingHash = null;
  // 首次加载占位（2026-10-22 慢后端实测缺陷）：切页签时目标面板若还空着，先给一张骨架再取数。
  // 实测（CDP 限速 1.2s）：模板页首帧 pane.innerHTML 长度 = 0 —— 整片空白，用户无从判断
  // 「正在读」还是「已经坏了」。判据卷「慢网/资源失败态：Loading 兜底不得永驻」的另一半是
  // **失败必须替换占位**：否则读失败会显示成「一直在读」（见 render 的 catch）。
  const loadingPane = (name) => {
    const el = panes[name] || document.getElementById('tab-' + name);
    if (!el || el.innerHTML) { return; }
    el.innerHTML = '<div class="card"><h3>读取中</h3>' +
      '<div class="skeleton"></div><div class="skeleton short"></div><div class="skeleton"></div></div>';
  };
  const failPane = (name, msg) => {
    const el = panes[name] || document.getElementById('tab-' + name);
    if (!el) { return; }
    el.innerHTML = '<div class="card"><h3>读取失败</h3>' +
      '<div class="empty-hint">' + esc(msg || '接口不可用') + '<br>点上方页签可重试。</div></div>';
  };
  function activateTab(name, opts) {
    if (TAB_NAMES.indexOf(name) < 0) { name = LEGACY_TABS[name] || 'overview'; }
    const allTabs = document.querySelectorAll('.tab');
    for (let j = 0; j < allTabs.length; j++) { allTabs[j].classList.remove('active'); }
    const allPanes = document.querySelectorAll('.tabpane');
    for (let j = 0; j < allPanes.length; j++) { allPanes[j].classList.remove('active'); }
    const tab = document.querySelector('.tab[data-tab="' + name + '"]');
    if (tab) { tab.classList.add('active'); }
    const pane = document.getElementById('tab-' + name);
    if (pane) { pane.classList.add('active'); }
    loadingPane(name); // 空面板先给骨架，避免慢后端下的整片空白
    if (!opts || opts.hash !== false) {
      if (location.hash !== '#' + name) { pendingHash = name; location.hash = name; }
    }
    render(name);
  }
  for (let i = 0; i < tabs.length; i++) {
    (function (tab) {
      tab.addEventListener('click', function () {
        activateTab(tab.getAttribute('data-tab'));
      });
    })(tabs[i]);
  }
  window.addEventListener('hashchange', function () {
    const name = (location.hash || '').replace(/^#/, '');
    // 自己写 hash 触发的那次 hashchange 已由 activateTab 渲染过，跳过以免重复请求。
    if (pendingHash === name) { pendingHash = null; return; }
    pendingHash = null;
    activateTab(name, { hash: false });
  });

  // SSE 每秒约 10 条事件，onmessage 对 overview/queue 直接重渲染（全量 innerHTML 替换）。
  // 实测事故（2026-10-23 并发时序走查）：用户把「任务」页表格滚到 100px 后，
  // 重渲染把整个 .table-scroll 元素换掉（elemStillInDom=false），新节点 scrollTop 归 0 ——
  // 列表每秒被弹回顶部十次，根本没法看。焦点同理（注入输入框实测 nodeReplaced=true、
  // caret/value/focus 全丢）。修：重渲染前快照滚动位置与焦点，渲染后按「同一位置」
  // （tabpane 选择器 + 索引）恢复；找不到对应节点就跳过，绝不猜测。
  const snapshotScroll = () => {
    const list = [];
    document.querySelectorAll('.tabpane.active .table-scroll, .tabpane.active .doc, .tabpane.active pre').forEach(function (el, i) {
      if (el.scrollTop > 0 || el.scrollLeft > 0) {
        list.push({ key: el.className + ':' + i, top: el.scrollTop, left: el.scrollLeft });
      }
    });
    return list;
  };
  const restoreScroll = (snap) => {
    if (!snap || !snap.length) { return; }
    // 关键（2026-10-23 实测踩坑）：**不要**手动按 scrollHeight 夹紧。
    // 刚换完 innerHTML 的那一刻布局还没完成，scrollHeight 读出来偏小，
    // maxTop 会被算成 0，Math.min(top, 0) 直接把滚动位置夹成 0 —— 第一版修复
    // 就是这个原因完全无效（30 次采样全是 top:0）。浏览器自己会把越界值 clamp 到合法范围。
    // 另外必须等布局完成：用双 rAF（第一帧布局，第二帧生效），否则赋值同样被丢弃。
    const apply = () => {
      document.querySelectorAll('.tabpane.active .table-scroll, .tabpane.active .doc, .tabpane.active pre').forEach(function (el, i) {
        const key = el.className + ':' + i;
        for (let k = 0; k < snap.length; k++) {
          if (snap[k].key === key) {
            el.scrollTop = snap[k].top;
            el.scrollLeft = snap[k].left;
            break;
          }
        }
      });
    };
    if (typeof requestAnimationFrame === 'function') {
      requestAnimationFrame(function () { requestAnimationFrame(apply); });
    } else {
      apply();
    }
  };
  async function render(name) {
    const scrollSnap = snapshotScroll();
    try {
      if (name === 'overview') { await renderOverview(); }
      else if (name === 'queue') { await renderQueue(); }
      else if (name === 'outputs') { await renderOutputs(); }
      else if (name === 'templates') { await renderTemplates(); }
      else if (name === 'settings') { await renderSettings(); }
      restoreScroll(scrollSnap);
    } catch (e) {
      // 占位不得永驻：失败时**替换**成失败态，否则「读失败」会被显示成「一直在读」。
      failPane(name, '加载失败：' + e.message);
      toast('加载失败：' + e.message);
    }
  }

  // 重入守卫：/api/status 要拿引擎写锁（Phase/LastError 都 e.mu.Lock），
  // 点「启动」时 Start() 持锁跨 bootstrapAutonomy（内含模型调用）可达数秒 —— 此时顶栏
  // 请求会排队。5s 一轮 + 无守卫 = 一次卡顿就堆一串在途，且旧响应后到会把顶栏往回拨。
  let topBusy = false;
  async function refreshTop() {
    if (topBusy) { return; }
    topBusy = true;
    try {
      const st = await api('/api/status', { timeoutMs: 20000 });
      const s = st.stats || {};
      const phaseText = { running: '运行中', paused: '已暂停', stopped: '已停止', idle: '待启动' }[st.phase] || st.phase;
      const el = document.getElementById('topStat');
      // 顶栏只放一句话结论 + 关键读数；完整原因与上游原文放 title（点击悬停可读）。
      const rl = st.rateLimit || {};
      const lh = st.laneHealth || {};
      let top = '引擎' + phaseText + ' ｜ 排队 ' + (s.queuedTasks || 0) + ' ｜ 今日产出 ' + (s.todayDone || 0);
      let color = '';
      let tip = '';
      // 停摆自证（2026-10-21 现场）：运行中 + 0 产出时，顶栏必须直接说清原因，
      // 否则"运行中但产出 0"就是一条无法解释的读数（用户只能靠猜）。
      const stallTop = st.stallReason || {};
      if (stallTop.code && !lh.blocked) {
        top = '⚠ ' + (stallTop.text || '当前没有产出') + ' ｜ 排队 ' + (s.queuedTasks || 0);
        color = 'var(--warn)';
        tip = stallTop.detail || '';
      }
      if (lh.blocked) {
        const label = lh.blockKind === 'region_block' ? '上游地区限制' : '上游封禁本机';
        top = '⛔ ' + label + '（IP ' + (lh.egressIp || '?') + '） ｜ 需等解封或换网络出口';
        color = 'var(--danger)';
        tip = (lh.reason || '') + '\n\n上游原文：' + (lh.evidence || '（无）');
      } else if (rl.quotaExhausted) {
        // 额度耗尽与「等冷却」必须分开说（round 95 · T77）：前者要**账号侧动作**，等到天亮也不会恢复。
        // 面板原先一律显示「上游限流冷却中，剩余 N 秒」⇒ 给了错的 action（让人去等一个不会来的恢复）。
        top = '⛔ 免费额度已耗尽（' + (rl.quotaLanes || 0) + ' 条车道因此冷却） ｜ 排队 ' + (s.queuedTasks || 0) + ' ｜ 需补额度/换 provider';
        color = 'var(--danger)';
        tip = rl.lastReason || '';
      } else if (rl.limited) {
        top = '⚠ 上游限流冷却中（剩余 ' + (rl.remainingSeconds || 0) + ' 秒） ｜ 排队 ' + (s.queuedTasks || 0);
        color = 'var(--warn)';
        tip = rl.lastReason || '';
      }
      if (el) { el.textContent = top; el.style.color = color; el.title = tip; }
    } catch (e) { /* ignore */ }
    finally { topBusy = false; }
  }

  const btnPause = document.getElementById('btnPause');
  const btnResume = document.getElementById('btnResume');
  const btnStop = document.getElementById('btnStop');
  if (btnPause) { btnPause.onclick = WB.pause; }
  if (btnResume) { btnResume.onclick = WB.resume; }
  // 停止引擎是破坏性操作（中断在跑任务），用两段式就地确认替代原生 confirm()。
  if (btnStop) { btnStop.onclick = function () { armConfirm(btnStop, '再点确认停止', function () { WB.stopEngine(); }); }; }

  // 点别处即否定待确认态（原生 confirm 没有这条退出通道，但它是最直觉的取消方式）。
  document.addEventListener('click', function (ev) {
    const armed = document.querySelector('[data-armed="1"]');
    if (armed && armed !== ev.target && !armed.contains(ev.target)) { disarmArm(armed); }
  }, true);

  // 「更多」菜单：跨页入口收进一个按钮，顶栏不再横向排 6 个按钮（视觉噪音）。
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
    document.addEventListener('keydown', function (ev) {
      if (ev.key === 'Escape' && !topMore.hidden) {
        topMore.hidden = true; btnMore.setAttribute('aria-expanded', 'false'); btnMore.focus();
      }
    });
  }

  // Escape：先解掉待确认态，再关模板编辑器 —— 键盘用户必须能否定破坏性操作。
  document.addEventListener('keydown', function (ev) {
    if (ev.key !== 'Escape') { return; }
    document.querySelectorAll('[data-armed="1"]').forEach(disarmArm);
    const box = document.getElementById('templateEditor');
    if (box && box.innerHTML) {
      ev.preventDefault();
      WB.closeTemplateEditor();
    }
  });

  try {
    const es = new EventSource('/events?token=' + encodeURIComponent(TOKEN));
    // 节流（2026-10-23 并发时序走查）：实测 SSE 峰值约 10 条/秒，原实现每条都对
    // overview/queue 做全量 innerHTML 重渲染。虽然当前长任务为 0（内容量小），但
    // 这是「事件频率 × 全量重建」的平方级浪费，内容一多就会掉帧。合并到 400ms 窗口：
    // 窗口内多条事件只渲染最后一帧（数据本来就是全量拉取，丢中间帧不丢信息）。
    //
    // ⚠️ 但当年这道闸门**只做了一半**（round 152 前端/性能面实测发现）：窗口只包住了 render()，
    // 而 refreshTop() 留在窗口之外无条件执行 —— 它是一次 await api('/api/status')，
    // 且按本文件 1007 行的注释，/api/status 要拿**引擎写锁**（Phase/LastError 都 e.mu.Lock）。
    // 后果：节流省下的是"全量重建"，留下的却是"每条 SSE 一次带锁请求"，而且因为它写在
    // 页签判断之前，连停在 settings/templates 页时也在打。SSE 峰值 10 条/秒 ⇒ 顶栏轮询
    // 从设计的 5 秒一次退化成最高 10 次/秒，全打在引擎写锁上。
    //
    // 现在把两件事收进**同一个** 400ms 窗口：顶栏照旧任何页签都刷新（语义不变），
    // 渲染仍只在 overview/queue 生效（语义不变）；窗口内多次事件只各做**一次**。
    let renderTimer = null;
    let renderPending = false;
    let renderName = '';
    let topPending = false;
    es.onmessage = function () {
      topPending = true; // 顶栏：任何页签都要更新，但要进闸门
      const active = document.querySelector('.tab.active');
      const name = active && active.getAttribute('data-tab');
      if (name === 'overview' || name === 'queue') {
        renderPending = true;
        renderName = name;
      }
      if (renderTimer) { return; } // 窗口已开：只置位，不重复登记
      renderTimer = setTimeout(function () {
        renderTimer = null;
        if (topPending) {
          topPending = false;
          refreshTop();
        }
        if (!renderPending) { return; }
        renderPending = false;
        render(renderName);
      }, 400);
    };
  } catch (e) { /* ignore */ }

  refreshTop();
  setInterval(refreshTop, 5000);
  activateTab((location.hash || '').replace(/^#/, ''), { hash: false });
})();
`
