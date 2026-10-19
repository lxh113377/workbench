package web

// appJS 是管理面板与首页的前端脚本。
const appJS = `(() => {
  const TOKEN = window.__WB_TOKEN__ || '';
  const isAdmin = document.body.classList.contains('admin');
  const api = async (path, opts = {}) => {
    const res = await fetch(path, {
      ...opts,
      headers: { 'Content-Type': 'application/json', 'X-WB-Token': TOKEN, ...(opts.headers || {}) },
    });
    const text = await res.text();
    let data = null;
    try { data = text ? JSON.parse(text) : null; } catch (e) { data = { raw: text }; }
    if (!res.ok) throw new Error((data && (data.error || data.message)) || ('HTTP ' + res.status));
    return data;
  };
  const ESC = { '&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;' };
  ESC[String.fromCharCode(39)] = '&#39;';
  const esc = (s) => String(s == null ? '' : s).replace(/[&<>"']/g, c => ESC[c]);
  const toast = (msg) => { const el = document.getElementById('toast'); if (!el) { console.log(msg); return; } el.textContent = msg; el.classList.add('show'); clearTimeout(el._t); el._t = setTimeout(() => el.classList.remove('show'), 2600); };
  const stateCls = (s) => 'state-' + String(s || '');
  const STATE_TEXT = {candidate:'候选',evaluating:'评估中',rejected:'已拒',approved:'已批',queued:'排队',running:'执行中',done:'已完成',failed:'失败',timeout:'超时',cancelled:'已取消',interrupted:'中断'};
  const kpi = (l, v) => '<div class="kpi"><div class="v">' + esc(v) + '</div><div class="l">' + esc(l) + '</div></div>';
  const fmtTime = (ms) => ms ? new Date(ms).toLocaleString('zh-CN', {hour12:false}) : '—';
  const fmtMin = (ms) => ms ? (ms/60000).toFixed(1) + ' 分钟' : '—';
  const fmtBytes = (b) => { if(!b) return '0 B'; const u=['B','KB','MB','GB']; let i=0,v=b; while(v>=1024&&i<u.length-1){v/=1024;i++;} return v.toFixed(i?1:0)+' '+u[i]; };

  // ── 首页：一个自动按钮（自动加载 + 自动响应 + 状态反馈）──
  if (!isAdmin) {
    const btn = document.getElementById('startBtn');
    if (!btn) { return; }
    const label = btn.querySelector('.btn-label');
    const live = document.getElementById('homeStatus');
    const PHASES = {
      idle:    { text: '开始运行', hint: '待启动',     busy: false, lock: false },
      busy:    { text: '启动中',   hint: '正在启动引擎', busy: true,  lock: true  },
      running: { text: '运行中',   hint: '引擎运行中',   busy: false, lock: true  },
      paused:  { text: '已暂停',   hint: '引擎已暂停',   busy: false, lock: true  },
      stopped: { text: '重新启动', hint: '引擎已停止',   busy: false, lock: false },
      error:   { text: '重试',     hint: '启动失败，可重试', busy: false, lock: false },
    };
    let current = btn.getAttribute('data-state') || 'idle';
    let inflight = false;
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
    let fails = 0;
    const ping = async () => {
      try {
        const st = await api('/api/status');
        fails = 0;
        const phase = (st && st.phase) || 'idle';
        paint(PHASES[phase] ? phase : 'idle');
      } catch (e) {
        fails += 1;
        if (current !== 'busy' && current !== 'error') { paint(fails >= 3 ? 'error' : current); }
      }
    };
    const start = async () => {
      if (inflight) { return; }
      inflight = true;
      paint('busy');
      try {
        await api('/api/engine/start', { method: 'POST', body: '{}' });
        paint('running');
        toast('引擎已启动，正在自动产出');
      } catch (e) {
        paint('error');
        toast('启动失败：' + e.message);
        setTimeout(() => { if (current === 'error') { ping(); } }, 2500);
      } finally {
        inflight = false;
      }
    };
    btn.addEventListener('click', start);
    // 自动加载：脚本一执行就进入加载态（不等任何网络返回）；
    // 随后由 /api/status 自动收敛到真实阶段。
    paint('busy');
    ping();
    setInterval(ping, 4000);
    // 自动触发：开屏发现引擎未在运行即自动点一次（仅一次，失败不循环）。
    setTimeout(() => { if (current === 'idle' || current === 'stopped') { start(); } }, 900);
    return;
  }

  // ── 管理面板 ──────────────────────────────────────────
  const panes = {
    overview: document.getElementById('tab-overview'),
    queue: document.getElementById('tab-queue'),
    outputs: document.getElementById('tab-outputs'),
    templates: document.getElementById('tab-templates'),
    autonomy: document.getElementById('tab-autonomy'),
    evaluate: document.getElementById('tab-evaluate'),
    logs: document.getElementById('tab-logs'),
    governance: document.getElementById('tab-governance'),
    workspace: document.getElementById('tab-workspace'),
    settings: document.getElementById('tab-settings'),
  };
  const cache = { status:null, tasks:[], templates:[], config:null };

  async function renderOverview() {
    const st = await api('/api/status');
    cache.status = st;
    const s = st.stats || {};
    const models = (st.models || []).map(m => {
      const total = (m.success || 0) + (m.failure || 0);
      const score = total ? Math.round(100 * m.success / total) : 70;
      const cd = m.cooldownTo && m.cooldownTo > Date.now();
      const statusHtml = cd ? '<span class="tag" style="color:var(--warn)">冷却中</span>' : '<span class="state-done">可用</span>';
      return '<tr><td>' + esc(m.model) + '</td><td>' + esc(m.tier || '-') + '</td><td>' + (m.success||0) + ' / ' + (m.failure||0) + '</td><td>' + score + '</td><td>' + statusHtml + '</td></tr>';
    }).join('');
    const phaseText = {running:'运行中',paused:'已暂停',stopped:'已停止',idle:'待启动'}[st.phase] || st.phase;
    const failRes = await api('/api/failures').catch(() => ({buckets:[], total:0, empty:true}));
    const metRes = await api('/api/metrics').catch(() => null);
    const KIND_TEXT = {timeout:'超时',upstream_failed:'上游失败',bad_exit:'异常退出',stalled:'卡死',unknown:'未知',missing_credential:'缺凭据',model_unavailable:'模型下线',boot_failed:'启动失败',cancelled:'已取消',done:'成功'};
    const buckets = (failRes.buckets || []).map(function(b) {
      var total = Math.max(1, failRes.total || 1);
      var pct = Math.round(100 * b.count / total);
      return '<tr><td>' + esc(KIND_TEXT[b.kind] || b.kind) + '</td><td>' + b.count + '</td><td>' +
        '<div style="background:var(--line);height:8px;border-radius:4px;overflow:hidden"><div style="width:' + pct + '%;height:100%;background:var(--danger,#e5534b)"></div></div></td>' +
        '<td>' + pct + '%</td><td class="muted">' + fmtTime(b.lastAt) + '</td></tr>';
    }).join('');
    const failCard = (failRes.empty
      ? '<div class="card"><h3>失败原因分布</h3><div class="muted">暂无失败样本（这通常是好消息；有失败任务后这里会按 ErrorKind 分类统计）</div></div>'
      : '<div class="card"><h3>失败原因分布（共 ' + failRes.total + ' 条）</h3><table><thead><tr><th>类别</th><th>数量</th><th>占比</th><th>百分比</th><th>最近发生</th></tr></thead><tbody>' + buckets + '</tbody></table></div>');
    const m = metRes || {};
    const metricsCard = '<div class="card"><h3>运行时指标</h3><div class="grid c4">' +
      kpi('堆内存', (m.heapMB || 0).toFixed(1) + ' MB') + kpi('goroutine', m.goroutine || 0) +
      kpi('句柄', m.handles || 0) + kpi('GC 次数', m.numGC || 0) + '</div>' +
      '<div class="muted" style="margin-top:8px">上限：内存 ' + (m.memLimitMB || 0) + ' MB / 句柄 ' + (m.handleLimit || 0) + '；采样时间 ' + fmtTime(m.at) + '</div></div>';
    panes.overview.innerHTML =
      '<div class="grid c4">' +
        kpi('引擎状态', phaseText) + kpi('今日产出', s.todayDone || 0) + kpi('队列中', s.queuedTasks || 0) + kpi('累计完成', s.doneTasks || 0) +
        kpi('成功率', (s.successRate || 0).toFixed(1) + '%') + kpi('平均耗时', (s.avgSeconds || 0).toFixed(0) + ' 秒') +
        kpi('产出总量', fmtBytes(s.totalBytes || 0)) + kpi('降级任务', s.degradedTasks || 0) +
        kpi('验收率', (s.doneTasks ? (100 * (s.verifiedTasks || 0) / s.doneTasks).toFixed(1) : '0.0') + '%') + kpi('被拒任务', s.rejectedTasks || 0) +
      '</div>' +
      '<div class="card" style="margin-top:16px"><h3>模型健康度</h3><table><thead><tr><th>模型</th><th>层级</th><th>成功/失败</th><th>健康分</th><th>状态</th></tr></thead><tbody>' + (models || '<tr><td colspan="5" class="empty">无模型</td></tr>') + '</tbody></table></div>' +
      metricsCard +
      failCard +
      (st.lastError ? '<div class="card"><h3>最近错误</h3><div class="muted">' + esc(st.lastError) + '</div></div>' : '') +
      '<div class="card"><h3>快捷操作</h3><div class="row"><button class="btn primary" onclick="WB.start()">启动引擎</button><button class="btn" onclick="WB.diagnose()">系统自检</button><button class="btn" onclick="WB.exportAll()">导出全部产出</button></div></div>';
  }

  async function renderQueue() {
    const queueRes = await api('/api/queue');
    const tasksRes = await api('/api/tasks?limit=600');
    cache.tasks = tasksRes.tasks || [];
    const active = cache.tasks.filter(t => ['queued','running','approved'].indexOf(t.state) >= 0);
    const rows = (queueRes.tasks || []).map(t => {
      const st = '<span class="' + stateCls(t.state) + '">' + (STATE_TEXT[t.state] || t.state) + '</span>';
      return '<tr><td>' + esc(t.title) + '</td><td>' + esc(t.domain || '-') + '</td><td>' + (t.priority || 0) + '</td><td>' + st + '</td>' +
        '<td class="row" style="gap:4px"><button class="btn sm" onclick="WB.priority(&apos;' + t.id + '&apos;)">置顶</button>' +
        '<button class="btn sm" onclick="WB.cancel(&apos;' + t.id + '&apos;)">终止</button></td></tr>';
    }).join('');
    const activeRows = active.slice(0, 80).map(t => {
      const st = '<span class="' + stateCls(t.state) + '">' + (STATE_TEXT[t.state] || t.state) + '</span>';
      const ops = (t.state === 'running' ? '<button class="btn sm" onclick="WB.cancel(&apos;' + t.id + '&apos;)">终止</button>' : '') +
        '<button class="btn sm" onclick="WB.openTask(&apos;' + t.id + '&apos;)">详情</button>';
      return '<tr><td>' + esc(t.title) + '</td><td>' + st + '</td><td>' + fmtTime(t.startedAt || t.updatedAt) + '</td><td class="row" style="gap:4px">' + ops + '</td></tr>';
    }).join('');
    panes.queue.innerHTML =
      '<div class="card"><h3>调度队列（优先级 + 先进先出）</h3><table><thead><tr><th>标题</th><th>领域</th><th>优先级</th><th>状态</th><th>操作</th></tr></thead><tbody>' +
      (rows || '<tr><td colspan="5" class="empty">队列为空</td></tr>') + '</tbody></table></div>' +
      '<div class="card"><h3>进行中 / 待执行</h3><table><thead><tr><th>标题</th><th>状态</th><th>更新时间</th><th>操作</th></tr></thead><tbody>' +
      (activeRows || '<tr><td colspan="4" class="empty">无活跃任务</td></tr>') + '</tbody></table></div>' +
      '<div class="card"><h3>手动添加任务</h3>' +
      '<label>标题</label><input id="mTitle" placeholder="例如：整理本周 AI 工具评测">' +
      '<label>任务描述</label><textarea id="mPrompt" rows="4" placeholder="描述要产出的成果、格式与要求"></textarea>' +
      '<label>领域</label><select id="mDomain"><option value="content">内容创作</option><option value="code">代码生成</option><option value="knowledge">知识整理</option><option value="data">数据处理</option><option value="solution">方案撰写</option><option value="report">报告生成</option></select>' +
      '<div style="margin-top:12px"><button class="btn primary" onclick="WB.manual()">评估并加入队列</button></div></div>';
  }

  // P3 验收徽章：只有真实执行验收通过的产出才算"可运行"。
  // 历史任务无验收证据、降级任务验收失败，都必须如实区分，不能一律标成功。
  const verifyBadge = (t) => {
    if (t.verified) return '<span class="state-done" title="verify 命令全部通过">✔ 已验收</span>';
    if (t.state === 'degraded') return '<span class="state-failed" title="验收未通过">✘ 验收未过</span>';
    if (t.verify) return '<span class="state-failed">✘ 未通过</span>';
    return '<span class="muted" title="改造前的历史任务，无验收证据">未验收</span>';
  };
  const repairBadge = (t) => {
    const n = t.repairRounds || 0;
    return n > 0 ? '<span class="tag">返工 ' + n + ' 轮</span>' : '—';
  };

  async function renderOutputs() {
    const res = await api('/api/tasks?limit=3000');
    cache.tasks = res.tasks || [];
    const done = cache.tasks.filter(t => t.state === 'done');
    const degraded = cache.tasks.filter(t => t.state === 'degraded');
    const verifiedN = done.filter(t => t.verified).length;
    const rowOf = (t) => {
      const q = t.critique ? t.critique.quality + '/10' : '—';
      return '<tr><td>' + esc(t.title) + '</td><td>' + esc(t.domain || '-') + '</td><td>' + fmtMin(t.elapsedMs) + '</td>' +
        '<td>' + q + '</td><td>' + verifyBadge(t) + '</td><td>' + repairBadge(t) + '</td><td>' + fmtBytes(t.outputBytes) + '</td>' +
        '<td class="row" style="gap:4px"><button class="btn sm" onclick="WB.openTask(&apos;' + t.id + '&apos;)">查看</button>' +
        (t.state === 'done' ? '<button class="btn sm" onclick="WB.rate(&apos;' + t.id + '&apos;,5)">★5</button>' : '') + '</td></tr>';
    };
    const head = '<table><thead><tr><th>标题</th><th>领域</th><th>耗时</th><th>自评</th><th>验收</th><th>返工</th><th>大小</th><th>操作</th></tr></thead><tbody>';
    panes.outputs.innerHTML =
      '<div class="card"><h3>已完成产出（' + done.length + '，其中已验收 ' + verifiedN + '）</h3>' +
      '<div class="muted" style="margin-bottom:10px">「已验收」= 产物已落盘为真实文件，且 verify 命令在本机真实执行通过。' +
      '降级产出保留供查看，但不计入今日产出、不触发后继链。</div>' +
      '<div class="row" style="margin-bottom:12px">' +
      '<button class="btn primary" onclick="WB.exportAll(&apos;md&apos;)">导出全部 Markdown</button>' +
      '<button class="btn" onclick="WB.exportAll(&apos;txt&apos;)">导出全部 TXT</button>' +
      '<button class="btn" onclick="WB.exportAll(&apos;docx&apos;)">导出全部 Word</button></div>' +
      head + (done.map(rowOf).join('') || '<tr><td colspan="8" class="empty">还没有产出，点击首页「开始运行」即可</td></tr>') +
      '</tbody></table></div>' +
      (degraded.length ? '<div class="card"><h3>降级产出（' + degraded.length + '）— 有产出但验收未通过</h3>' +
        head + degraded.map(rowOf).join('') + '</tbody></table></div>' : '') +
      '<div class="card" id="taskDetail"></div>';
  }

  async function renderTemplates() {
    const res = await api('/api/templates');
    cache.templates = res.templates || [];
    const rows = cache.templates.map(t => {
      const enabled = t.enabled ? '<span class="state-done">启用</span>' : '<span class="muted">停用</span>';
      return '<tr><td>' + esc(t.name) + '</td><td>' + esc(t.domain) + '</td><td>' + t.priorityWeight + '</td><td>' + (t.valuePrior || 0) + '</td><td>' + enabled + '</td>' +
        '<td class="row" style="gap:4px"><button class="btn sm" onclick="WB.editTemplate(&apos;' + t.id + '&apos;)">编辑</button>' +
        '<button class="btn sm" onclick="WB.deleteTemplate(&apos;' + t.id + '&apos;)">删除</button></td></tr>';
    }).join('');
    panes.templates.innerHTML =
      '<div class="card"><h3>任务模板库（' + cache.templates.length + '）</h3><div class="row" style="margin-bottom:12px">' +
      '<button class="btn primary" onclick="WB.newTemplate()">新建模板</button>' +
      '<button class="btn" onclick="WB.resetTemplates()">恢复默认</button></div>' +
      '<table><thead><tr><th>名称</th><th>领域</th><th>优先级</th><th>价值先验</th><th>状态</th><th>操作</th></tr></thead><tbody>' + rows + '</tbody></table></div>' +
      '<div class="card" id="templateEditor"></div>';
  }

  async function renderEvaluate() {
    const res = await api('/api/eval/config');
    const e = res.eval || {};
    const w = e.weights || {};
    panes.evaluate.innerHTML =
      '<div class="card"><h3>三维评估权重与阈值</h3><div class="grid c2">' +
      '<div><label>产出价值权重</label><input id="wV" type="number" step="0.05" min="0" max="1" value="' + (w.value || 0.5) + '">' +
      '<label>资源成本权重</label><input id="wC" type="number" step="0.05" min="0" max="1" value="' + (w.cost || 0.2) + '">' +
      '<label>可行性权重</label><input id="wF" type="number" step="0.05" min="0" max="1" value="' + (w.feasibility || 0.3) + '"></div>' +
      '<div><label>自动批准阈值（0-100）</label><input id="th" type="number" min="1" max="100" value="' + (e.threshold || 60) + '">' +
      '<label>每日任务上限</label><input id="cap" type="number" min="1" value="' + (e.dailyCap || 200) + '">' +
      '<label>相似度去重阈值（0-1）</label><input id="sim" type="number" step="0.01" min="0.5" max="1" value="' + (e.similarityCap || 0.9) + '"></div></div>' +
      '<div class="row" style="margin-top:14px">' +
      '<label style="flex:0 0 auto;margin:0"><input id="semantic" type="checkbox" style="width:auto;margin-right:8px" ' + (e.semanticOn ? 'checked' : '') + '>启用语义判分（LLM）</label>' +
      '<label style="flex:0 0 auto;margin:0"><input id="repeat" type="checkbox" style="width:auto;margin-right:8px" ' + (e.repeatGate ? 'checked' : '') + '>重复任务硬门禁</label></div>' +
      '<div style="margin-top:16px"><button class="btn primary" onclick="WB.saveEval()">保存评估配置</button></div></div>' +
      '<div class="card"><h3>试评估（不产生任务）</h3>' +
      '<label>标题</label><input id="pTitle" placeholder="任务标题">' +
      '<label>任务描述</label><textarea id="pPrompt" rows="4" placeholder="任务 prompt"></textarea>' +
      '<label>领域</label><select id="pDomain"><option value="content">内容创作</option><option value="code">代码生成</option><option value="knowledge">知识整理</option><option value="data">数据处理</option><option value="solution">方案撰写</option><option value="report">报告生成</option></select>' +
      '<div style="margin-top:12px"><button class="btn primary" onclick="WB.previewEval()">试评估</button></div>' +
      '<div id="previewResult" style="margin-top:14px"></div></div>';
  }

  async function renderLogs() {
    const audit = await api('/api/logs?tail=200&file=audit.jsonl');
    const tasksRes = await api('/api/tasks?limit=80');
    cache.tasks = tasksRes.tasks || [];
    const rows = (audit.lines || []).slice().reverse().map(l => {
      try {
        const o = JSON.parse(l);
        const ev = (o.from ? o.from + ' → ' : '') + (o.to || o.kind || '');
        return '<tr><td>' + fmtTime(o.at) + '</td><td>' + esc(o.actor || '') + '</td><td>' + esc(o.taskId || '') + '</td><td>' + esc(ev) + '</td><td>' + esc(o.message || '') + '</td></tr>';
      } catch (e) {
        return '<tr><td colspan="5" class="muted">' + esc(l) + '</td></tr>';
      }
    }).join('');
    const failed = cache.tasks.filter(t => t.state === 'failed' || t.state === 'timeout').map(t =>
      '<tr><td>' + esc(t.title) + '</td><td>' + esc(t.state) + '</td><td class="muted">' + esc((t.error || '').slice(0, 180)) + '</td>' +
      '<td><button class="btn sm" onclick="WB.retry(&apos;' + t.id + '&apos;)">重试</button></td></tr>').join('');
    panes.logs.innerHTML =
      '<div class="card"><h3>审计日志（最近 200 条）</h3><table><thead><tr><th>时间</th><th>操作者</th><th>任务</th><th>事件</th><th>说明</th></tr></thead><tbody>' +
      (rows || '<tr><td colspan="5" class="empty">暂无日志</td></tr>') + '</tbody></table></div>' +
      '<div class="card"><h3>错误任务</h3><table><thead><tr><th>标题</th><th>状态</th><th>错误</th><th>操作</th></tr></thead><tbody>' +
      (failed || '<tr><td colspan="4" class="empty">无错误任务</td></tr>') + '</tbody></table></div>';
  }

  async function renderSettings() {
    const cfgRes = await api('/api/settings');
    const autoRes = await api('/api/autostart');
    const c = cfgRes.config || {};
    const ex = c.executor || {};
    const ch = c.chain || {};
    cache.config = c;
    panes.settings.innerHTML =
      '<div class="card"><h3>执行与并发</h3><div class="grid c2">' +
      '<div><label>并发执行数（1-6）</label><input id="conc" type="number" min="1" max="6" value="' + (ex.concurrency || 2) + '">' +
      '<label>单任务默认超时（分钟）</label><input id="timeout" type="number" min="1" max="120" value="' + (ex.defaultTimeout || 30) + '">' +
      '<label>失败重试次数</label><input id="retries" type="number" min="0" max="10" value="' + (ex.maxRetries || 3) + '"></div>' +
      '<div><label>链最大深度</label><input id="chainDepth" type="number" min="1" max="20" value="' + (ch.maxDepth || 5) + '">' +
      '<label>每链扇出</label><input id="chainFan" type="number" min="0" max="4" value="' + (ch.fanout || 2) + '">' +
      '<label>链最低质量分（低于则停止延伸）</label><input id="chainQ" type="number" min="1" max="10" value="' + (ch.minQuality || 6) + '"></div></div>' +
      '<div style="margin-top:16px"><button class="btn primary" onclick="WB.saveSettings()">保存设置</button></div></div>' +
      '<div class="card"><h3>开机自启</h3><div class="row">' +
      '<label style="margin:0;flex:0 0 auto"><input id="autostart" type="checkbox" style="width:auto;margin-right:8px" ' + (autoRes.enabled ? 'checked' : '') + '>随 Windows 启动（后台静默，恢复上次运行状态）</label>' +
      '<button class="btn" style="flex:0 0 auto" onclick="WB.saveAutostart()">应用</button></div></div>' +
      '<div class="card"><h3>运行信息</h3><table><tbody>' +
      '<tr><td>面板端口</td><td>' + esc(cfgRes.port) + '</td></tr>' +
      '<tr><td>DSH profile</td><td>' + esc(ex.profileName || 'web-desktop') + '</td></tr>' +
      '<tr><td>开机自启</td><td>' + (autoRes.enabled ? '已开启' : '未开启') + (autoRes.error ? '（' + esc(autoRes.error) + '）' : '') + '</td></tr>' +
      '</tbody></table></div>' +
      '<div class="card"><h3>维护</h3><div class="row"><button class="btn" onclick="WB.diagnose()">打印自检报告</button>' +
      '<button class="btn danger" onclick="WB.stopEngine()">停止引擎</button></div><pre id="diagOut" style="margin-top:12px;display:none"></pre></div>';
  }

  // ── 交互动作 ─────────────────────────────────────────
  const WB = {
    async start() { await api('/api/engine/start', { method: 'POST', body: '{}' }); toast('引擎已启动'); renderOverview(); },
    async stopEngine() { await api('/api/engine/stop', { method: 'POST', body: '{}' }); toast('正在停止引擎'); },
    async pause() { try { await api('/api/engine/pause', { method: 'POST', body: '{}' }); toast('已暂停'); refreshTop(); } catch (e) { toast(e.message); } },
    async resume() { try { await api('/api/engine/resume', { method: 'POST', body: '{}' }); toast('已继续'); refreshTop(); } catch (e) { toast(e.message); } },
    async cancel(id) { try { await api('/api/tasks/' + id + '/cancel', { method: 'POST', body: '{}' }); toast('已请求终止'); renderQueue(); } catch (e) { toast(e.message); } },
    async retry(id) { try { await api('/api/tasks/' + id + '/retry', { method: 'POST', body: '{}' }); toast('已重新入队'); renderLogs(); } catch (e) { toast(e.message); } },
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
        if (!box) { return; }
        const q = t.critique ? ' ｜ 自评：' + t.critique.quality + '/10' : '';
        box.innerHTML = '<h3>' + esc(t.title) + '</h3>' +
          '<div class="muted" style="margin-bottom:10px">状态：' + esc(STATE_TEXT[t.state] || t.state) + ' ｜ 领域：' + esc(t.domain || '-') +
          ' ｜ 模型：' + esc(t.model || '-') + ' ｜ 耗时：' + fmtMin(t.elapsedMs) + q + '</div>' +
          '<div class="doc">' + mdToHtml(res.content || '（暂无产出）') + '</div>' +
          '<div style="margin-top:12px" class="row"><button class="btn sm" onclick="WB.rate(&apos;' + t.id + '&apos;,5)">★★★★★</button>' +
          '<button class="btn sm" onclick="WB.rate(&apos;' + t.id + '&apos;,4)">★★★★</button>' +
          '<button class="btn sm" onclick="WB.rate(&apos;' + t.id + '&apos;,3)">★★★</button></div>';
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
      box.innerHTML = '<h3>新建模板</h3>' +
        '<label>ID（英文唯一）</label><input id="tId" placeholder="my-template">' +
        '<label>名称</label><input id="tName" placeholder="我的模板">' +
        '<label>领域</label><select id="tDomain"><option value="content">内容创作</option><option value="code">代码生成</option><option value="knowledge">知识整理</option><option value="data">数据处理</option><option value="solution">方案撰写</option><option value="report">报告生成</option></select>' +
        '<label>任务描述</label><input id="tDesc" placeholder="这个模板产出什么">' +
        '<label>Prompt 模板（必须含 {topic}）</label><textarea id="tPrompt" rows="6" placeholder="请围绕「{topic}」产出……"></textarea>' +
        '<label>优先级权重（1-5）</label><input id="tPriority" type="number" min="1" max="5" value="3">' +
        '<label>价值先验（0-100）</label><input id="tValue" type="number" min="0" max="100" value="75">' +
        '<div style="margin-top:12px"><button class="btn primary" onclick="WB.saveTemplate()">保存模板</button></div>';
    },
    editTemplate(id) {
      const t = cache.templates.find(x => x.id === id);
      if (!t) { return; }
      const box = document.getElementById('templateEditor');
      box.innerHTML = '<h3>编辑模板：' + esc(t.name) + '</h3>' +
        '<label>名称</label><input id="tName" value="' + esc(t.name) + '">' +
        '<label>任务描述</label><input id="tDesc" value="' + esc(t.description || '') + '">' +
        '<label>Prompt 模板</label><textarea id="tPrompt" rows="6">' + esc(t.promptTemplate || '') + '</textarea>' +
        '<label>优先级权重（1-5）</label><input id="tPriority" type="number" min="1" max="5" value="' + (t.priorityWeight || 3) + '">' +
        '<label>价值先验（0-100）</label><input id="tValue" type="number" min="0" max="100" value="' + (t.valuePrior || 75) + '">' +
        '<label>是否启用</label><select id="tEnabled"><option value="1"' + (t.enabled ? ' selected' : '') + '>启用</option><option value="0"' + (t.enabled ? '' : ' selected') + '>停用</option></select>' +
        '<div style="margin-top:12px"><button class="btn primary" onclick="WB.saveTemplate(&apos;' + id + '&apos;)">保存修改</button></div>';
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
        tpl = JSON.parse(JSON.stringify(cache.templates.find(x => x.id === id)));
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
    async deleteTemplate(id) {
      if (!confirm('确认删除或停用该模板？')) { return; }
      try { await api('/api/templates/' + id, { method: 'DELETE' }); toast('已处理'); renderTemplates(); } catch (e) { toast(e.message); }
    },
    async resumeAutonomy(goalId) {
      try { await api('/api/autonomy/resume', { method: 'POST', body: JSON.stringify({ goalId: goalId }) }); toast('已恢复'); render('autonomy'); }
      catch (e) { toast('恢复失败：' + e.message); }
    },
    async setAutonomyGoal(title, intent, domain) {
      try { await api('/api/autonomy/goal', { method: 'POST', body: JSON.stringify({ title: title, intent: intent, domain: domain }) }); toast('目标已设置'); render('autonomy'); }
      catch (e) { toast('设置失败：' + e.message); }
    },
    async resetTemplates() {
      if (!confirm('恢复为内置默认模板？自定义模板会被清除。')) { return; }
      try { await api('/api/templates/reset', { method: 'POST', body: '{}' }); toast('已恢复默认'); renderTemplates(); } catch (e) { toast(e.message); }
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
        await renderWorkspace();
      } catch (e) { toast('切换失败：' + e.message); }
    },
    async wsReset() {
      if (!confirm('切回项目模式？之后的任务会在工作台沙箱目录里执行，不再写你的项目文件夹。')) { return; }
      try {
        await api('/api/workspace/reset', { method: 'POST', body: '{}' });
        toast('已切回项目模式');
        await renderWorkspace();
      } catch (e) { toast('切换失败：' + e.message); }
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
      if (raw.trim().indexOf('` + "`" + `` + "`" + `` + "`" + `') === 0) { inCode = !inCode; html += inCode ? '<pre>' : '</pre>'; continue; }
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

  // 治理页签：把治理总览渲染进 #tab-governance（与独立 /governance 页共用同一份 API）
  async function renderGovernance() {
    const pane = document.getElementById('tab-governance');
    if (!pane) { return; }
    let sum;
    try {
      sum = await api('/api/gov/summary');
    } catch (e) {
      pane.innerHTML = '<p class="gov-dim">治理子系统未启用或加载失败：' + esc(e.message) + '</p>';
      return;
    }
    const lvName = (l) => (l === 'red' ? '红区' : l === 'warn' ? '黄区' : l === 'unverified' ? '未验证' : '绿区');
    const lvClass = (l) => (l === 'red' ? 'lv-red' : l === 'warn' ? 'lv-warn' : l === 'unverified' || l === 'unknown' ? 'lv-unk' : 'lv-ok');
    const c = sum.contracts, v = sum.volume, ck = sum.checklist || {};
    const parts = [];
    parts.push('<div class="gov-banner ' + lvClass(sum.level) + '">治理总等级：' + lvName(sum.level) +
      ' ｜ 契约 ' + c.total + ' 份（在履约 ' + c.active + '）｜ 体量采样 ' + v.samples + ' 点 ｜ 考核 ' +
      ck.pass + ' PASS / ' + ck.fail + ' FAIL / ' + (ck.unverified || 0) + ' 未验证</div>');

    const stageRows = (sum.stageBuckets || []).map((b) => '<div class="gov-kv"><span>' + esc(b.name) + '</span><b>' + b.count + ' 份</b></div>').join('');
    parts.push('<section class="gov-card"><h3>契约全生命周期</h3><div class="gov-grid">' +
      '<div class="gov-kv"><span>总量</span><b>' + c.total + '</b></div>' +
      '<div class="gov-kv"><span>签署/履约/归档</span><b>' + c.signed + '/' + c.performing + '/' + c.archived + '</b></div>' +
      '<div class="gov-kv"><span>合规均分</span><b>' + c.complianceAvg + '</b></div>' +
      '<div class="gov-kv"><span>逾期/临期</span><b>' + c.overdue + '/' + c.expiringSoon + '</b></div>' +
      stageRows + '</div></section>');

    const domainRows = (sum.domains || []).map((d) => '<div class="gov-row ' + lvClass(d.level) + '"><b>' + esc(d.name) +
      '</b><span>' + lvName(d.level) + '</span><span>绿 ' + d.ok + ' / 黄 ' + d.warn + ' / 红 ' + d.red + ' / 未验证 ' + d.unknown +
      '</span><span>峰值占用 ' + Math.round(d.peakRatio * 100) + '%</span></div>').join('');
    parts.push('<section class="gov-card"><h3>体量治理三域</h3>' + (domainRows || '<p class="gov-dim">暂无采样（未验证）</p>') + '</section>');

    const evalRows = (sum.evals || []).map((e) => '<div class="gov-row ' + lvClass(e.level) + '"><b>' + esc(e.name) +
      '</b><span>' + e.value + esc(e.unit) + '</span><span>警示 ' + e.warn + ' / 红线 ' + e.red + '</span><span>' + lvName(e.level) + '</span></div>').join('');
    parts.push('<section class="gov-card"><h3>量化指标（阈值与红线）</h3>' + (evalRows || '<p class="gov-dim">暂无采样（未验证）</p>') + '</section>');

    const alertRows = (sum.alerts.top || []).map((a) => '<div class="gov-row ' + lvClass(a.severity === 'critical' ? 'red' : a.severity) +
      '"><b>' + esc(a.title) + '</b><span>' + esc(a.detail) + '</span><span class="gov-dim">处置：' + esc(a.suggestion) + '</span></div>').join('');
    parts.push('<section class="gov-card"><h3>风险预警（' + sum.alerts.total + ' 条：红 ' + sum.alerts.critical + ' / 黄 ' + sum.alerts.warn + '）</h3>' +
      (alertRows || '<p class="gov-dim">当前无预警</p>') + '</section>');

    const adjRows = (sum.adjustments || []).map((a) => '<div class="gov-row"><b>' + esc(a.metric) + '</b><span>' + esc(a.kind) +
      '</span><span>' + esc(a.action) + '</span><span class="gov-dim">' + esc(a.basis) + '</span></div>').join('');
    parts.push('<section class="gov-card"><h3>自适应调整建议</h3>' + (adjRows || '<p class="gov-dim">无调整建议（样本不足或全绿）</p>') + '</section>');

    const lk = sum.linkage || {};
    const sigRows = (lk.signals || []).filter((s) => s.severity !== 'info').map((s) => '<div class="gov-row ' +
      lvClass(s.severity === 'critical' ? 'red' : s.severity) + '"><b>' + esc(s.title) + '</b><span>' + esc(s.detail) +
      '</span><span class="gov-dim">决策：' + esc(s.decision) + '</span></div>').join('');
    parts.push('<section class="gov-card"><h3>契约 × 体量 联动分析</h3><div class="gov-grid">' +
      '<div class="gov-kv"><span>契约负载率</span><b>' + Math.round((lk.loadRatio || 0) * 100) + '%</b></div>' +
      '<div class="gov-kv"><span>负载×耗时相关</span><b>' + lk.corrCycle + '</b></div>' +
      '<div class="gov-kv"><span>负载×失败相关</span><b>' + lk.corrFailure + '</b></div>' +
      '<div class="gov-kv"><span>联动等级</span><b>' + lvName(lk.level) + '</b></div></div>' +
      (sigRows || '<p class="gov-dim">无联动异常信号</p>') + '</section>');

    const ckRows = (ck.items || []).map((it) => '<div class="gov-row ' + lvClass(it.level) + '"><b>' + esc(it.code) + ' ' + esc(it.name) +
      '</b><span>' + (it.level === 'ok' ? 'PASS' : it.level === 'unverified' ? '未验证' : 'FAIL') + '</span><span>目标：' +
      esc(it.target) + '</span><span>实际：' + esc(it.actual) + '</span><span class="gov-dim">' + esc(it.evidence) + '</span></div>').join('');
    parts.push('<section class="gov-card"><h3>考核校验</h3>' + ckRows + '</section>');

    if (sum.dataGaps && sum.dataGaps.length) {
      parts.push('<section class="gov-card"><h3>数据缺口（必须显式声明）</h3><ul>' +
        sum.dataGaps.map((g) => '<li>' + esc(g) + '</li>').join('') + '</ul></section>');
    }
    pane.innerHTML = '<div class="gov-actions"><button class="btn ghost" id="govTabSample">立即采样</button>' +
      '<a class="btn ghost" href="/governance" target="_blank">打开独立治理控制台</a></div>' + parts.join('');
    const btn = document.getElementById('govTabSample');
    if (btn) { btn.onclick = async () => { try { await api('/api/gov/sample', { method: 'POST', body: '{}' }); await renderGovernance(); } catch (e) { toast('采样失败：' + e.message); } }; }
  }

  // 自主页签：项目目标 / 阶段进度 / 验收标准 / 决策台账（全部来自 /api/autonomy*）
  async function renderAutonomy() {
    const pane = document.getElementById('tab-autonomy');
    if (!pane) { return; }
    let sum;
    try { sum = await api('/api/autonomy?decisions=40'); }
    catch (e) { pane.innerHTML = '<p class="muted">自主子系统不可用：' + esc(e.message) + '</p>'; return; }
    if (!sum.enabled) {
      pane.innerHTML = '<div class="card"><h3>自主运行</h3><div class="muted">当前为固定模板模式（配置 autonomy.enabled=false）。' +
        '开启后 AI 会基于项目目标自主推导阶段性任务、自主验收并在目标完成后自动设置下一目标。</div></div>';
      return;
    }
    const active = sum.active;
    const STATUS_TEXT = {active:'进行中',completed:'已完成',blocked:'已阻塞',abandoned:'已放弃'};
    const head = active ? (
      '<div class="grid c4">' +
        kpi('目标状态', STATUS_TEXT[active.status] || active.status) +
        kpi('已完成阶段', (sum.stage || 0) + ' / ' + (active.maxStages || '-')) +
        kpi('目标进度', (sum.progress || 0) + '%') +
        kpi('回退次数', (active.rollbacks || 0) + ' / ' + (active.maxRollbacks || '-')) +
      '</div>' +
      '<div class="card" style="margin-top:16px"><h3>' + esc(active.title) + '</h3>' +
        '<div class="muted">' + esc(active.intent) + '</div>' +
        '<div class="muted" style="margin-top:6px">来源：' + esc(active.source) + ' ｜ 领域：' + esc(active.domain) +
        ' ｜ 创建：' + fmtTime(active.createdAt) + (active.blockReason ? ' ｜ 阻塞原因：' + esc(active.blockReason) : '') + '</div>' +
        (active.status === 'blocked' ? '<div style="margin-top:10px"><button class="btn primary" onclick="WB.resumeAutonomy(&apos;' + esc(active.id) + '&apos;)">恢复该目标</button></div>' : '') +
      '</div>') : '<div class="card"><h3>暂无活跃目标</h3><div class="muted">引擎启动后会自动按配置创建项目级目标。</div></div>';

    const critRows = active ? (active.criteria || []).map(function(c) {
      const pct = c.threshold > 0 ? Math.min(100, Math.round(100 * c.current / c.threshold)) : 0;
      const color = c.passed ? 'var(--ok,#3fb950)' : 'var(--warn,#d29922)';
      return '<tr><td>' + esc(c.desc) + '</td><td>' + (c.kind === 'manual' ? '人工' : (c.passed ? '已通过' : '未通过')) +
        '</td><td>' + c.current + ' / ' + c.threshold +
        '<div style="background:var(--line);height:8px;border-radius:4px;overflow:hidden;margin-top:4px"><div style="width:' + pct + '%;height:100%;background:' + color + '"></div></div></td>' +
        '<td class="muted">' + esc(c.detail || '') + '</td></tr>';
    }).join('') : '';
    const critCard = active ? '<div class="card"><h3>自主验收标准（机器判定）</h3><table><thead><tr><th>标准</th><th>状态</th><th>现值 / 阈值</th><th>说明</th></tr></thead><tbody>' +
      (critRows || '<tr><td colspan="4" class="empty">无验收标准</td></tr>') + '</tbody></table></div>' : '';

    const stageRows = active ? (active.stages || []).map(function(s) {
      return '<tr><td>' + (s.index + 1) + '</td><td>' + esc(s.title) + '</td><td>' + (s.quality || 0) + '/10</td><td>' + (s.words || 0) +
        '</td><td>' + (s.passed ? '<span class="state-done">通过</span>' : '<span class="state-failed">回退</span>') +
        '</td><td class="muted">' + esc(s.note || '') + '</td><td class="muted">' + fmtTime(s.at) + '</td></tr>';
    }).join('') : '';
    const stageCard = '<div class="card"><h3>阶段轨迹（自主执行记录）</h3><table><thead><tr><th>#</th><th>阶段</th><th>质量</th><th>字数</th><th>结果</th><th>说明</th><th>时间</th></tr></thead><tbody>' +
      (stageRows || '<tr><td colspan="7" class="empty">暂无阶段（等待首条自主任务完成）</td></tr>') + '</tbody></table></div>';

    const KIND_TEXT = {plan:'规划',accept:'验收通过',reject:'验收未过',rollback:'阶段回退',goalDone:'目标完成',nextGoal:'下一目标',block:'阻塞',boundReject:'边界拒绝',resume:'人工恢复'};
    const decRows = (sum.decisions || []).map(function(d) {
      return '<tr><td>' + fmtTime(d.at) + '</td><td>' + esc(KIND_TEXT[d.kind] || d.kind) + '</td><td>' + esc(d.reason) +
        '</td><td class="muted">' + esc(d.evidence || '') + '</td><td class="muted">' + esc(d.actor || '') + '</td></tr>';
    }).join('');
    const decCard = '<div class="card"><h3>自主决策台账（最近 ' + (sum.decisions || []).length + ' 条）</h3>' +
      '<div class="muted" style="margin-bottom:8px">每一次自主规划、验收、回退、目标推导都留痕；这是自主运行可审计、可解释的唯一依据。</div>' +
      '<table><thead><tr><th>时间</th><th>决策</th><th>原因</th><th>证据</th><th>主体</th></tr></thead><tbody>' +
      (decRows || '<tr><td colspan="5" class="empty">暂无决策记录</td></tr>') + '</tbody></table></div>';

    const goalRows = (sum.goals || []).map(function(g) {
      return '<tr><td>' + esc(g.title) + '</td><td>' + (STATUS_TEXT[g.status] || g.status) + '</td><td>' + (g.stages || []).length +
        '</td><td>' + g.rollbacks + '</td><td class="muted">' + esc(g.source || '') + '</td><td class="muted">' + fmtTime(g.createdAt) + '</td></tr>';
    }).join('');
    const goalCard = '<div class="card"><h3>目标树（共 ' + (sum.goals || []).length + ' 个）</h3><table><thead><tr><th>目标</th><th>状态</th><th>阶段</th><th>回退</th><th>来源</th><th>创建</th></tr></thead><tbody>' +
      (goalRows || '<tr><td colspan="6" class="empty">暂无目标</td></tr>') + '</tbody></table></div>';

    pane.innerHTML = '<div class="card"><h3>自主运行总览</h3>' +
      '<div class="muted">' + esc(sum.statusLine || '') + '</div>' +
      '<div class="muted" style="margin-top:6px">闭环：项目目标 → 动态规划阶段任务 → 执行 → 自主验收 → 目标完成 → 自动推导下一目标。' +
      '规划产物必须通过边界校验（领域白名单/字数/人工介入关键词）与预算闸门（任务数/阶段数/回退次数）。</div></div>' +
      head + critCard + stageCard + decCard + goalCard;
  }

  // ── 工作目录页签（AI 开发功能扩展：可选外部目录模式） ────────────────
  // 交互：路径输入 + 内置目录选择器（列出子目录/盘符）+ 先校验 + 再切换。
  // 所有写入都经服务端校验与护栏；前端只负责展示原因与建议。
  const wsState = { path: '', browse: null, validation: null, view: null };

  async function renderWorkspace() {
    const pane = panes.workspace;
    if (!pane) { return; }
    let view;
    try { view = await api('/api/workspace'); } catch (e) {
      pane.innerHTML = '<div class="card"><h3>工作目录</h3><div class="muted">接口不可用：' + esc(e.message) + '</div></div>';
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
      : '<span class="state-queued">项目模式（默认，兼容原有行为）</span>';

    const treeRows = (snap.tree || []).slice(0, 200).map(function(e) {
      return '<tr><td>' + (e.isDir ? '📁 ' : '📄 ') + esc(e.path) + '</td><td class="muted">' + (e.isDir ? '目录' : fmtBytes(e.size)) + '</td></tr>';
    }).join('');

    pane.innerHTML =
      '<div class="card"><h3>当前工作目录</h3>' +
        '<div class="muted" style="margin-bottom:8px">' + modeBadge + '</div>' +
        '<table><tbody>' +
          '<tr><td style="width:140px">模式</td><td>' + esc(view.mode) + '</td></tr>' +
          '<tr><td>AI 实际工作目录</td><td><code>' + esc(view.root || '-') + '</code></td></tr>' +
          '<tr><td>工作台数据根</td><td class="muted"><code>' + esc(snap.dataRoot || '-') + '</code></td></tr>' +
          '<tr><td>切换时间</td><td>' + fmtTime(cfg.since) + '</td></tr>' +
        '</tbody></table>' +
        (external
          ? '<div class="row" style="margin-top:12px"><button class="btn ghost" onclick="WB.wsReset()">切回项目模式</button></div>'
          : '<div class="muted" style="margin-top:12px">项目模式下每个任务在独立沙箱目录执行，不会碰你的其它文件夹。选择外部目录后 AI 才会直接在你的项目里创建/修改文件。</div>') +
      '</div>' +

      '<div class="card"><h3>选择工作目录</h3>' +
        '<div class="muted">可以填任意本地绝对路径，也可以用下面的选择器逐层点进去。切换前必须先通过校验：目录存在、可读写、不是系统目录，也不能是工作台自身目录或磁盘根。</div>' +
        '<div class="row" style="margin-top:10px">' +
          '<input id="wsPath" style="flex:1" placeholder="D:\\projects\\my-app" value="' + esc(wsState.path || '') + '">' +
          '<button class="btn" onclick="WB.wsBrowse(document.getElementById(&apos;wsPath&apos;).value)">浏览</button>' +
          '<button class="btn" onclick="WB.wsValidate()">校验</button>' +
          '<button class="btn primary" onclick="WB.wsSelect()">切换到此目录</button>' +
        '</div>' +
        '<div id="wsValidateBox" style="margin-top:10px"></div>' +
      '</div>' +

      '<div class="card" id="wsBrowser"><h3>目录选择器</h3><div class="muted" id="wsBrowseBody">点「浏览」开始（不填路径则列出本机盘符）。</div></div>' +

      (val ? '<div class="card"><h3>最近一次校验</h3>' +
        '<div class="muted">' + (val.allowed ? '<span class="state-done">通过</span>' : '<span class="state-failed">未通过</span>') +
        ' ｜ ' + esc(val.canonical || val.input) + '</div>' +
        '<div class="muted" style="margin-top:6px">存在：' + (val.exists ? '是' : '否') + ' ｜ 目录：' + (val.isDir ? '是' : '否') +
        ' ｜ 可读：' + (val.readable ? '是' : '否') + ' ｜ 可写：' + (val.writable ? '是' : '否') +
        ' ｜ 入口：' + (val.entries || 0) + ' 项（目录 ' + (val.dirs || 0) + ' / 文件 ' + (val.files || 0) + '）' +
        ' ｜ 检查时间：' + fmtTime(val.checkedAt) + '</div>' +
        (val.reason ? '<div class="muted" style="margin-top:6px;color:var(--danger,#e5534b)">原因：' + esc(val.reason) + '</div>' : '') +
        (val.suggestion ? '<div class="muted" style="margin-top:4px">建议：' + esc(val.suggestion) + '</div>' : '') +
        ((val.sampleFiles || []).length ? '<div class="muted" style="margin-top:4px">样例文件：' + esc((val.sampleFiles || []).join('、')) + '</div>' : '') +
      '</div>' : '') +

      '<div class="card"><h3>目录内容（' + (snap.entries || 0) + ' 项' + (snap.treeTruncated ? '，已截断展示' : '') + '）</h3>' +
        '<table><thead><tr><th>路径</th><th>类型/大小</th></tr></thead><tbody>' +
        (treeRows || '<tr><td colspan="2" class="empty">目录为空或不可列</td></tr>') + '</tbody></table>' +
      '</div>' +

      '<div class="card"><h3>访问审计（允许 ' + (audit.allowed || 0) + ' / 拒绝 ' + (audit.denied || 0) + '）</h3>' +
        '<div class="muted">每一次路径校验与读写裁决都会落盘到 <code>' + esc(snap.auditPath || '-') + '</code>，越权尝试可追溯。</div>' +
        (audit.lastDeny ? '<div class="muted" style="margin-top:6px;color:var(--danger,#e5534b)">最近拒绝：' + esc(audit.lastDeny) + '</div>' : '<div class="muted" style="margin-top:6px">暂无拒绝记录</div>') +
        ((audit.reasons || []).length ? '<div class="muted" style="margin-top:6px">拒绝原因：' + esc((audit.reasons || []).join('；')) + '</div>' : '') +
        '<div class="row" style="margin-top:10px"><button class="btn ghost" onclick="WB.wsAudit()">查看审计明细</button></div>' +
        '<pre id="wsAuditOut" style="display:none;margin-top:10px;max-height:260px;overflow:auto"></pre>' +
      '</div>';

    if (wsState.browse) { renderBrowseBox(wsState.browse); }
  }

  function renderBrowseBox(b) {
    const box = document.getElementById('wsBrowseBody');
    const outer = document.getElementById('wsBrowser');
    if (!box || !outer) { return; }
    const drives = (b.drives || []).map(function(d) {
      return '<button class="btn sm" onclick="WB.wsBrowse(\'' + esc(d) + '\')">' + esc(d) + '</button>';
    }).join(' ');
    const items = (b.entries || []).map(function(e) {
      return '<button class="btn sm" style="margin:4px" onclick="WB.wsBrowse(\'' + esc(e.path) + '\')">📁 ' + esc(e.name) + '</button>';
    }).join('');
    box.innerHTML =
      '<div class="muted">当前位置：<code>' + esc(b.path || '（盘符列表）') + '</code></div>' +
      '<div class="row" style="margin:8px 0">' +
        (b.parent ? '<button class="btn sm" onclick="WB.wsBrowse(\'' + esc(b.parent) + '\')">⬆ 上一级</button>' : '') +
        '<button class="btn sm" onclick="WB.wsUseBrowse()">用此目录</button>' +
      '</div>' +
      (drives ? '<div style="margin-bottom:8px">' + drives + '</div>' : '') +
      '<div style="max-height:240px;overflow:auto">' + (items || '<span class="muted">没有子目录</span>') + '</div>' +
      (b.reason ? '<div class="muted" style="color:var(--danger,#e5534b)">' + esc(b.reason) + '</div>' : '');
  }

  // 校验结果渲染（校验与切换共用）
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

  // Tab 切换
  const tabs = document.querySelectorAll('.tab');
  for (let i = 0; i < tabs.length; i++) {
    (function (tab) {
      tab.addEventListener('click', function () {
        const allTabs = document.querySelectorAll('.tab');
        for (let j = 0; j < allTabs.length; j++) { allTabs[j].classList.remove('active'); }
        const allPanes = document.querySelectorAll('.tabpane');
        for (let j = 0; j < allPanes.length; j++) { allPanes[j].classList.remove('active'); }
        tab.classList.add('active');
        const name = tab.getAttribute('data-tab');
        const pane = document.getElementById('tab-' + name);
        if (pane) { pane.classList.add('active'); }
        render(name);
      });
    })(tabs[i]);
  }

  async function render(name) {
    try {
      if (name === 'overview') { await renderOverview(); }
      else if (name === 'queue') { await renderQueue(); }
      else if (name === 'outputs') { await renderOutputs(); }
      else if (name === 'templates') { await renderTemplates(); }
      else if (name === 'autonomy') { await renderAutonomy(); }
      else if (name === 'evaluate') { await renderEvaluate(); }
      else if (name === 'logs') { await renderLogs(); }
      else if (name === 'governance') { await renderGovernance(); }
      else if (name === 'workspace') { await renderWorkspace(); }
      else if (name === 'settings') { await renderSettings(); }
    } catch (e) { toast('加载失败：' + e.message); }
  }

  async function refreshTop() {
    try {
      const st = await api('/api/status');
      const s = st.stats || {};
      const phaseText = {running:'运行中',paused:'已暂停',stopped:'已停止',idle:'待启动'}[st.phase] || st.phase;
      const el = document.getElementById('topStat');
      // 限流自证（2026-10-19）：上游 429 时工作台"运行中但 0 产出"，
      // 顶栏必须直接说清原因与剩余时间，否则用户会以为它坏了。
      const rl = st.rateLimit || {};
      const lh = st.laneHealth || {};
      let top = '状态：' + phaseText + ' ｜ 队列 ' + (s.queuedTasks || 0) + ' ｜ 运行 ' + (s.runningTasks || 0) + ' ｜ 今日产出 ' + (s.todayDone || 0);
      let color = '';
      let tip = '';
      // 优先级：结构性阻塞（IP 封禁/地区限制）> 临时限流 > 正常。
      // 两者对用户的意义完全不同：前者等多久都没用，必须换网络；
      // 后者等冷却就好，且已由限速闸门防住。
      if (lh.blocked) {
        // 顶栏要简洁：只放一句话结论 + 关键读数；完整原因与上游原文放 title。
        // （第一版把 lh.reason 整段拼进顶栏，而 reason 本身已含 IP，导致
        // 「封禁本机（IP x）」与 reason 里的「封禁本机（IP x）」重复。）
        const label = lh.blockKind === 'region_block' ? '上游地区限制' : '上游封禁本机';
        top = '⛔ ' + label + '（IP ' + (lh.egressIp || '?') + '）' +
          ' ｜ 队列 ' + (s.queuedTasks || 0) + ' ｜ 今日产出 ' + (s.todayDone || 0) +
          ' ｜ 重试无效，需等解封或换网络出口';
        color = 'var(--danger)';
        tip = (lh.reason || '') + '\n\n上游原文：' + (lh.evidence || '（无）');
      } else if (rl.limited) {
        top = '⚠ 上游限流冷却中（剩余 ' + (rl.remainingSeconds || 0) + ' 秒，连续第 ' + (rl.consecutive || 0) + ' 次）' +
          ' ｜ 队列 ' + (s.queuedTasks || 0) + ' ｜ 今日产出 ' + (s.todayDone || 0);
        color = 'var(--warn)';
        tip = rl.lastReason || '';
      }
      if (el) {
        el.textContent = top;
        el.style.color = color;
        el.title = tip;
      }
    } catch (e) { /* ignore */ }
  }

  const btnPause = document.getElementById('btnPause');
  const btnResume = document.getElementById('btnResume');
  const btnStop = document.getElementById('btnStop');
  if (btnPause) { btnPause.onclick = WB.pause; }
  if (btnResume) { btnResume.onclick = WB.resume; }
  if (btnStop) { btnStop.onclick = function () { if (confirm('确认停止引擎？在跑任务会被中断。')) { WB.stopEngine(); } }; }

  try {
    const es = new EventSource('/events?token=' + encodeURIComponent(TOKEN));
    es.onmessage = function () {
      refreshTop();
      const active = document.querySelector('.tab.active');
      const name = active && active.getAttribute('data-tab');
      if (name === 'overview' || name === 'queue') { render(name); }
    };
  } catch (e) { /* ignore */ }

  refreshTop();
  setInterval(refreshTop, 5000);
  render('overview');
})();
`
