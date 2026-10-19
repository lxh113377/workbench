package web

const appCSS = `
/* ── 治理控制台 ── */
.gov-card{background:var(--panel);border:1px solid var(--line);border-radius:var(--radius);padding:14px 16px;margin:0 0 14px}
.gov-card h3{margin:0 0 10px;font-size:15px;color:var(--fg)}
.gov-card h4{margin:12px 0 8px;font-size:13px;color:var(--dim)}
.gov-banner{border-radius:var(--radius);padding:10px 14px;margin:0 0 14px;font-weight:600}
.gov-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(150px,1fr));gap:8px}
.gov-kv{background:var(--panel2);border:1px solid var(--line);border-radius:8px;padding:8px 10px;display:flex;flex-direction:column;gap:2px}
.gov-kv span{color:var(--dim);font-size:12px}
.gov-kv b{font-size:16px}
.gov-stages{display:flex;gap:8px;margin-top:10px;flex-wrap:wrap}
.gov-row{display:flex;gap:12px;align-items:baseline;flex-wrap:wrap;padding:7px 10px;border-radius:8px;background:var(--panel2);border-left:3px solid var(--line);margin:6px 0}
.gov-dim{color:var(--dim);font-size:12px}
.lv-ok{border-left-color:var(--accent2)}
.lv-warn{border-left-color:var(--warn)}
.lv-red{border-left-color:var(--danger)}
.lv-unk{border-left-color:var(--dim)}
.gov-banner.lv-ok{background:rgba(58,210,159,.12);color:var(--accent2)}
.gov-banner.lv-warn{background:rgba(255,176,32,.12);color:var(--warn)}
.gov-banner.lv-red{background:rgba(255,92,92,.12);color:var(--danger)}
.gov-banner.lv-unk{background:rgba(139,151,166,.12);color:var(--dim)}
.gov-actions{display:flex;gap:10px;align-items:center;margin:0 0 14px;flex-wrap:wrap}

:root{
  --bg:#0b0d10; --panel:#14181d; --panel2:#1b2027; --line:#262d36;
  --fg:#e8edf3; --dim:#8b97a6; --accent:#4f8cff; --accent2:#3ad29f;
  --warn:#ffb020; --danger:#ff5c5c; --radius:12px;
}
*{box-sizing:border-box}
html,body{margin:0;padding:0;height:100%}
body{
  background:var(--bg); color:var(--fg);
  font:15px/1.55 "Segoe UI","Microsoft YaHei",system-ui,-apple-system,sans-serif;
  -webkit-font-smoothing:antialiased;
}
button{font-family:inherit;font-size:inherit;cursor:pointer}
a{color:inherit;text-decoration:none}

/* ── 极简首页：一个自动按钮 + 零冗余 ─────────────────────── */
body.home{display:flex;align-items:center;justify-content:center;overflow:hidden}
body.home::before{
  content:'';position:fixed;inset:0;pointer-events:none;
  background:radial-gradient(120% 90% at 50% 32%,rgba(79,140,255,.16),rgba(11,13,16,0) 62%);
}
.home-main{
  display:flex;align-items:center;justify-content:center;
  width:100%;height:100%;position:relative;
}
.sr-only{
  position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;
  clip:rect(0 0 0 0);white-space:nowrap;border:0;
}
/* 唯一的交互元素：自动按钮。点击前必须自己完成加载，点击后自己给出状态反馈。 */
.start-btn{
  position:relative;width:min(260px,72vw);min-height:104px;border-radius:20px;
  border:1px solid var(--btn-line,#2e6fd4);
  background:linear-gradient(180deg,#5b95ff,#3b74e0);
  color:#fff;font-size:clamp(22px,5.4vw,30px);font-weight:600;letter-spacing:.08em;
  display:flex;align-items:center;justify-content:center;gap:10px;
  box-shadow:0 18px 48px rgba(59,116,224,.34),inset 0 1px 0 rgba(255,255,255,.24);
  transition:transform .16s ease,box-shadow .16s ease,filter .16s ease,background .2s ease,border-color .2s ease;
  touch-action:manipulation;-webkit-tap-highlight-color:transparent;
}
.start-btn:hover{transform:translateY(-2px);box-shadow:0 24px 60px rgba(59,116,224,.44)}
.start-btn:active{transform:translateY(0)}
.start-btn:focus-visible{outline:2px solid #9dc0ff;outline-offset:4px}
.start-btn:disabled{cursor:progress;filter:grayscale(.25) brightness(.9)}
/* 阶段反馈：每个状态有自己的配色与文案（由 JS 写 data-state） */
.start-btn[data-state=paused]{background:linear-gradient(180deg,#3d4654,#2b323d);border-color:#3a4350;box-shadow:0 14px 36px rgba(20,24,30,.42)}
.start-btn[data-state=stopped]{background:linear-gradient(180deg,#4a4030,#33291c);border-color:#5a4a2c}
.start-btn[data-state=running]:disabled{cursor:default}
/* 加载环：三段共用一个旋转环，空/静态态自动隐藏 */
.btn-load{position:absolute;width:22px;height:22px;border-radius:50%;opacity:0;transition:opacity .18s ease}
.btn-spinner{
  border:2px solid rgba(255,255,255,.28);border-top-color:#fff;
  animation:btnspin .8s linear infinite;left:22px;
}
.btn-arc{border:2px solid rgba(255,255,255,.14);border-top-color:rgba(255,255,255,.55);animation:btnspin 2.4s linear infinite;right:22px}
@keyframes btnspin{to{transform:rotate(360deg)}}
.start-btn[data-state=idle] .btn-spinner,
.start-btn[data-state=stopped] .btn-spinner,
.start-btn[data-state=paused] .btn-arc,
.start-btn[data-state=running] .btn-arc{opacity:1}
.start-btn[data-state=busy]{filter:saturate(.85)}
.start-btn[data-state=error]{background:linear-gradient(180deg,#d45a5a,#a63b3b);border-color:#8f2f2f}
/* 隐藏式后门（配置面可达但不占视觉）：右下角一个几乎不可见的点 */
.home-admin{
  position:absolute;bottom:14px;right:16px;font-size:11px;line-height:1;
  color:transparent;padding:6px 10px;border-radius:6px;
  transition:color .18s ease,background .18s ease;
}
.home-admin:hover,.home-admin:focus-visible{
  color:var(--dim);background:rgba(255,255,255,.04);outline:none;
}
/* ── 管理面板 ─────────────────────────────────── */
body.admin{display:flex;flex-direction:column}
.topbar{
  display:flex;align-items:center;gap:18px;padding:14px 22px;
  border-bottom:1px solid var(--line);background:var(--panel);
}
.brand{font-weight:600;letter-spacing:.04em}
.topstat{color:var(--dim);font-size:13px;flex:1}
.topactions{display:flex;gap:8px}
.btn{
  padding:7px 14px;border-radius:9px;border:1px solid var(--line);
  background:var(--panel2);color:var(--fg);font-size:13px;transition:.14s;
}
.btn:hover{border-color:#38424f;background:#20262e}
.btn.primary{background:var(--accent);border-color:var(--accent)}
.btn.primary:hover{filter:brightness(1.08)}
.btn.danger{color:#ffb3b3;border-color:#5a2a2a;background:#2a1618}
.btn.danger:hover{background:#3a1b1e}
.btn.ghost{background:transparent}
.btn.sm{padding:4px 9px;font-size:12px;border-radius:7px}
.tabs{display:flex;gap:4px;padding:10px 22px 0;border-bottom:1px solid var(--line);background:var(--panel)}
.tab{
  padding:9px 16px;border:0;border-bottom:2px solid transparent;background:transparent;
  color:var(--dim);font-size:14px;transition:.14s;
}
.tab.active{color:var(--fg);border-bottom-color:var(--accent)}
.tab:hover{color:var(--fg)}
.content{flex:1;overflow:auto;padding:20px 22px 40px}
.tabpane{display:none}
.tabpane.active{display:block}

.card{
  background:var(--panel);border:1px solid var(--line);border-radius:var(--radius);
  padding:16px 18px;margin-bottom:16px;
}
.card h3{margin:0 0 12px;font-size:14px;font-weight:600;letter-spacing:.04em;color:#c7d2df}
.grid{display:grid;gap:14px}
.grid.c4{grid-template-columns:repeat(auto-fit,minmax(160px,1fr))}
.grid.c2{grid-template-columns:repeat(auto-fit,minmax(320px,1fr))}
.kpi{background:var(--panel2);border:1px solid var(--line);border-radius:10px;padding:14px}
.kpi .v{font-size:26px;font-weight:600;letter-spacing:.02em}
.kpi .l{font-size:12px;color:var(--dim);margin-top:4px}
table{width:100%;border-collapse:collapse;font-size:13px}
th,td{text-align:left;padding:9px 10px;border-bottom:1px solid var(--line);vertical-align:top}
th{color:var(--dim);font-weight:500;font-size:12px;letter-spacing:.04em}
tr:hover td{background:#171c23}
.tag{display:inline-block;padding:1px 8px;border-radius:20px;font-size:11px;border:1px solid var(--line);color:var(--dim)}
.state-done{color:var(--accent2)}
.state-running{color:var(--accent)}
.state-failed,.state-timeout{color:var(--danger)}
.state-rejected,.state-cancelled{color:var(--dim)}
.state-queued{color:var(--warn)}
input,select,textarea{
  background:#0f1318;border:1px solid var(--line);border-radius:8px;color:var(--fg);
  padding:8px 10px;font-size:13px;font-family:inherit;width:100%;
}
input:focus,select:focus,textarea:focus{outline:none;border-color:var(--accent)}
label{display:block;font-size:12px;color:var(--dim);margin:10px 0 4px}
.row{display:flex;gap:10px;align-items:center}
.row > *{flex:1}
.row > .fix{flex:0 0 auto}
.muted{color:var(--dim);font-size:12px}
pre{background:#0f1318;border:1px solid var(--line);border-radius:8px;padding:12px;overflow:auto;font-size:12px;max-height:460px}
.doc{background:#0f1318;border:1px solid var(--line);border-radius:10px;padding:20px 24px;max-height:60vh;overflow:auto;line-height:1.75}
.doc h1,.doc h2,.doc h3{margin:18px 0 10px}
.doc h1{font-size:22px}.doc h2{font-size:18px}.doc h3{font-size:16px}
.doc p{margin:8px 0}
.doc ul,.doc ol{margin:8px 0;padding-left:22px}
.doc code{background:#1c232c;padding:1px 5px;border-radius:4px;font-size:12px}
.toast{
  position:fixed;bottom:22px;left:50%;transform:translateX(-50%) translateY(20px);
  background:#232a33;border:1px solid var(--line);color:var(--fg);
  padding:10px 18px;border-radius:10px;opacity:0;transition:.22s;pointer-events:none;font-size:13px;
}
.toast.show{opacity:1;transform:translateX(-50%) translateY(0)}
.empty{color:var(--dim);font-size:13px;padding:18px 0;text-align:center}

/* ── 响应式适配（目标要求 3）──────────────────────────────
   断点策略：≥1080 桌面 / 720–1080 小笔电 / 480–720 平板竖屏 / ≤480 手机。
   首页按钮用 min()/clamp 连续缩放，不依赖断点；子页面在此处做结构重排。 */
@media (max-width:1080px){
  .content{padding:16px 16px 32px}
  .topbar{gap:12px;padding:12px 16px}
  .tabs{padding:8px 16px 0}
}
@media (max-width:720px){
  .topbar{flex-wrap:wrap;row-gap:10px}
  .brand{width:100%}
  .topstat{flex:1 1 100%;order:3}
  .topactions{flex-wrap:wrap;gap:6px}
  .tabs{overflow-x:auto;-webkit-overflow-scrolling:touch;scrollbar-width:none}
  .tabs::-webkit-scrollbar{display:none}
  .tab{flex:0 0 auto;padding:10px 14px}
  .row{flex-direction:column;align-items:stretch}
  .grid.c2,.grid.c4{grid-template-columns:1fr}
  .gov-grid{grid-template-columns:repeat(auto-fill,minmax(130px,1fr))}
  /* 表格固定布局 + 任意位置可断行：审计日志这类「内容长度不可控」的表，
     在窄屏下也不得把整页撑出横向滚动。 */
  table{font-size:12px;table-layout:fixed}
  th,td{padding:8px 6px;word-break:break-word;overflow-wrap:anywhere}
  /* 窄屏让长日志/长文本换行，而不是把整页撑出横向滚动 */
  pre{white-space:pre-wrap;word-break:break-word}
}
@media (max-width:480px){
  body{font-size:14px}
  .content{padding:12px 10px 26px}
  .card{padding:13px 12px;border-radius:10px}
  .start-btn{border-radius:18px;letter-spacing:.06em}
  .home-admin{bottom:max(10px,env(safe-area-inset-bottom));right:12px}
  .btn{padding:8px 12px}
  .toast{left:10px;right:10px;transform:translateY(20px);text-align:center}
  .toast.show{transform:translateY(0)}
}
/* 低高度横屏（手机横放 / 小窗口）：保证按钮仍在首屏内且不溢出 */
@media (max-height:520px){
  .start-btn{min-height:88px;font-size:clamp(20px,4.6vw,26px)}
}
@media (min-width:1600px){
  .content{padding:24px 32px 48px}
}
/* 触屏/窄屏下可点性：主按钮与页签 ≥44px，桌面保持紧凑 */
@media (pointer:coarse), (max-width:720px){
  .btn{min-height:44px}
  .tab{min-height:44px}
}
/* 无障碍：尊重系统「减少动态效果」，用静态描边替代旋转动画 */
@media (prefers-reduced-motion:reduce){
  *{animation-duration:.001ms !important;animation-iteration-count:1 !important;transition-duration:.001ms !important}
  .start-btn[data-state=busy] .btn-load{opacity:1;border-top-color:rgba(255,255,255,.4)}
}
`