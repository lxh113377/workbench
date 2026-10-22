#!/usr/bin/env node
// design-shots.mjs —— 设计走查「定版截图」工具（不是门禁；门禁在 frontend-gate.mjs）。
// 用 CDP 直接控制 Edge：建 target → 设视口 → navigate → 等 → captureBeyondViewport。
import { spawn, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import http from "node:http";
import os from "node:os";
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
const OUT = path.resolve(arg("out", path.join(ROOT, "dist", "ui-shots", "design-" + new Date().toISOString().replace(/[:.]/g, "-").slice(0, 19))));
mkdirSync(OUT, { recursive: true });

const EDGE = ["C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe", "C:\\Program Files\\Microsoft\\Edge\\Application\\msedge.exe"].find((p) => existsSync(p));
if (!EDGE) { console.log("[shots] 未找到 msedge.exe"); process.exit(3); }

function freePort(start, end) {
  for (let p = start; p < end; p++) {
    const r = spawnSync("cmd", ["/c", "netstat -ano | findstr :" + p], { encoding: "utf8" });
    if (!r.stdout || !r.stdout.trim()) return p;
  }
  throw new Error("没有空闲端口");
}
const PORT = freePort(17955, 17999);
const CDP_PORT = freePort(9462, 9512);
const DIR = path.join(OUT, "probe-runtime");
mkdirSync(DIR, { recursive: true });

async function startInstance() {
  const cmd = String.fromCharCode(34) + EXE + String.fromCharCode(34) + " --ui-probe --dir " + String.fromCharCode(34) + DIR + String.fromCharCode(34) + " --port " + PORT + " --seconds 900";
  const ps = spawn("powershell", ["-NoProfile", "-NonInteractive", "-Command",
    "Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{ CommandLine = '" + cmd.replace(/'/g, "''") + "'; CurrentDirectory = '" + path.dirname(EXE).replace(/'/g, "''") + "' } | Select-Object -ExpandProperty ProcessId"],
    { encoding: "utf8" });
  let out = "";
  ps.stdout.on("data", (d) => (out += d.toString()));
  await new Promise((r) => ps.on("close", r));
  return parseInt(out.trim(), 10) || 0;
}
async function waitUrl() {
  const flag = path.join(DIR, "ui-probe.url");
  for (let i = 0; i < 120; i++) {
    await delay(400);
    if (!existsSync(flag)) continue;
    const lines = readFileSync(flag, "utf8").split(/\r?\n/).filter(Boolean);
    if (lines.length >= 2) return { base: lines[0].replace(/\/$/, ""), token: lines[1] };
  }
  throw new Error("候选实例 48s 内未就绪（ui-probe.url 未写出）");
}
function httpJson(url) {
  return new Promise((resolve, reject) => {
    const req = http.get(url, (res) => { let b = ""; res.on("data", (c) => (b += c)); res.on("end", () => { try { resolve(JSON.parse(b)); } catch (e) { reject(e); } }); });
    req.on("error", reject); req.setTimeout(5000, () => req.destroy(new Error("timeout")));
  });
}
class CDP {
  constructor(ws) { this.ws = ws; this.id = 0; this.pending = new Map(); }
  static async connect(url) {
    const ws = new WebSocket(url);
    await new Promise((res, rej) => { ws.addEventListener("open", res, { once: true }); ws.addEventListener("error", () => rej(new Error("ws")), { once: true }); setTimeout(() => rej(new Error("timeout")), 8000); });
    const c = new CDP(ws);
    ws.addEventListener("message", (ev) => {
      let m; try { m = JSON.parse(ev.data); } catch (e) { return; }
      if (m.id && c.pending.has(m.id)) { const p = c.pending.get(m.id); c.pending.delete(m.id); m.error ? p.reject(new Error(JSON.stringify(m.error))) : p.resolve(m.result); }
    });
    return c;
  }
  send(method, params, timeoutMs) {
    params = params || {}; timeoutMs = timeoutMs || 20000;
    const id = ++this.id;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.ws.send(JSON.stringify({ id, method, params }));
      setTimeout(() => { if (this.pending.has(id)) { this.pending.delete(id); reject(new Error(method + " 超时")); } }, timeoutMs);
    });
  }
}

