package web

// poolJS 是模型池控制台的前端逻辑（原生 JS，无框架、无构建）。
const poolJS = `(() => {
  const TOKEN = window.__WB_TOKEN__ || '';
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
  const esc = (s) => String(s == null ? '' : s).replace(/[&<>"']/g, c => ESC[c] || c);
  const toast = (msg) => { const el = document.getElementById('toast'); if (!el) { console.log(msg); return; } el.textContent = msg; el.classList.add('show'); clearTimeout(el._t); el._t = setTimeout(() => el.classList.remove('show'), 3000); };
  const fmtMs = (ms) => { if (!ms) return '—'; if (ms < 1000) return ms + ' ms'; return (ms/1000).toFixed(1) + ' s'; };
  const fmtTime = (ms) => ms ? new Date(ms).toLocaleString('zh-CN', {hour12:false}) : '—';
  const STATE_TEXT = { healthy:'可用', degraded:'降级', down:'下线', unknown:'未探测', disabled:'已停用' };
  let cache = null;

  const pill = (st) => '<span class="pill ' + esc(st||'unknown') + '">' + esc(STATE_TEXT[st] || st || '未知') + '</span>';
  const availBadge = (m) => {
    if (!m.enabled) return '<span class="pill disabled">已停用</span>';
    if (m.routable === false) return '<span class="pill unknown">仅网关</span>';
    return pill(m.state);
  };

  // ── 概览 ──────────────────────────────────────────────
  const renderOverview = (d) => {
    const g = d.gateway || {};
    const items = [
      ['可用模型', (d.available||0) + ' / ' + (d.total||0)],
      ['可调度（DSH 车道）', String(d.routable||0)],
      ['调度策略', d.strategy || 'priority'],
      ['探测轮次', String(d.probeRuns||0)],
      ['网关', g.base ? (g.hasKey ? '已连接' : '缺密钥') : '未发现'],
      ['网关广播模型', String(g.models||0)],
    ];
    const kv = items.map(x => '<div class="item"><div class="v">' + esc(x[1]) + '</div><div class="l">' + esc(x[0]) + '</div></div>').join('');
    const strategies = ['priority','round-robin','weighted','least-loaded'];
    const stratSel = '<select id="strategySel">' + strategies.map(s => '<option value="' + s + '"' + (d.strategy===s?' selected':'') + '>' + s + '</option>').join('') + '</select>';
    const lp = d.lastProbe || {};
    document.getElementById('poolOverview').innerHTML =
      '<div class="card"><h3>总览</h3><div class="kv">' + kv + '</div>' +
      '<div style="margin-top:12px" class="pool-inline"><label>调度策略' + stratSel + '</label>' +
      '<div class="muted">最近一轮探测：共 ' + esc(lp.total||0) + ' 个，可用 ' + esc(lp.ok||0) + '，下线 ' + esc(lp.down||0) + '，未知 ' + esc(lp.unknown||0) + '（' + esc(fmtTime(lp.at)) + '）</div></div>' +
      (g.error ? '<div class="muted" style="margin-top:8px;color:#f85149">网关错误：' + esc(g.error) + '</div>' : '') +
      '</div>';
    const sel = document.getElementById('strategySel');
    if (sel) sel.onchange = async () => {
      try { await api('/api/pool/strategy', { method:'POST', body: JSON.stringify({ strategy: sel.value }) }); toast('策略已切换为 ' + sel.value); load(); }
      catch (e) { toast('切换失败：' + e.message); }
    };
  };

  // ── 车道 ──────────────────────────────────────────────
  const renderLanes = (d) => {
    const rows = (d.lanes || []).map(l =>
      '<tr>' +
      '<td data-label="车道">' + esc(l.label || l.lane) + '</td>' +
      '<td data-label="总数">' + esc(l.total) + '</td>' +
      '<td data-label="可用">' + esc(l.available) + '</td>' +
      '<td data-label="下线">' + esc(l.down) + '</td>' +
      '<td data-label="停用">' + esc(l.disabled) + '</td>' +
      '</tr>'
    ).join('');
    document.getElementById('poolLanes').innerHTML =
      '<div class="card" style="margin-top:16px"><h3>车道水位</h3><div class="pool-table-wrap"><table class="pool-table"><thead><tr><th>车道</th><th>总数</th><th>可用</th><th>下线</th><th>停用</th></tr></thead><tbody>' +
      (rows || '<tr><td colspan="5" class="empty">暂无模型</td></tr>') + '</tbody></table></div></div>';
  };

  // ── 模型清单 ──────────────────────────────────────────
  const renderList = (d) => {
    const specs = d.specs || [];
    const health = {};
    (d.health || []).forEach(h => { health[h.id] = h; });
    const rows = specs.map((m, i) => {
      const h = health[m.id] || {};
      return '<tr data-id="' + esc(m.id) + '">' +
        '<td data-label="#">' + (i+1) + '</td>' +
        '<td data-label="模型"><div>' + esc(m.label || m.id) + '</div><div class="muted">' + esc(m.id) + '</div></td>' +
        '<td data-label="分组">' + esc(m.group || m.lane) + '</td>' +
        '<td data-label="优先级/权重">' + esc(m.priority) + ' / ' + esc(m.weight) + '</td>' +
        '<td data-label="状态">' + availBadge(m) + '</td>' +
        '<td data-label="延迟">' + fmtMs(h.latencyMs || m.latencyMs) + '</td>' +
        '<td data-label="成功/失败">' + esc(h.success||0) + ' / ' + esc(h.failure||0) + '</td>' +
        '<td data-label="最近错误/备注" class="muted">' + esc(h.lastErr || m.note || '') + '</td>' +
        '<td data-label="操作" class="pool-ops"><div class="pool-row-actions">' +
          '<button data-act="up" data-id="' + esc(m.id) + '">↑</button>' +
          '<button data-act="down" data-id="' + esc(m.id) + '">↓</button>' +
          '<button data-act="' + (m.enabled?'disable':'enable') + '" data-id="' + esc(m.id) + '">' + (m.enabled?'停用':'启用') + '</button>' +
          '<button data-act="restore" data-id="' + esc(m.id) + '">恢复</button>' +
          '<button data-act="edit" data-id="' + esc(m.id) + '">编辑</button>' +
          '<button data-act="del" data-id="' + esc(m.id) + '">删除</button>' +
        '</div></td></tr>';
    }).join('');
    document.getElementById('poolList').innerHTML =
      '<div class="card" style="margin-top:16px"><h3>模型清单（按调度优先级排序，越靠上越先用）</h3>' +
      '<div class="pool-table-wrap"><table class="pool-table"><thead><tr><th>#</th><th>模型</th><th>分组</th><th>优先级/权重</th><th>状态</th><th>延迟</th><th>成功/失败</th><th>最近错误/备注</th><th>操作</th></tr></thead><tbody>' +
      (rows || '<tr><td colspan="9" class="empty">暂无模型，点上方「同步网关清单」或下方添加</td></tr>') + '</tbody></table></div></div>';
    document.getElementById('poolList').querySelectorAll('button[data-act]').forEach(btn => {
      btn.onclick = () => onRowAction(btn.getAttribute('data-act'), btn.getAttribute('data-id'), d);
    });
  };

  const onRowAction = async (act, id, d) => {
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
        if (!confirm('删除模型 ' + id + '？')) return;
        await api('/api/pool/models/' + encodeURIComponent(id), { method:'DELETE' });
        toast('已删除 ' + id);
      } else if (act === 'edit') {
        openEditor(d.specs.find(x => x.id === id));
        return;
      } else {
        await api('/api/pool/models/' + encodeURIComponent(id) + '/' + act, { method:'POST', body: '{}' });
        toast('已' + ({enable:'启用',disable:'停用',restore:'恢复'} [act] || act) + '：' + id);
      }
      load();
    } catch (e) { toast('操作失败：' + e.message); }
  };

  // ── 添加/编辑（就地展开，不弹窗） ──────────────────────
  let editing = null;
  const openEditor = (m) => { editing = m || null; renderAdd(); document.getElementById('poolAdd').scrollIntoView({behavior:'smooth'}); };
  const renderAdd = () => {
    const m = editing || { format:'openai-chat', tier:'balanced', lane:'custom', priority:100, weight:50, maxFail:3, cooldownSec:120, enabled:true, routable:true };
    const isEdit = !!editing;
    document.getElementById('poolAdd').innerHTML =
      '<div class="card" style="margin-top:16px"><h3>' + (isEdit ? '编辑模型 · ' + esc(m.id) : '添加兼容模型接口') + '</h3>' +
      '<div class="pool-inline">' +
        '<label>模型 ID（唯一）<input id="fId" value="' + esc(m.id||'') + '" ' + (isEdit?'disabled':'') + ' placeholder="如 my-endpoint/llama-3"></label>' +
        '<label>显示名<input id="fLabel" value="' + esc(m.label||'') + '" placeholder="可空"></label>' +
        '<label>上游模型名<input id="fModel" value="' + esc(m.model||'') + '" placeholder="如 gpt-4o-mini"></label>' +
        '<label>BaseURL<input id="fBase" value="' + esc(m.baseUrl||'') + '" placeholder="https://api.example.com/v1（空=本机 OFM 网关）"></label>' +
        '<label>API Key<input id="fKey" type="password" placeholder="' + (isEdit && m.hasKey ? '已配置 ' + esc(m.keyHint||'') + '，留空不修改' : '自定义端点必填') + '"></label>' +
        '<label>线协议<select id="fFormat">' +
          ['openai-chat','openai-responses','anthropic-messages'].map(f => '<option value="' + f + '"' + (m.format===f?' selected':'') + '>' + f + '</option>').join('') +
        '</select></label>' +
        '<label>车道<select id="fLane">' +
          [['custom','自定义'],['free','OFM 免费'],['kilo','Kilo'],['eac','EAC'],['aggregate','聚合']].map(x => '<option value="' + x[0] + '"' + (m.lane===x[0]?' selected':'') + '>' + x[1] + '</option>').join('') +
        '</select></label>' +
        '<label>Tier<select id="fTier">' +
          ['quality','balanced','fast'].map(t => '<option value="' + t + '"' + (m.tier===t?' selected':'') + '>' + t + '</option>').join('') +
        '</select></label>' +
        '<label>优先级（1=最高）<input id="fPri" type="number" min="1" value="' + esc(m.priority||100) + '"></label>' +
        '<label>权重<input id="fWeight" type="number" min="1" value="' + esc(m.weight||50) + '"></label>' +
        '<label>连续失败阈值<input id="fMaxFail" type="number" min="1" value="' + esc(m.maxFail||3) + '"></label>' +
        '<label>冷却秒数<input id="fCool" type="number" min="10" value="' + esc(m.cooldownSec||120) + '"></label>' +
      '</div>' +
      '<div class="pool-row-actions" style="margin-top:12px">' +
        '<button class="btn" id="btnSave">' + (isEdit ? '保存' : '添加') + '</button>' +
        '<button class="btn ghost" id="btnCancel">取消</button>' +
        '<span class="muted">兼容任意 OpenAI Chat Completions / Responses / Anthropic Messages 端点；密钥只写本机，不对外回显。</span>' +
      '</div></div>';
    document.getElementById('btnCancel').onclick = () => { editing = null; renderAdd(); };
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
        editing = null; load();
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
      '<div class="card" style="margin-top:16px"><h3>模型状态日志（最近 120 条，新→旧）</h3>' +
      '<div class="muted">探测、故障转移、恢复、增删改都会留一行，便于排查「刚才为什么换了模型」。</div>' +
      '<div class="logbox" style="margin-top:10px">' + body + '</div></div>';
  };

  // ── 加载与动作 ────────────────────────────────────────
  const load = async () => {
    try {
      const d = await api('/api/pool');
      cache = d;
      renderOverview(d); renderLanes(d); renderList(d); renderAdd(); renderLog(d);
      const g = d.gateway || {};
      document.getElementById('poolStat').textContent =
        '可用 ' + (d.available||0) + '/' + (d.total||0) + ' ｜ 策略 ' + (d.strategy||'-') + ' ｜ 网关 ' + (g.base ? '在线' : '未连接');
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

  load();
  setInterval(load, 15000);
})();
`;
