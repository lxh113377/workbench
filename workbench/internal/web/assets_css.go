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

/* ── 极简首页 ─────────────────────────────────── */
body.home{display:flex;align-items:center;justify-content:center;overflow:hidden}
.home-main{
  display:flex;flex-direction:column;align-items:center;justify-content:center;
  gap:28px;width:100%;height:100%;position:relative;
}
.home-badge{
  position:absolute;top:28px;font-size:13px;letter-spacing:.14em;
  color:var(--dim);text-transform:uppercase;
}
.start-btn{
  width:240px;height:96px;border-radius:18px;border:1px solid #2e6fd4;
  background:linear-gradient(180deg,#5b95ff,#3b74e0);
  color:#fff;font-size:30px;font-weight:600;letter-spacing:.08em;
  box-shadow:0 18px 48px rgba(59,116,224,.34),inset 0 1px 0 rgba(255,255,255,.24);
  transition:transform .16s ease,box-shadow .16s ease,filter .16s ease;
}
.start-btn:hover{transform:translateY(-2px);box-shadow:0 24px 60px rgba(59,116,224,.44)}
.start-btn:active{transform:translateY(0)}
.start-btn:disabled{filter:grayscale(.4) brightness(.82);cursor:default;box-shadow:none}
.home-status{margin:0;color:var(--dim);font-size:14px;letter-spacing:.04em}
/* 隐藏式后门（目标要求 6）：视觉上右下角只是一个几乎不可见的点，悬停/聚焦才显形。
   首页不得出现可见的配置入口，但配置面必须可达（不允许只能靠改配置或命令行进入）。 */
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
`