function cases(base, token) {
  const q = "?token=" + encodeURIComponent(token);
  const A = base + "/admin" + q;
  const list = [
    { n: "home-1440", url: base + "/", w: 1440, h: 900, wait: 3000 },
    { n: "home-390", url: base + "/", w: 390, h: 844, wait: 3000 },
    { n: "home-1920", url: base + "/", w: 1920, h: 1080, wait: 3000 },
    { n: "admin-overview", url: A + "#overview", w: 1440, h: 1000, wait: 2800 },
    { n: "admin-queue", url: A + "#queue", w: 1440, h: 1000, wait: 2800 },
    { n: "admin-outputs", url: A + "#outputs", w: 1440, h: 1000, wait: 2800 },
    { n: "admin-templates", url: A + "#templates", w: 1440, h: 900, wait: 2600 },
    { n: "admin-autonomy", url: A + "#autonomy", w: 1440, h: 1000, wait: 2800 },
    { n: "admin-evaluate", url: A + "#evaluate", w: 1440, h: 900, wait: 2600 },
    { n: "admin-logs", url: A + "#logs", w: 1440, h: 900, wait: 2800 },
    { n: "admin-governance", url: A + "#governance", w: 1440, h: 1000, wait: 3000 },
    { n: "admin-workspace", url: A + "#workspace", w: 1440, h: 950, wait: 2800 },
    { n: "admin-settings", url: A + "#settings", w: 1440, h: 900, wait: 2600 },
    { n: "admin-overview-390", url: A + "#overview", w: 390, h: 844, wait: 2800 },
    { n: "modelpool", url: base + "/modelpool" + q, w: 1440, h: 1200, wait: 2800 },
    { n: "modelpool-390", url: base + "/modelpool" + q, w: 390, h: 900, wait: 2800 },
    { n: "governance", url: base + "/governance" + q, w: 1440, h: 1200, wait: 2800 },
    { n: "governance-390", url: base + "/governance" + q, w: 390, h: 900, wait: 2800 },
  ];
  const only = (arg("cases", "") || "").split(",").map((s) => s.trim()).filter(Boolean);
  return only.length ? list.filter((c) => only.some((o) => c.n.indexOf(o) >= 0)) : list;
}

(async () => {
  const pid = await startInstance();
  let browser = null;
  try {
    const info = await waitUrl();
    const base = info.base, token = info.token;
    console.log("[shots] 候选实例 " + base + "（pid=" + pid + "）→ " + OUT);
    const profile = path.join(OUT, "cdp-profile-" + Date.now());
    browser = spawn(EDGE, ["--headless=new", "--disable-gpu", "--no-first-run", "--no-default-browser-check", "--disable-extensions", "--hide-scrollbars", "--mute-audio", "--remote-debugging-port=" + CDP_PORT, "--user-data-dir=" + profile, "about:blank"], { stdio: ["ignore", "ignore", "ignore"] });
    let version = null;
    for (let i = 0; i < 60; i++) { await delay(400); try { version = await httpJson("http://127.0.0.1:" + CDP_PORT + "/json/version"); if (version && version.webSocketDebuggerUrl) break; } catch (e) {} }
    if (!version) throw new Error("CDP 未就绪");
    const bw = await CDP.connect(version.webSocketDebuggerUrl);
    const created = [];
    bw.ws.addEventListener("message", (ev) => { try { const m = JSON.parse(ev.data); if (m.method === "Target.targetCreated" && m.params.targetInfo.type === "page") created.push(m.params.targetInfo.targetId); } catch (e) {} });
    await bw.send("Target.setDiscoverTargets", { discover: true });
    let n = 0;
    for (const c of cases(base, token)) {
      try {
        const before = created.length;
        await bw.send("Target.createTarget", { url: "about:blank" });
        for (let i = 0; i < 60 && created.length === before; i++) await delay(100);
        const targetId = created[created.length - 1];
        const page = await CDP.connect("ws://127.0.0.1:" + CDP_PORT + "/devtools/page/" + targetId);
        await page.send("Page.enable"); await page.send("Runtime.enable");
        await page.send("Emulation.setDeviceMetricsOverride", { width: c.w, height: c.h, deviceScaleFactor: 1, mobile: c.w < 600 });
        await page.send("Page.navigate", { url: c.url });
        await delay(c.wait);
        const r = await page.send("Page.captureScreenshot", { format: "png", captureBeyondViewport: true }, 25000);
        writeFileSync(path.join(OUT, c.n + ".png"), Buffer.from(r.data, "base64"));
        console.log("[shots] ok   " + c.n + " " + c.w + "x" + c.h);
        n++;
        try { await bw.send("Target.closeTarget", { targetId }); } catch (e) {}
        try { page.ws.close(); } catch (e) {}
      } catch (e) { console.log("[shots] FAIL " + c.n + " " + String(e).slice(0, 160)); }
    }
    console.log("[shots] 完成 " + n + " 张 → " + OUT);
  } catch (e) {
    console.log("[shots] 失败：" + String(e).slice(0, 300)); process.exitCode = 3;
  } finally {
    if (browser) { try { browser.kill("SIGKILL"); } catch (e) {} }
    if (pid) spawnSync("taskkill", ["/PID", String(pid), "/T", "/F"], { stdio: "ignore" });
  }
})();
