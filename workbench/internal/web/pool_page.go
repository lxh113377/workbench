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
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="dark">
<meta name="theme-color" content="#04070d">
<title>个人 AI 工作台 · 模型池</title>
<link rel="icon" type="image/svg+xml" href="/assets/favicon.svg">
<link rel="stylesheet" href="/assets/app.css">
<style>
  /* 模型池页内样式：只放「池专属」的排版与窄屏重排；通用件（card/kpi/btn/pill/table）走全局 app.css。 */
  .pool-grid { display: grid; grid-template-columns: 1fr; gap: 16px; }
  .pool-inline { display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 12px; align-items: end; }
  .pool-inline label { display: flex; flex-direction: column; gap: 5px; margin: 0; }
  .pool-inline input, .pool-inline select { padding: 9px 10px; }
  /* 分组卡片（2026-10-20 重做模型清单）：每组一个 card，头部是聚合健康度。
     参考 Our Free Model 设置页的渠道卡片：pill 定色 + 一排 key:value 读数，
     让「哪条渠道在掉线」一眼可见，而不是从 37 行明细里逐行数。 */
  .pool-group { margin-bottom: 14px; }
  .pool-group:last-child { margin-bottom: 0; }
  .pool-group-head {
    display: flex; align-items: center; gap: 10px; flex-wrap: wrap;
    padding-bottom: 10px; margin-bottom: 2px;
    border-bottom: 1px solid var(--line-2);
  }
  .pool-group-head .pill { font-size: 12px; padding: 4px 10px; }
  .pool-group-stat { font-size: 12px; color: var(--fg-2); white-space: nowrap; }
  .pool-group-stat b { color: var(--fg); font-weight: 600; }
  .pool-group-stat.warn { color: var(--warn); }
  .pool-group-stat.warn b { color: var(--warn); }
  .pool-group-stat.dim { color: var(--dim); }
  .pool-group .fold { border-top: 0; margin-top: 6px; }
  .pool-group .fold > summary { padding: 8px 2px; font-size: 12px; }
  @media (max-width:720px) {
    .pool-group-head { gap: 6px 10px; }
    .pool-group-actions { margin-left: 0; width: 100%; }
    .pool-group-stat { font-size: 11.5px; }
  }
  .pool-row-actions { display: flex; gap: 6px; flex-wrap: wrap; justify-content: flex-end; }
  .pool-row-actions button {
    /* 量化审计：原 24px 高，鼠标能点、触屏与高频操作偏小；提到 32（桌面） / 44（触屏档）。 */
    min-height: 32px; min-width: 32px;
    padding: 4px 10px; font-size: 12px; border-radius: 7px;
    border: 1px solid var(--line-2); background: rgba(255,255,255,.04); color: var(--fg-2);
    display: inline-flex; align-items: center; justify-content: center;
    transition: background .14s ease, border-color .14s ease, color .14s ease;
  }
  .pool-row-actions button:hover { background: rgba(var(--accent-rgb),.13); border-color: rgba(var(--accent-rgb),.45); color: var(--fg); }
  /* 分组卡的一键启用/一键停用（老大 2026-10-22 令）：靠右排布，与组内读数同行。 */
  .pool-group-actions { margin-left: auto; display: flex; gap: 6px; flex-wrap: wrap; }
  .pool-group-actions button {
    min-height: 30px; padding: 4px 10px; font-size: 12px; border-radius: 7px;
    border: 1px solid var(--line-2); background: rgba(255,255,255,.04); color: var(--fg-2);
    display: inline-flex; align-items: center; justify-content: center;
    transition: background .14s ease, border-color .14s ease, color .14s ease;
  }
  .pool-group-actions button:hover { background: rgba(var(--accent-rgb),.13); border-color: rgba(var(--accent-rgb),.45); color: var(--fg); }
  .pool-group-actions button:disabled { opacity: .4; cursor: not-allowed; background: rgba(255,255,255,.02); }
  .pool-group-actions button[data-gact="gdisable"] { color: var(--warn); border-color: rgba(255,170,60,.35); }
  /* 两段式确认「待确认」态的池内分支（老大 2026-10-22：armConfirm 取代原生 confirm）。
     全局 .btn.armed 只覆盖带 .btn 的按钮；池内行操作与一键开关都是裸 <button>，
     按契约（TestPoolRowActionButtonsMeetTouchBaseline）池专项规则必须写在页内 <style>，
     故此处补齐同款红态；armPulse 关键帧沿用全局定义。 */
  .pool-row-actions button.armed, .pool-group-actions button.armed {
    color:#fff;border-color:rgba(var(--danger-rgb),.72);
    background:linear-gradient(180deg,rgba(var(--danger-rgb),.34),rgba(var(--danger-rgb),.18));
    box-shadow:0 0 0 1px rgba(var(--danger-rgb),.35),0 0 22px -8px rgba(var(--danger-rgb),.6);
    animation:armPulse 1.1s ease-in-out infinite alternate;
  }
  .logbox { max-height: 380px; }
  .pool-table-wrap { overflow-x: auto; }
  table.pool-table td, table.pool-table th { vertical-align: middle; }
  table.pool-table td:first-child { color: var(--fg); font-weight: 500; }
  /* 窄屏可读性：9 列模型表在手机上将挤成一团，所以小屏统一改为卡片式（label:value），
     而不是靠横向滚动——横向滚动在触屏上会让「操作」列永远够不到。 */
  @media (max-width:720px) {
    table.pool-table thead { display: none; }
    table.pool-table, table.pool-table tbody, table.pool-table tr, table.pool-table td { display: block; width: 100%; }
    table.pool-table tr {
      border: 1px solid var(--line-2); border-radius: 12px;
      padding: 12px 14px; margin-bottom: 14px; background: rgba(255,255,255,.02);
      box-shadow: 0 10px 26px -22px rgba(0,0,0,.9);
    }
    table.pool-table tr:last-child { margin-bottom: 0; }
    table.pool-table td { border: 0; padding: 4px 0; display: flex; gap: 10px; align-items: center; }
    table.pool-table td::before { content: attr(data-label); color: var(--dim); font-family: var(--font-mono); font-size: 10.5px; letter-spacing: .1em; text-transform: uppercase; min-width: 72px; flex: 0 0 auto; }
    table.pool-table td.pool-ops::before { content: ''; min-width: 0; }
    .pool-row-actions { width: 100%; justify-content: flex-start; }
    .pool-inline { grid-template-columns: 1fr; }
  }
  /* 触控/窄屏可点性：行内操作按钮（↑↓/停用/恢复/编辑/删除）是裸 <button>，
     .btn{min-height:44px} 覆盖不到它，触屏上约 20px 高基本点不中。 */
  @media (pointer:coarse), (max-width:720px){ .pool-row-actions button{min-height:44px;padding:6px 12px} }
  /* 一键开关同样要够触屏基线；单独一条，避免动到被契约测试钉住的上一行。 */
  @media (pointer:coarse), (max-width:720px){ .pool-group-actions button{min-height:44px;padding:6px 12px} }
</style>
</head>
<body class="admin">
<div class="topbar-shell">
<header class="topbar">
  <div class="brand">个人 AI 工作台</div>
  <div class="topstat" id="poolStat">加载中…</div>
  <div class="topactions">
    <button id="btnSync" class="btn ghost">同步网关清单</button>
    <button id="btnProbe" class="btn ghost">立即探测</button>
    <button id="btnRefresh" class="btn ghost">刷新</button>
    <button id="btnMore" class="btn ghost" type="button" aria-expanded="false" aria-controls="topMore">更多</button>
  </div>
  <div class="topmore" id="topMore" hidden>
    <a href="/admin?token=%s">管理面板</a>
    <a href="/?token=%s">回首页</a>
  </div>
</header>
<div class="pagebar"><span class="pagecrumb">控制台</span><h1 class="pagetitle">模型池</h1><span class="pagesub">多接口统一纳管</span></div>
</div>
<main class="content">
  <section id="poolOverview"></section>
  <section id="poolList"></section>
  <section id="poolAdd"></section>
  <section id="poolLog"></section>
</main>
<div id="toast" class="toast" role="status" aria-live="polite"></div>
%s
<script src="/assets/pool.js"></script>
</body>
</html>
`
