(() => {
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

  // ── 首页 ──────────────────────────────────────────────
  if (!isAdmin) {
    const btn = document.getElementById('startBtn');
    const status = document.getElementById('status');
    const badge = document.getElementById('badge');
    const PHASE_TEXT = {running:'运行中',paused:'已暂停',stopped:'已停止',idle:'待启动'};
    const setPhase = (phase) => {
      status.textContent = '引擎状态：' + (PHASE_TEXT[phase] || phase);
      if (badge) badge.textContent = 'PERSONAL AI WORKBENCH';
      if (phase === 'running' || phase === 'paused') {
        btn.disabled = true;
        btn.textContent = phase === 'running' ? '运行中…' : '已暂停';
      } else {
        btn.disabled = false;
        btn.textContent = '开始运行';
      }
    };
    const ping = async () => {
      try { const st = await api('/api/status'); setPhase(st.phase); } catch (e) { /* 未启动时静默 */ }
    };
    if (btn) btn.addEventListener('click', async () => {
      btn.disabled = true; btn.textContent = '启动中…';
      try {
        const r = await api('/api/engine/start', { method: 'POST', body: '{}' });
        setPhase(r.phase);
        toast('引擎已启动，正在自动产出');
      } catch (e) {
        toast('启动失败：' + e.message);
        btn.disabled = false; btn.textContent = '开始运行';
      }
    });
    ping();
    setInterval(ping, 4000);
    return;
  }

  // ── 管理面板 ──────────────────────────────────────────
  const panes = {
    overview: document.getElementById('tab-overview'),
    queue: document.getElementById('tab-queue'),
    outputs: document.getElementById('tab-outputs'),
    templates: document.getElementById('tab-templates'),
    evaluate: document.getElementById('tab-evaluate'),
    logs: document.getElementById('tab-logs'),
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
    panes.overview.innerHTML =
      '<div class="grid c4">' +
        kpi('引擎状态', phaseText) + kpi('今日产出', s.todayDone || 0) + kpi('队列中', s.queuedTasks || 0) + kpi('累计完成', s.doneTasks || 0) +
        kpi('成功率', (s.successRate || 0).toFixed(1) + '%') + kpi('平均耗时', (s.avgSeconds || 0).toFixed(0) + ' 秒') +
        kpi('产出总量', fmtBytes(s.totalBytes || 0)) + kpi('被拒任务', s.rejectedTasks || 0) +
      '</div>' +
      '<div class="card" style="margin-top:16px"><h3>模型健康度</h3><table><thead><tr><th>模型</th><th>层级</th><th>成功/失败</th><th>健康分</th><th>状态</th></tr></thead><tbody>' + (models || '<tr><td colspan="5" class="empty">无模型</td></tr>') + '</tbody></table></div>' +
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

  async function renderOutputs() {
    const res = await api('/api/tasks?limit=3000');
    cache.tasks = res.tasks || [];
    const done = cache.tasks.filter(t => t.state === 'done');
    const rows = done.map(t => {
      const q = t.critique ? t.critique.quality + '/10' : '—';
      return '<tr><td>' + esc(t.title) + '</td><td>' + esc(t.domain || '-') + '</td><td>' + fmtMin(t.elapsedMs) + '</td><td>' + q + '</td><td>' + fmtBytes(t.outputBytes) + '</td>' +
        '<td class="row" style="gap:4px"><button class="btn sm" onclick="WB.openTask(&apos;' + t.id + '&apos;)">查看</button>' +
        '<button class="btn sm" onclick="WB.rate(&apos;' + t.id + '&apos;,5)">★5</button></td></tr>';
    }).join('');
    panes.outputs.innerHTML =
      '<div class="card"><h3>已完成产出（' + done.length + '）</h3>' +
      '<div class="row" style="margin-bottom:12px">' +
      '<button class="btn primary" onclick="WB.exportAll(&apos;md&apos;)">导出全部 Markdown</button>' +
      '<button class="btn" onclick="WB.exportAll(&apos;txt&apos;)">导出全部 TXT</button>' +
      '<button class="btn" onclick="WB.exportAll(&apos;docx&apos;)">导出全部 Word</button></div>' +
      '<table><thead><tr><th>标题</th><th>领域</th><th>耗时</th><th>自评</th><th>大小</th><th>操作</th></tr></thead><tbody>' +
      (rows || '<tr><td colspan="6" class="empty">还没有产出，点击首页「开始运行」即可</td></tr>') + '</tbody></table></div>' +
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
    async resetTemplates() {
      if (!confirm('恢复为内置默认模板？自定义模板会被清除。')) { return; }
      try { await api('/api/templates/reset', { method: 'POST', body: '{}' }); toast('已恢复默认'); renderTemplates(); } catch (e) { toast(e.message); }
    },
  };
  window.WB = WB;

  // Markdown 极简渲染
  function mdToHtml(md) {
    const lines = String(md || '').split('\n');
    let html = '', inList = false, inCode = false;
    for (let i = 0; i < lines.length; i++) {
      const raw = lines[i];
      if (raw.trim().indexOf('```') === 0) { inCode = !inCode; html += inCode ? '<pre>' : '</pre>'; continue; }
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
      else if (name === 'evaluate') { await renderEvaluate(); }
      else if (name === 'logs') { await renderLogs(); }
      else if (name === 'settings') { await renderSettings(); }
    } catch (e) { toast('加载失败：' + e.message); }
  }

  async function refreshTop() {
    try {
      const st = await api('/api/status');
      const s = st.stats || {};
      const phaseText = {running:'运行中',paused:'已暂停',stopped:'已停止',idle:'待启动'}[st.phase] || st.phase;
      const el = document.getElementById('topStat');
      if (el) {
        el.textContent = '状态：' + phaseText + ' ｜ 队列 ' + (s.queuedTasks || 0) + ' ｜ 运行 ' + (s.runningTasks || 0) + ' ｜ 今日产出 ' + (s.todayDone || 0);
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
