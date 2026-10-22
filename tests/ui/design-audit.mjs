#!/usr/bin/env node
// design-audit.mjs —— 设计质量的**量化审计**（不是门禁，不判 PASS/FAIL，只出读数）。
//
// 为什么需要它：肉眼走查只能发现「明显难看」，发现不了
//   ① 正文对比度 4.6:1 与 3.9:1 的差别（截图上看都是灰字）
//   ② 某处 28px 的可点区域（鼠标能用、触屏点不中）
//   ③ 11px 微型标签在 390px 下的实际可读性
// 本工具在真实浏览器里取 computedStyle，逐元素算 WCAG 对比度，输出可复核的读数表。
import { spawn, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import http from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { setTimeout as delay } from "node:timers/promises";

// 从脚本自身位置推导仓根（2026-10-23 可移植性修复）：
// 此前这里是硬编码的 "D:\\workdesktop" —— 换机器 / CI 检出到其它路径就直接崩，
// 也让前端验收永远无法接进 CI（判据存在但只在某台机器上能跑）。
// 本文件位于 <ROOT>/tests/ui/，上溯两级即仓根。
// 用 Node 标准的 fileURLToPath 转路径（Windows 盘符/空格/中文都不会出错）：
// 早期写法 new URL(...).pathname 在 Windows 上得到 /D:/... 需再剥前导斜杠，正则易写错；
// fileURLToPath 一步到位。
const HERE = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(HERE, "..", "..");
function arg(name, def) { const i = process.argv.indexOf("--" + name); return i >= 0 && process.argv[i + 1] ? process.argv[i + 1] : def; }
const EXE = path.resolve(arg("exe", path.join(ROOT, "dist", "workbench.exe")));
const OUT = path.resolve(arg("out", path.join(ROOT, "dist", "ui-shots", "audit-" + new Date().toISOString().replace(/[:.]/g, "-").slice(0, 19))));
mkdirSync(OUT, { recursive: true });
const EDGE = ["C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe", "C:\\Program Files\\Microsoft\\Edge\\Application\\msedge.exe"].find((p) => existsSync(p));
if (!EDGE) { console.log("[audit] 未找到 msedge.exe"); process.exit(3); }

function freePort(a, b) { for (let p = a; p < b; p++) { const r = spawnSync("cmd", ["/c", "netstat -ano | findstr :" + p], { encoding: "utf8" }); if (!r.stdout || !r.stdout.trim()) return p; } throw new Error("no free port"); }
const PORT = freePort(17955, 17999);
const CDP_PORT = freePort(9462, 9512);

async function startInstance() {
  const dir = path.join(OUT, "probe-runtime"); mkdirSync(dir, { recursive: true });
  const cmd = String.fromCharCode(34) + EXE + String.fromCharCode(34) + " --ui-probe --dir " + String.fromCharCode(34) + dir + String.fromCharCode(34) + " --port " + PORT + " --seconds 900";
  const ps = spawn("powershell", ["-NoProfile", "-NonInteractive", "-Command",
    "Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{ CommandLine = '" + cmd.replace(/'/g, "''") + "'; CurrentDirectory = '" + path.dirname(EXE).replace(/'/g, "''") + "' } | Select-Object -ExpandProperty ProcessId"], { encoding: "utf8" });
  let out = ""; ps.stdout.on("data", (d) => (out += d.toString()));
  await new Promise((r) => ps.on("close", r));
  return { pid: parseInt(out.trim(), 10) || 0, dir };
}
async function waitUrl(dir) {
  const flag = path.join(dir, "ui-probe.url");
  for (let i = 0; i < 120; i++) { await delay(400); if (!existsSync(flag)) continue; const l = readFileSync(flag, "utf8").split(/\r?\n/).filter(Boolean); if (l.length >= 2) return { base: l[0].replace(/\/$/, ""), token: l[1] }; }
  throw new Error("实例未就绪");
}
function httpJson(url) { return new Promise((res, rej) => { const q = http.get(url, (r) => { let b = ""; r.on("data", (c) => (b += c)); r.on("end", () => { try { res(JSON.parse(b)); } catch (e) { rej(e); } }); }); q.on("error", rej); q.setTimeout(5000, () => q.destroy(new Error("timeout"))); }); }
class CDP {
  constructor(ws) { this.ws = ws; this.id = 0; this.pending = new Map(); }
  static async connect(url) {
    const ws = new WebSocket(url);
    await new Promise((r, j) => { ws.addEventListener("open", r, { once: true }); ws.addEventListener("error", () => j(new Error("ws")), { once: true }); setTimeout(() => j(new Error("timeout")), 8000); });
    const c = new CDP(ws);
    ws.addEventListener("message", (ev) => { let m; try { m = JSON.parse(ev.data); } catch (e) { return; } if (m.id && c.pending.has(m.id)) { const p = c.pending.get(m.id); c.pending.delete(m.id); m.error ? p.reject(new Error(JSON.stringify(m.error))) : p.resolve(m.result); } });
    return c;
  }
  send(method, params, t) { params = params || {}; t = t || 25000; const id = ++this.id; return new Promise((res, rej) => { this.pending.set(id, { resolve: res, reject: rej }); this.ws.send(JSON.stringify({ id, method, params })); setTimeout(() => { if (this.pending.has(id)) { this.pending.delete(id); rej(new Error(method + " timeout")); } }, t); }); }
}
const AUDIT = readFileSync(path.join(ROOT, "tests", "ui", "probes", "audit.js"), "utf8");
function pages(base, token) {
  const q = "?token=" + encodeURIComponent(token);
  return [
    { n: "home-1440", url: base + "/", w: 1440, h: 900, wait: 2600 },
    { n: "home-390", url: base + "/", w: 390, h: 844, wait: 2600 },
    { n: "admin-overview", url: base + "/admin" + q + "#overview", w: 1440, h: 1000, wait: 2800 },
    { n: "admin-queue", url: base + "/admin" + q + "#queue", w: 1440, h: 1000, wait: 2800 },
    { n: "admin-outputs", url: base + "/admin" + q + "#outputs", w: 1440, h: 1000, wait: 2800 },
    { n: "admin-templates", url: base + "/admin" + q + "#templates", w: 1440, h: 900, wait: 2600 },
    // 2026-10-20 信息架构简化（10 页签 → 5）：原来 5 条页签级审计合并进 admin-settings，
    // #autonomy/#evaluate/#logs/#governance/#workspace 这些旧 hash 仍保留在 legacy 用例里，
    // 用来确认「旧书签落到合并后的页面」时同样没有对比度/可点性缺陷。
    { n: "admin-settings", url: base + "/admin" + q + "#settings", w: 1440, h: 900, wait: 2600 },
    { n: "admin-settings-390", url: base + "/admin" + q + "#settings", w: 390, h: 844, wait: 2600 },
    { n: "admin-legacy-logs", url: base + "/admin" + q + "#logs", w: 1440, h: 900, wait: 2800 },
    { n: "admin-legacy-workspace", url: base + "/admin" + q + "#workspace", w: 390, h: 844, wait: 2800 },
    { n: "admin-overview-390", url: base + "/admin" + q + "#overview", w: 390, h: 844, wait: 2800 },
    { n: "modelpool", url: base + "/modelpool" + q, w: 1440, h: 1200, wait: 2800 },
    { n: "modelpool-390", url: base + "/modelpool" + q, w: 390, h: 900, wait: 2800 },
    { n: "governance", url: base + "/governance" + q, w: 1440, h: 1200, wait: 2800 },
    { n: "governance-390", url: base + "/governance" + q, w: 390, h: 900, wait: 2800 },
  ];
}

(async () => {
  const inst = await startInstance();
  let browser = null;
  try {
    const info = await waitUrl(inst.dir);
    console.log("[audit] 实例 " + info.base + " → " + OUT);
    const profile = path.join(OUT, "cdp-profile-" + Date.now());
    browser = spawn(EDGE, ["--headless=new", "--disable-gpu", "--no-first-run", "--no-default-browser-check", "--disable-extensions", "--hide-scrollbars", "--mute-audio", "--remote-debugging-port=" + CDP_PORT, "--user-data-dir=" + profile, "about:blank"], { stdio: ["ignore", "ignore", "ignore"] });
    let version = null;
    for (let i = 0; i < 60; i++) { await delay(400); try { version = await httpJson("http://127.0.0.1:" + CDP_PORT + "/json/version"); if (version && version.webSocketDebuggerUrl) break; } catch (e) {} }
    if (!version) throw new Error("CDP 未就绪");
    const bw = await CDP.connect(version.webSocketDebuggerUrl);
    const created = [];
    bw.ws.addEventListener("message", (ev) => { try { const m = JSON.parse(ev.data); if (m.method === "Target.targetCreated" && m.params.targetInfo.type === "page") created.push(m.params.targetInfo.targetId); } catch (e) {} });
    await bw.send("Target.setDiscoverTargets", { discover: true });
    // 减少动效核查：先把媒体特性模拟成 reduce，再单独跑首页一次，读实际 animationDuration。
    // 「我们尊重 prefers-reduced-motion」是 CSS 里写着的声明，必须实测而非自述。
    let reduceReading = null;
    try {
      const before = created.length;
      await bw.send("Target.createTarget", { url: "about:blank" });
      for (let i = 0; i < 60 && created.length === before; i++) await delay(100);
      const rid = created[created.length - 1];
      const rp2 = await CDP.connect("ws://127.0.0.1:" + CDP_PORT + "/devtools/page/" + rid);
      await rp2.send("Page.enable"); await rp2.send("Runtime.enable");
      await rp2.send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-reduced-motion", value: "reduce" }] });
      await rp2.send("Emulation.setDeviceMetricsOverride", { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false });
      await rp2.send("Page.navigate", { url: info.base + "/" });
      await delay(2600);
      const rr = await rp2.send("Runtime.evaluate", { expression: AUDIT, returnByValue: true, awaitPromise: true }, 30000);
      const rv = rr.result && rr.result.value;
      if (rv) { const rd = JSON.parse(rv); reduceReading = { prefersReducedMotion: rd.prefersReducedMotion, animSample: rd.animSample, animatedElements: rd.animatedElements }; }
      console.log("[audit] reduce 模拟: matches=" + (reduceReading && reduceReading.prefersReducedMotion) + " btnAnim=" + JSON.stringify(reduceReading && reduceReading.animSample));
      try { await bw.send("Target.closeTarget", { targetId: rid }); } catch (e) {}
      try { rp2.ws.close(); } catch (e) {}
    } catch (e) { console.log("[audit] reduce 模拟失败 " + String(e).slice(0, 120)); }

    const rows = [];
    for (const p of pages(info.base, info.token)) {
      try {
        const before = created.length;
        await bw.send("Target.createTarget", { url: "about:blank" });
        for (let i = 0; i < 60 && created.length === before; i++) await delay(100);
        const tid = created[created.length - 1];
        const page = await CDP.connect("ws://127.0.0.1:" + CDP_PORT + "/devtools/page/" + tid);
        await page.send("Page.enable"); await page.send("Runtime.enable");
        await page.send("Emulation.setDeviceMetricsOverride", { width: p.w, height: p.h, deviceScaleFactor: 1, mobile: p.w < 600 });
        // CLS 采集必须早于首次绘制：用 evaluateOnNewDocument 在页面脚本执行前埋 observer。
        try {
          await page.send("Page.addScriptToEvaluateOnNewDocument", { source: "window.__cls=0;(function(){try{new PerformanceObserver(function(l){for(var i=0;i<l.getEntries().length;i++){var e=l.getEntries()[i];if(!e.hadRecentInput)window.__cls+=e.value;}}).observe({type:'layout-shift',buffered:true});}catch(e){}})();" });
        } catch (e) {}
        await page.send("Page.navigate", { url: p.url });
        await delay(p.wait);
        const r = await page.send("Runtime.evaluate", { expression: AUDIT, returnByValue: true, awaitPromise: true }, 30000);
        const val = r.result && r.result.value;
        if (!val) { console.log("[audit] 空读数 " + p.n); continue; }
        const data = JSON.parse(val);
        rows.push({ page: p.n, ...data });
        console.log("[audit] " + p.n.padEnd(20) + " 文本节点=" + data.textStats.nodes + " 最低对比=" + data.textStats.minRatio + " <4.5 数=" + data.textStats.below45 + " 最小可点=" + data.minTarget.h + "px 焦点缺失=" + data.focus.issues + "/" + data.focus.tested + " 溢出=" + data.overflowX);
        try { await bw.send("Target.closeTarget", { targetId: tid }); } catch (e) {}
        try { page.ws.close(); } catch (e) {}
      } catch (e) { console.log("[audit] FAIL " + p.n + " " + String(e).slice(0, 160)); }
    }
    writeFileSync(path.join(OUT, "design-audit.json"), JSON.stringify({ at: new Date().toISOString(), base: info.base, reduceReading: reduceReading, pages: rows }, null, 1));
    console.log("[audit] 完成 " + rows.length + " 页 → " + path.join(OUT, "design-audit.json"));
  } catch (e) {
    console.log("[audit] 失败 " + String(e).slice(0, 300)); process.exitCode = 3;
  } finally {
    if (browser) { try { browser.kill("SIGKILL"); } catch (e) {} }
    if (inst.pid) spawnSync("taskkill", ["/PID", String(inst.pid), "/T", "/F"], { stdio: "ignore" });
  }
})();