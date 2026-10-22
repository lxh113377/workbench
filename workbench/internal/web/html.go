package web

const indexPage = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="dark">
<meta name="theme-color" content="#04070d">
<title>个人 AI 工作台</title>
<link rel="icon" type="image/svg+xml" href="/assets/favicon.svg">
<link rel="stylesheet" href="/assets/app.css">
</head>
<body class="home">
<main class="home-main">
<button id="startBtn" class="start-btn" type="button" data-state="%s" aria-busy="false" aria-describedby="homeStatus" title="%s">
<span class="btn-load btn-spinner" aria-hidden="true"></span>
<span class="btn-label">%s</span>
<span class="btn-load btn-arc" aria-hidden="true"></span>
</button>
<span id="homeStatus" class="sr-only" role="status" aria-live="polite" aria-atomic="true">%s</span>
<nav class="home-entries" aria-label="子页面入口">
<a class="home-entry" href="/admin?token=%s">管理面板</a>
<a class="home-entry" href="/modelpool?token=%s">模型池</a>
<a class="home-entry" href="/governance?token=%s">治理</a>
</nav>
</main>
%s
<script src="/assets/app.js"></script>
</body>
</html>
`

const adminPage = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="dark">
<meta name="theme-color" content="#04070d">
<title>个人 AI 工作台 · 管理面板</title>
<link rel="icon" type="image/svg+xml" href="/assets/favicon.svg">
<link rel="stylesheet" href="/assets/app.css">
</head>
<body class="admin">
<div class="topbar-shell">
<header class="topbar">
  <a class="brand" href="/" title="回首页">个人 AI 工作台</a>
  <div class="topstat" id="topStat">加载中…</div>
  <div class="topactions">
    <button id="btnPause" class="btn ghost">暂停</button>
    <button id="btnResume" class="btn ghost">继续</button>
    <button id="btnStop" class="btn danger">停止</button>
    <button id="btnMore" class="btn ghost" type="button" aria-expanded="false" aria-controls="topMore">更多</button>
  </div>
  <div class="topmore" id="topMore" hidden>
    <a href="/modelpool?token=%s">模型池</a>
    <a href="/governance?token=%s">治理控制台</a>
    <a href="/?token=%s">回首页</a>
  </div>
</header>
<div class="pagebar"><span class="pagecrumb">控制台</span><span class="pagetitle">管理面板</span></div>
</div>
<nav class="tabs" aria-label="管理面板分区">
  <button class="tab active" data-tab="overview">总览</button>
  <button class="tab" data-tab="queue">任务</button>
  <button class="tab" data-tab="outputs">产出</button>
  <button class="tab" data-tab="templates">模板</button>
  <button class="tab" data-tab="settings">设置</button>
</nav>
<main class="content">
  <section id="tab-overview" class="tabpane active"></section>
  <section id="tab-queue" class="tabpane"></section>
  <section id="tab-outputs" class="tabpane"></section>
  <section id="tab-templates" class="tabpane"></section>
  <section id="tab-settings" class="tabpane"></section>
</main>
<div id="toast" class="toast" role="status" aria-live="polite"></div>
%s
<script src="/assets/app.js"></script>
</body>
</html>
`

// governancePage 是独立的治理控制台（不依赖 app.js 的页签逻辑，单页可直读）。
const governancePage = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="dark">
<meta name="theme-color" content="#04070d">
<title>个人 AI 工作台 · 治理控制台</title>
<link rel="icon" type="image/svg+xml" href="/assets/favicon.svg">
<link rel="stylesheet" href="/assets/app.css">
</head>
<body class="admin">
<div class="topbar-shell">
<header class="topbar">
  <div class="brand">个人 AI 工作台</div>
  <div class="topstat" id="govStat">加载中…</div>
  <div class="topactions">
    <button id="govRefresh" class="btn ghost">刷新</button>
    <button id="govSample" class="btn primary">立即采样</button>
    <button id="btnMore" class="btn ghost" type="button" aria-expanded="false" aria-controls="topMore">更多</button>
  </div>
  <div class="topmore" id="topMore" hidden>
    <a href="/admin?token=%s">管理面板</a>
    <a href="/modelpool?token=%s">模型池</a>
    <a href="/?token=%s">回首页</a>
  </div>
</header>
<div class="pagebar"><span class="pagecrumb">控制台</span><span class="pagetitle">治理控制台</span><span class="pagesub">契约 × 体量</span></div>
</div>
<main class="content" id="govRoot"></main>
<div id="toast" class="toast" role="status" aria-live="polite"></div>
%s
<script src="/assets/gov.js"></script>
</body>
</html>
`
