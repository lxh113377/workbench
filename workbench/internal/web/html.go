package web

const indexPage = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="dark">
<meta name="theme-color" content="#0b0d10">
<title>个人 AI 工作台</title>
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
<a class="home-admin" href="/admin?token=%s" title="管理面板" aria-hidden="true" tabindex="-1">·</a>
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
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>个人 AI 工作台 · 管理面板</title>
<link rel="stylesheet" href="/assets/app.css">
</head>
<body class="admin">
<header class="topbar">
  <div class="brand">个人 AI 工作台</div>
  <div class="topstat" id="topStat">加载中…</div>
  <div class="topactions">
    <button id="btnPause" class="btn ghost">暂停</button>
    <button id="btnResume" class="btn ghost">继续</button>
    <button id="btnStop" class="btn danger">停止</button>
    <a class="btn ghost" href="/modelpool?token=%s">模型池</a>
    <a class="btn ghost" href="/?token=%s">回首页</a>
  </div>
</header>
<nav class="tabs">
  <button class="tab active" data-tab="overview">概览</button>
  <button class="tab" data-tab="queue">队列</button>
  <button class="tab" data-tab="outputs">产出</button>
  <button class="tab" data-tab="templates">模板</button>
  <button class="tab" data-tab="autonomy">自主</button>
  <button class="tab" data-tab="evaluate">评估</button>
  <button class="tab" data-tab="logs">日志</button>
  <button class="tab" data-tab="governance">治理</button>
  <button class="tab" data-tab="workspace">工作目录</button>
  <button class="tab" data-tab="settings">设置</button>
</nav>
<main class="content">
  <section id="tab-overview" class="tabpane active"></section>
  <section id="tab-queue" class="tabpane"></section>
  <section id="tab-outputs" class="tabpane"></section>
  <section id="tab-templates" class="tabpane"></section>
  <section id="tab-autonomy" class="tabpane"></section>
  <section id="tab-evaluate" class="tabpane"></section>
  <section id="tab-logs" class="tabpane"></section>
  <section id="tab-governance" class="tabpane"></section>
  <section id="tab-workspace" class="tabpane"></section>
  <section id="tab-settings" class="tabpane"></section>
</main>
<div id="toast" class="toast"></div>
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
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>个人 AI 工作台 · 治理控制台</title>
<link rel="stylesheet" href="/assets/app.css">
</head>
<body class="admin">
<header class="topbar">
  <div class="brand">治理控制台 · 契约 × 体量</div>
  <div class="topstat" id="govStat">加载中…</div>
  <div class="topactions">
    <button id="govRefresh" class="btn ghost">刷新</button>
    <button id="govSample" class="btn ghost">立即采样</button>
    <a class="btn ghost" href="/admin?token=%s">管理面板</a>
    <a class="btn ghost" href="/?token=%s">回首页</a>
  </div>
</header>
<main class="content" id="govRoot"></main>
<div id="toast" class="toast"></div>
%s
<script src="/assets/gov.js"></script>
</body>
</html>
`
