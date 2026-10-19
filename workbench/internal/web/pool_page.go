package web

// poolPageHTML 是模型池独立控制台的页面外壳（不依赖主面板 app.js）。
//
// 设计：单页 + 左列表右详情，不弹窗、不抽屉，所有交互就地展开（沿用项目既有偏好）。
// 五块内容对应目标的五条要求：
//  1. 概览：可用/总数/车道水位/调度策略/网关连通；
//  2. 模型清单：优先级、状态、延迟、成功失败、启停、编辑、删除、上移下移；
//  3. 添加模型：兼容接口（OpenAI chat/responses、Anthropic messages）+ BaseURL/Key；
//  4. 状态日志：探测/切换/恢复逐条流水；
//  5. 操作区：立即同步网关、立即探测、恢复全部冷却。
const modelPoolPage = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>个人 AI 工作台 · 模型池</title>
<link rel="stylesheet" href="/assets/app.css">
<style>
  .pool-grid { display: grid; grid-template-columns: 1fr; gap: 16px; }
  .pool-inline { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 10px; align-items: end; }
  .pool-inline label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--muted, #7b8794); }
  .pool-inline input, .pool-inline select { padding: 7px 9px; border-radius: 6px; border: 1px solid var(--line, #30363d); background: var(--bg2, #161b22); color: inherit; font-size: 13px; }
  .pool-row-actions { display: flex; gap: 6px; flex-wrap: wrap; }
  .pool-row-actions button { padding: 3px 8px; font-size: 12px; border-radius: 5px; }
  .pill { display: inline-block; padding: 1px 8px; border-radius: 999px; font-size: 12px; border: 1px solid transparent; }
  .pill.healthy { background: rgba(63,185,80,.15); color: #3fb950; border-color: rgba(63,185,80,.4); }
  .pill.degraded { background: rgba(210,153,34,.15); color: #d29922; border-color: rgba(210,153,34,.4); }
  .pill.down { background: rgba(248,81,73,.15); color: #f85149; border-color: rgba(248,81,73,.4); }
  .pill.unknown { background: rgba(139,148,158,.15); color: #8b949e; border-color: rgba(139,148,158,.4); }
  .pill.disabled { background: rgba(110,118,129,.15); color: #6e7681; border-color: rgba(110,118,129,.4); }
  .logbox { max-height: 380px; overflow: auto; font-family: ui-monospace, Consolas, monospace; font-size: 12px; line-height: 1.6; white-space: pre-wrap; background: var(--bg2, #0d1117); border-radius: 6px; padding: 10px; }
  .kv { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 10px; }
  .kv .item { background: var(--bg2, #161b22); border-radius: 8px; padding: 10px 12px; }
  .kv .item .v { font-size: 20px; font-weight: 600; }
  .kv .item .l { font-size: 12px; color: var(--muted, #7b8794); margin-top: 2px; }
  table.pool-table td, table.pool-table th { vertical-align: middle; }
  .muted { color: var(--muted, #7b8794); font-size: 12px; }
  /* 窄屏可读性：9 列模型表在手机上将挤成一团，所以小屏统一改为卡片式（label:value），
     而不是靠横向滚动——横向滚动在触屏上会让「操作」列永远够不到。 */
  @media (max-width:720px) {
    table.pool-table thead { display: none; }
    table.pool-table, table.pool-table tbody, table.pool-table tr, table.pool-table td { display: block; width: 100%; }
    table.pool-table tr { border: 1px solid var(--line, #30363d); border-radius: 10px; padding: 8px 10px; margin-bottom: 10px; }
    table.pool-table td { border: 0; padding: 3px 0; display: flex; gap: 8px; align-items: center; }
    table.pool-table td::before { content: attr(data-label); color: var(--dim, #8b97a6); font-size: 12px; min-width: 72px; flex: 0 0 auto; }
    table.pool-table td.pool-ops::before { content: ''; min-width: 0; }
    .pool-row-actions { width: 100%; }
    .pool-inline { grid-template-columns: 1fr; }
  }
  /* 中等屏：表格容器允许横滚而不是撑破整页布局 */
  .pool-table-wrap { overflow-x: auto; }
</style>
</head>
<body class="admin">
<header class="topbar">
  <div class="brand">模型池 · 多接口统一纳管</div>
  <div class="topstat" id="poolStat">加载中…</div>
  <div class="topactions">
    <button id="btnSync" class="btn ghost">同步网关清单</button>
    <button id="btnProbe" class="btn ghost">立即探测</button>
    <button id="btnRefresh" class="btn ghost">刷新</button>
    <a class="btn ghost" href="/admin?token=%s">管理面板</a>
    <a class="btn ghost" href="/?token=%s">回首页</a>
  </div>
</header>
<main class="content">
  <section id="poolOverview" class="pool-grid"></section>
  <section id="poolLanes"></section>
  <section id="poolList"></section>
  <section id="poolAdd"></section>
  <section id="poolLog"></section>
</main>
<div id="toast" class="toast"></div>
%s
<script src="/assets/pool.js"></script>
</body>
</html>
`
