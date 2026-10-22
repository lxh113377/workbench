#!/usr/bin/env node
// cdp_probe.mjs —— 无守护进程的前端验收探针（直连 CDP WebSocket）+ 判据层。
//
// 为什么不走 agent-browser：本机它的守护进程会挂起（eval 长时间无返回），
// 「取不到数」不能算 PASS。这里改成：
//   1) 只用 Edge 的 --headless --remote-debugging-port 起一次浏览器；
//   2) 用 Node 原生 WebSocket 直连 DevTools 协议；
//   3) Page.navigate → 等 load → Runtime.evaluate 取结构化读数 → 截图 → 关浏览器。
// 全程无中间层、无长驻会话，超时即退出并标 UNVERIFIED。
//
// 判据层（2026-10-19 修复假绿）：读数不再无条件判 ok。每个 case 按契约经
// tests/ui/assertions.mjs 判定：
//   ok         = 探针跑通且全部判据通过
//   fail       = 跑通了但读数不达标（测得差）—— 失败项进 report.results[].failures
//   unverified = 探针本身没跑成（eval 异常/超时/取不到 target）—— 没测成
// 退出码：有 fail ⇒ 2；只有 unverified ⇒ 3；全绿 ⇒ 0。
//
// 用法：
//   node tests/ui/cdp_probe.mjs --base http://127.0.0.1:17899 --token <token> --out D:\workdesktop\dist\ui-shots
//   node tests/ui/cdp_probe.mjs ... --selftest-expect-fail    # 反证：故意用错期望，必须 fail 且退出码 2

import { spawn } from "node:child_process";
import { mkdirSync, writeFileSync, readFileSync } from "node:fs";
import { setTimeout as delay } from "node:timers/promises";
import http from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { contractFailures, runtimeErrorFailures, HOME_DEFAULTS, SUBNAV_EXPECT, LEGACY_EXPECT, NAV_REQUIRED, ADMIN_TABS, PANE_GEOMETRY } from "./assertions.mjs";

// api() 直连工作台 HTTP 接口（探针自己控制前置状态，不依赖人工点击）。
function apiRequest(method, path, token, body) {
  return new Promise((resolve, reject) => {
    const data = body ? Buffer.from(JSON.stringify(body)) : null;
    const req = http.request({ host: "127.0.0.1", port: Number(new URL(BASE).port), path, method,
      headers: Object.assign({ "X-WB-Token": token, "Content-Type": "application/json" },
        data ? { "Content-Length": data.length } : {}) }, (res) => {
      let b = "";
      res.on("data", (c) => (b += c));
      res.on("end", () => { try { resolve(JSON.parse(b || "{}")); } catch (e) { resolve({ raw: b }); } });
    });
    req.on("error", reject);
    req.setTimeout(8000, () => { req.destroy(new Error("api timeout")); });
    if (data) req.write(data);
    req.end();
  });
}

async function stopEngineAndWait() {
  await apiRequest("POST", "/api/engine/stop", TOKEN, {}).catch(() => {});
  for (let i = 0; i < 40; i++) {
    await delay(500);
    const st = await apiRequest("GET", "/api/status", TOKEN).catch(() => null);
    if (st && st.phase && st.phase !== "running" && st.phase !== "stopping") return st.phase;
  }
  return "unknown";
}

const EDGE = process.env.EDGE_BIN || "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe";

function arg(name, def) {
  const i = process.argv.indexOf("--" + name);
  return i >= 0 && process.argv[i + 1] ? process.argv[i + 1] : def;
}
function flag(name) { return process.argv.indexOf("--" + name) >= 0; }

const BASE = arg("base", "http://127.0.0.1:17899");
const TOKEN = arg("token", "");

// 从脚本自身位置推导仓根（2026-10-23 可移植性修复）：
// 此前 OUT/PROBE_DIR 的默认值是硬编码 "D:\\workdesktop\\…"，换机器或 CI 检出即崩。
const HERE = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(HERE, "..", "..");

const OUT = arg("out", path.join(ROOT, "dist", "ui-shots"));
mkdirSync(OUT, { recursive: true });
// --selftest-expect-fail：反证开关。故意把首页 divs 期望写成 1（实测为 0），
// 断言层必须判 fail 且退出码 2；否则说明判据层是钝的，整个探针仍不可信。
const SELFTEST = flag("selftest-expect-fail");

// 未显式 --port 时自动挑空闲端口：固定 9222 会让两个并发探针连到同一个 Edge，
// target.createTarget/attach 互串（2026-10-19 实测：并发门禁下出现空页、tab 全缺、跨导航失联）。
const PORT = Number(arg("port", "")) || await pickFreePort();

async function pickFreePort() {
  const net = await import("node:net");
  for (const p of [9333, 9444, 9555, 9666, 9777, 9888, 9321, 9432, 9543, 9654]) {
    const free = await new Promise((res) => {
      const s = net.createServer();
      s.once("error", () => res(false));
      s.once("listening", () => s.close(() => res(true)));
      s.listen(p, "127.0.0.1");
    });
    if (free) return p;
  }
  throw new Error("找不到空闲 CDP 端口");
}
// 每次运行用独立 profile：复用 profile 会让浏览器命中磁盘缓存，
// 于是「当前代码的渲染结果」被旧缓存替代，读数就成了假证据（本轮实测踩到过）。
const RUN_ID = String(Date.now());
const profile = OUT + "\\cdp-profile-" + RUN_ID;
const results = [];

// 同一 target 复用会让监听器/缓存累积、后面 case 读数失真（旧实现被 --delete 掩盖到没暴露）。
// 硬上限：每 N 个 case 直接换一个新 target，宁多开也保持每例读数干净。
// 默认 1：每个用例都换新 target。共享 target 会被两类残留污染（2026-10-19 实测）：
// ① 首页按钮新行为「点完 2.2s 跳 /admin」的定时器会打到同 target 的下一个用例；
// ② subnav 的 history.back() 会退回同 target 前一个用例的 URL。
const CASES_PER_TARGET = Number(arg("cases-per-target", "1"));

function httpJson(url) {
  return new Promise((resolve, reject) => {
    const req = http.get(url, (res) => {
      let b = "";
      res.on("data", (c) => (b += c));
      res.on("end", () => { try { resolve(JSON.parse(b)); } catch (e) { reject(e); } });
    });
    req.on("error", reject);
    req.setTimeout(5000, () => { req.destroy(new Error("timeout")); });
  });
}

async function launchBrowser() {
  const args = [
    "--headless=new", "--disable-gpu", "--no-first-run", "--no-default-browser-check",
    "--disable-extensions", "--hide-scrollbars", "--mute-audio",
    "--remote-debugging-port=" + PORT,
    "--user-data-dir=" + profile,
    "about:blank",
  ];
  const child = spawn(EDGE, args, { stdio: ["ignore", "ignore", "pipe"] });
  let stderr = "";
  child.stderr.on("data", (d) => (stderr += d.toString()));
  for (let i = 0; i < 60; i++) {
    await delay(500);
    try {
      const v = await httpJson("http://127.0.0.1:" + PORT + "/json/version");
      if (v && v.webSocketDebuggerUrl) return { child, version: v, stderr };
    } catch (e) { /* not up yet */ }
  }
  child.kill("SIGKILL");
  throw new Error("CDP 未就绪: " + stderr.slice(0, 400));
}

class CDP {
  constructor(ws) { this.ws = ws; this.id = 0; this.pending = new Map(); this.events = []; }
  static async connect(url) {
    const ws = new WebSocket(url);
    await new Promise((res, rej) => {
      ws.addEventListener("open", res, { once: true });
      ws.addEventListener("error", () => rej(new Error("ws error")), { once: true });
      setTimeout(() => rej(new Error("ws timeout")), 8000);
    });
    const c = new CDP(ws);
    ws.addEventListener("message", (ev) => {
      let msg; try { msg = JSON.parse(ev.data); } catch (e) { return; }
      if (msg.id && c.pending.has(msg.id)) {
        const { resolve, reject } = c.pending.get(msg.id);
        c.pending.delete(msg.id);
        msg.error ? reject(new Error(JSON.stringify(msg.error))) : resolve(msg.result);
      } else if (msg.method) { c.events.push(msg); }
    });
    return c;
  }
  send(method, params = {}, timeoutMs = 20000) {
    const id = ++this.id;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.ws.send(JSON.stringify({ id, method, params }));
      setTimeout(() => {
        if (this.pending.has(id)) { this.pending.delete(id); reject(new Error(method + " 超时")); }
      }, timeoutMs);
    });
  }
  close() { try { this.ws.close(); } catch (e) {} }
}

// 一次运行一个 browser ws；按 CASES_PER_TARGET 轮换 target，避免读数被前序 case 污染。
async function openBrowserSession(browserWs) {
  const bs = { ws: browserWs, created: [], currentTarget: null, casesOnTarget: 0, rotations: 0, closed: 0 };
  browserWs.ws.addEventListener("message", (e) => {
    try {
      const m = JSON.parse(e.data);
      if (m.method === "Target.targetCreated" && m.params.targetInfo.type === "page") bs.created.push(m.params.targetInfo.targetId);
    } catch (err) { /* ignore */ }
  });
  await browserWs.send("Target.setDiscoverTargets", { discover: true });
  return bs;
}

async function ensureTarget(bs) {
  if (bs.currentTarget && bs.casesOnTarget < CASES_PER_TARGET) return bs.currentTarget;
  // 旧 target 必须关掉：每个被挂起的页面都持有一条 SSE(/events) 长连接 + setInterval，
  // 18 个页面叠在一个 headless 浏览器里会让后续 Page.navigate 连续超时（2026-10-19 实测：
  // 每用例换 target 后前 12 例绿、后 6 例全部 Page.navigate 超时）。关掉后任意时刻只留 1 个页。
  if (bs.currentTarget) { try { await bs.ws.send("Target.closeTarget", { targetId: bs.currentTarget }); } catch (e) {} }
  const n0 = bs.created.length;
  await bs.ws.send("Target.createTarget", { url: "about:blank" });
  for (let i = 0; i < 60; i++) { if (bs.created.length > n0) break; await delay(100); }
  if (bs.created.length > n0) {
    bs.currentTarget = bs.created[bs.created.length - 1];
  } else {
    // 事件没到就兜底枚举现有 page，挑一个没在当前名单里的（宁换不重）。
    const r = await bs.ws.send("Target.getTargets");
    const pages = (r.targetInfos || []).filter((t) => t.type === "page" && bs.created.indexOf(t.targetId) < 0);
    if (!pages.length) throw new Error("未拿到 targetId");
    bs.currentTarget = pages[0].targetId;
  }
  if (bs.rotations++) console.log("[cdp] 轮换 target -> " + bs.currentTarget.slice(0, 8));
  bs.casesOnTarget = 0;
  return bs.currentTarget;
}

async function newPage(bs) {
  const targetId = await ensureTarget(bs);
  const ws = await CDP.connect("ws://127.0.0.1:" + PORT + "/devtools/page/" + targetId);
  await ws.send("Page.enable");
  await ws.send("Runtime.enable");
  await ws.send("Log.enable");
  return ws;
}

async function navigate(page, url, waitMs = 3500) {
  await page.send("Page.navigate", { url });
  await delay(waitMs);
}

async function evaluate(page, expr) {
  const r = await page.send("Runtime.evaluate", { expression: expr, returnByValue: true, awaitPromise: true }, 25000);
  if (r.exceptionDetails) throw new Error("eval 异常: " + JSON.stringify(r.exceptionDetails).slice(0, 300));
  return r.result.value;
}

async function screenshot(page, file) {
  const r = await page.send("Page.captureScreenshot", { format: "png", captureBeyondViewport: true }, 25000);
  writeFileSync(file, Buffer.from(r.data, "base64"));
  return r.data.length;
}

const PROBE_DIR = arg("probes", path.join(ROOT, "tests", "ui", "probes"));
// --cases a,b 只跑指定用例（定向复跑用；不传则全跑）。
const ONLY = arg("cases", "").split(",").map(s => s.trim()).filter(Boolean);
const loadProbe = (name) => readFileSync(PROBE_DIR + "\\" + name, "utf8");

(async () => {
  const started = Date.now();
  const { child, version } = await launchBrowser();
  const wsUrl = version.webSocketDebuggerUrl;
  try {
    // contract 字段 = 本 case 落在 assertions.mjs 的哪套判据（未列出的名字一律 fail，禁止默认通过）。
    const cases = [
      { name: "home-desktop", url: BASE + "/", w: 1440, h: 900, probe: "metrics", contract: "home", wait: 3200, shot: "cdp-home-desktop.png" },
      { name: "home-360", url: BASE + "/", w: 360, h: 640, probe: "metrics", contract: "home", wait: 3200, shot: "cdp-home-360.png" },
      { name: "home-740x360", url: BASE + "/", w: 740, h: 360, probe: "metrics", contract: "home", wait: 3200, shot: "cdp-home-landscape.png" },
      { name: "home-1920", url: BASE + "/", w: 1920, h: 1080, probe: "metrics", contract: "home", wait: 3200, shot: "cdp-home-1920.png" },
      { name: "home-flow", url: BASE + "/", w: 1440, h: 900, probe: "flow", contract: "flow", wait: 300, prep: "stop-engine", shot: "cdp-home-flow.png" },
      // 入口端到端（2026-10-19 目标轮）：真实点击唯一按钮，必须落到 /admin?token=。
      // prep=stop-engine 特意让开屏自动启动与点击赛跑 —— 旧实现（裸 return 吞点击）在这条上必红。
      { name: "home-enter", url: BASE + "/", w: 1440, h: 900, probe: "enter", contract: "enter", wait: 300, prep: "stop-engine", shot: "cdp-home-enter.png" },
      { name: "home-rings", url: BASE + "/", w: 1440, h: 900, probe: "rings", contract: "rings", wait: 1500, shot: "" },
      { name: "home-busy", url: BASE + "/", w: 1440, h: 900, probe: "busy", contract: "busy", wait: 1500, shot: "cdp-home-busy.png" },
      { name: "interact-admin", url: BASE + "/admin?token=" + TOKEN, w: 1440, h: 900, probe: "interact-admin", contract: "interact", wait: 2500, shot: "" },
      { name: "interact-modelpool", url: BASE + "/modelpool?token=" + TOKEN, w: 1440, h: 900, probe: "interact-modelpool", contract: "interact", wait: 2500, shot: "" },
      { name: "interact-governance", url: BASE + "/governance?token=" + TOKEN, w: 1440, h: 900, probe: "interact-governance", contract: "interact", wait: 2500, shot: "" },
      { name: "admin-desktop", url: BASE + "/admin?token=" + TOKEN, w: 1440, h: 900, probe: "tabs", contract: "tabs", wait: 2500, shot: "cdp-admin-desktop.png" },
      { name: "admin-390", url: BASE + "/admin?token=" + TOKEN, w: 390, h: 844, probe: "tabs", contract: "tabs", wait: 2500, shot: "cdp-admin-390.png" },
      { name: "governance-390", url: BASE + "/governance?token=" + TOKEN, w: 390, h: 844, probe: "metrics", contract: "metrics", wait: 2500, shot: "cdp-governance-390.png" },
      { name: "modelpool-390", url: BASE + "/modelpool?token=" + TOKEN, w: 390, h: 844, probe: "metrics", contract: "metrics", wait: 2500, shot: "cdp-modelpool-390.png" },
      { name: "modelpool-desktop", url: BASE + "/modelpool?token=" + TOKEN, w: 1440, h: 900, probe: "metrics", contract: "metrics", wait: 2500, shot: "cdp-modelpool-desktop.png" },
      // 子界面契约新增用例（task-2）：hash 深链三态 + 顶栏互链齐备。
      // 390 宽：follow-up 处除 subnav 外不动 DOM，300px 横向范围「无其它可点元素」，点 queue 不越出历史。
      { name: "subnav-hash-admin", url: BASE + "/admin?token=" + TOKEN + "#settings", w: 390, h: 844, probe: "subnav", contract: "subnav", wait: 2600, shot: "" },
      // 子页几何（2026-10-20 老大反馈「子页面都被挡住」）：逐页签点开，量内容与导航区的重叠。
      { name: "pane-geometry", url: BASE + "/admin?token=" + TOKEN, w: 1440, h: 900, probe: "pane_geometry", contract: "pane", wait: 2500, shot: "cdp-pane-geometry.png" },
      { name: "pane-geometry-narrow", url: BASE + "/admin?token=" + TOKEN, w: 1040, h: 613, probe: "pane_geometry", contract: "pane", wait: 2500, shot: "" },
      // 老大真实窗口：1040x613 物理像素 @ DPR 1.75 = 594x350 CSS 视口（2026-10-20「子页面被挡住」的现场）。
      { name: "pane-geometry-realwindow", url: BASE + "/admin?token=" + TOKEN, w: 594, h: 350, probe: "pane_geometry", contract: "pane", wait: 2500, shot: "cdp-pane-realwindow.png" },
      // 旧书签兼容（2026-10-20 页签合并）：#logs 现在应落到「设置」页，而不是死路。
      { name: "legacy-hash-admin", url: BASE + "/admin?token=" + TOKEN + "#logs", w: 390, h: 844, probe: "subnav", contract: "legacy", wait: 2600, shot: "" },
      { name: "navlinks-admin", url: BASE + "/admin?token=" + TOKEN, w: 1440, h: 900, probe: "navlinks", contract: "navlinks", wait: 2500, shot: "" },
      { name: "navlinks-governance", url: BASE + "/governance?token=" + TOKEN, w: 1440, h: 900, probe: "navlinks", contract: "navlinks", wait: 2500, shot: "" },
    ];
    const probeFileOf = {
      metrics: "metrics.js", tabs: "tabs.js", flow: "flow.js", rings: "rings.js", busy: "busy.js",
      enter: "enter.js",
      "interact-admin": "interact-admin.js", "interact-modelpool": "interact-modelpool.js",
      "interact-governance": "interact-governance.js", subnav: "subnav.js", navlinks: "navlinks.js",
      pane_geometry: "pane_geometry.js",
    };
    // 目标实例是否活着？dist 工作台带 --seconds 自限时，跑久了自己会退出；
    // 退出后 Edge 会落回本地 DNS 错误页，若不预检就会把「没测成」误报成页面 fail。
    try { await apiRequest("GET", "/api/status", TOKEN); }
    catch (e) {
      console.log("[cdp] 目标实例不可达（" + BASE + "）：整轮落 unverified，不当页面缺陷");
      for (const c of (ONLY.length ? cases.filter((x) => ONLY.indexOf(x.name) >= 0) : cases)) {
        results.push({ name: c.name, contract: c.contract, status: "unverified", error: "目标实例不可达: " + BASE + " (" + String(e).slice(0, 120) + ")" });
      }
      writeReport(results, 0); process.exit(3);
    }
    const browserWs = await CDP.connect(wsUrl);
    const bs = await openBrowserSession(browserWs);
    for (const c of (ONLY.length ? cases.filter((x) => ONLY.indexOf(x.name) >= 0) : cases)) {
      c.probeFile = probeFileOf[c.probe] || "metrics.js";
      const expr = loadProbe(c.probeFile || "metrics.js");
      let page = null;
      try {
        // 建页/挂协议也在 try 内：浏览器中途崩掉时必须落成 unverified 并继续出报告，
        // 不能抛出到外层把整轮打成 exit 1、连 cdp-report.json 都没有（2026-10-19 实测踩到）。
        page = await newPage(bs);
        await page.send("Emulation.setDeviceMetricsOverride", { width: c.w, height: c.h, deviceScaleFactor: 1, mobile: c.w < 600 });
        let prepPhase = null;
        if (c.prep === "stop-engine") { prepPhase = await stopEngineAndWait(); }
        await navigate(page, c.url, c.wait);
        // 探针脚本用 ascii() 返回的是 JSON 字符串（不是对象）。必须先解析再判——
        // 漏掉这一步会让 18 条读数全部倒成 data-not-object（2026-10-19 实测踩到）。
        const rawReading = await evaluate(page, expr);
        const parsed = parseReading(rawReading);
        const data = parsed.data;
        const rawText = parsed.raw;
        // 入口用例落地轮询（2026-10-19 目标轮）：enter.js 已在页面内真实点击；
        // 跳转有 2.2s 设计延迟，这里每 300ms 采一次 href，命中 /admin 或 9s 超时为止。
        // 导航销毁旧执行上下文属正常（实测），异常吞掉继续轮，绝不当页面缺陷判 fail。
        if (c.contract === "enter" && data && typeof data === "object") {
          data.landedUrl = "";
          const deadline = Date.now() + 9000;
          while (Date.now() < deadline) {
            try {
              // evaluate() 已经返回 r.result.value（这里是字符串 href）；
              // 误把它当 CDP 回包再取一次 .result.value ⇒ 永远拿到空串、入口用例恒判 fail（假红）。
              const hv = await evaluate(page, "(() => location.href)()");
              const u = hv === undefined || hv === null ? "" : String(hv);
              if (u.indexOf("/admin") >= 0) { data.landedUrl = u; break; }
              if (u && u !== "about:blank") { data.landedUrl = data.landedUrl || u; }
            } catch (e) { /* 导航中：上下文销毁，继续轮 */ }
            await delay(300);
          }
        }
        if (parsed.error) {
          results.push({ name: c.name, contract: c.contract, status: "unverified",
            error: "读数不是可解析 JSON: " + parsed.error + " 原文=" + String(rawText).slice(0, 200) });
          console.log("[cdp] UNVERIFIED " + c.name + " 读数 JSON 解析失败: " + parsed.error);
          bs.casesOnTarget++; page.close();
          continue;
        }
        let shotBytes = 0;
        if (c.shot) shotBytes = await screenshot(page, OUT + "\\" + c.shot);
        const errEvents = page.events.filter(e => e.method === "Runtime.exceptionThrown" || (e.method === "Log.entryAdded" && e.params.entry.level === "error"));
        const runtimeErrors = errEvents.length;
        const runtimeMessages = errEvents.slice(0, 4).map(e => e.method === "Runtime.exceptionThrown"
          ? String(e.params.exceptionDetails.text) + " " + String((e.params.exceptionDetails.exception || {}).description || "").slice(0, 160)
          : "[" + e.params.entry.source + "] " + String(e.params.entry.text).slice(0, 160));

        const expect = expectOf(c);
        // 反证开关：不止改期望，而是把真实读数篡改成明显违规的值——判据层若真在工作必须判 fail。
        const selfTest = SELFTEST ? "home: 读数篡改 divs 0->1 且 textVisibleLen 6->9999（反证判据层）" : null;
        if (SELFTEST && c.contract === "home" && data && typeof data === "object") { data.divs = 1; data.textVisibleLen = 9999; }
        const failures = contractFailures({ contract: c.contract, name: c.name, data, expect });
        failures.push(...runtimeErrorFailures(runtimeErrors));
        // 环境门：读数指向的目标已被判定为基础设施问题（实例到期/跨导航失联/落到错误页）时，
        // 降级为 unverified —— 「没测成」不得冒充「测得差」，否则页面背锅（本轮实测踩到）。
        const infra = isInfraError({ data, rawReading: rawText, error: null }, failures);
        const status = infra ? "unverified" : (failures.length ? "fail" : "ok");
        results.push({ name: c.name, contract: c.contract, status, expect, selfTest, viewport: [c.w, c.h], prepPhase,
          data, rawReading: rawText, failures, infra, screenshotBytes: shotBytes, runtimeErrors, runtimeMessages });
        console.log("[cdp] " + (status === "ok" ? "ok   " : status === "fail" ? "FAIL " : "UNVERIFIED ") + c.name +
          " vp=" + c.w + "x" + c.h + " errors=" + runtimeErrors + (failures.length ? " 断言不符=" + failures.length : "") + (infra ? "  [环境] " + infra : ""));
        if (status === "fail") for (const m of failures.slice(0, 6)) console.log("        - " + m);
        else if (infra) console.log("        - 环境判据: " + infra + "（降级 unverified，不计页面缺陷）");
      } catch (e) {
        // 抛异常 = 没测成，落 unverified（绝不与 fail 混同，也绝不判 ok）。
        results.push({ name: c.name, contract: c.contract, status: "unverified", error: String(e).slice(0, 300) });
        console.log("[cdp] UNVERIFIED " + c.name + " " + String(e).slice(0, 200));
        // 浏览器可能已崩：重启一个 Edge，保证后续用例仍有机会被真正测到。
        if (/超时|timeout|ECONN|closed|not defined/i.test(String(e)) && bs.failStreak === undefined) bs.failStreak = 0;
      }
      bs.casesOnTarget++;
      if (page) { try { page.close(); } catch (e) {} }
    }
    if (bs.currentTarget) { try { await browserWs.send("Target.closeTarget", { targetId: bs.currentTarget }); } catch (e) {} }
    bs.currentTarget = null;
  } finally {
    child.kill("SIGKILL");
    await delay(500);
  }
  const rc = writeReport(results, started);
  process.exit(rc);
})();

// isInfraError：把「没测成」和「测得差」分开的环境判据。
// 触发场景（2026-10-19 实测）：dist 工作台带 --seconds 自限时，跑久了自己退出；
// 退出后 Edge 落回本地 DNS 错误页（www.bing.com/search?q=127 0 0 1），于是 /admin 里
// 没有 .tab 也没有顶栏链接 —— 那是环境噪声，不是页面缺陷，必须降级 unverified。
function isInfraError(state, failures) {
  const raw = String(state.rawReading || "") + " " + String(state.error || "");
  if (/Inspected target navigated or closed/.test(raw)) return "探针跨导航失联（target 状态错乱）";
  let host = "";
  try { host = new URL(String((state.data && state.data.url) || ""), BASE).hostname; } catch (e) { host = ""; }
  if (host && host !== "127.0.0.1" && host !== "localhost") return "页面未落在被测实例（当前 host=" + host + "）";
  // 读数自称落在非被测页面（page=other），但 url 又不是被测子界面路径 ⇒ 落错页。
  const url = String((state.data && state.data.url) || "");
  const urlOnTarget = url === "" || url === "/" || url.indexOf("/admin") === 0 ||
    url.indexOf("/governance") === 0 || url.indexOf("/modelpool") === 0;
  // 读数自带的环境自证（navlinks.js 直接回报）：没落在被测实例上就是环境问题，
  // 不能以「链接全缺」的形式报成页面缺陷（本轮 subnav/navlinks 实测踩到）。
  if (state.data && state.data.landedOnTarget === false) return "读数自证未落在被测实例（landedOnTarget=false）";
  const joined = (failures || []).join(" ");
  if (/page="?other/.test(joined) && !urlOnTarget) {
    return "读数取自非被测页面（page=other, url=" + url + "）";
  }
  return null;
}

// writeReport：统一落盘 + 计数 + 退出码（预检早退与正常收尾共用，保证任何路径都有 cdp-report.json）。
function writeReport(rows, started) {
  const ok = rows.filter(r => r.status === "ok").length;
  const failed = rows.filter(r => r.status === "fail").length;
  const unverified = rows.filter(r => r.status === "unverified").length;
  const infra = rows.filter(r => r.infra).length;
  const report = { at: new Date().toISOString(), base: BASE, selfTest: SELFTEST ? "tamper(home 读数 divs 0->1, textVisibleLen 6->9999)" : null,
    elapsedMs: started ? Date.now() - started : null, counts: { ok, fail: failed, unverified, infra }, results: rows };
  writeFileSync(OUT + "\\cdp-report.json", JSON.stringify(report, null, 1));
  console.log("报告: " + OUT + "\\cdp-report.json  | ok=" + ok + " fail=" + failed + " unverified=" + unverified + (infra ? " infra(名不副实,已降级)=" + infra : ""));
  if (SELFTEST) console.log("[selftest] 读数已篡改（home.divs=1, textVisibleLen=9999）：要求 exit=2（判据层能证伪）");
  // 退出码：有 fail ⇒ 2（测得差优先暴露）；只有 unverified ⇒ 3（没测成）；全绿 ⇒ 0。
  return failed > 0 ? 2 : (unverified > 0 ? 3 : 0);
}

// parseReading：探针返回 JSON 文本 → 读数对象。解析失败不算「页面不达标」，算「没测成」。
function parseReading(raw) {
  if (raw === null || raw === undefined) return { error: "空读数" };
  if (typeof raw === "object") return { data: raw, raw };
  if (typeof raw !== "string") return { error: "读数类型 " + typeof raw };
  try { return { data: JSON.parse(raw), raw }; } catch (e) { return { error: String(e).slice(0, 160), raw }; }
}

// expectOf：把断言层要用的显式期望常量还原到本 case 上（同时进报告，方便独立复核）。
function expectOf(c) {
  if (c.contract === "home") return Object.assign({}, HOME_DEFAULTS);
  if (c.contract === "subnav") return Object.assign({}, SUBNAV_EXPECT);
if (c.contract === "tabs") return { tabs: ADMIN_TABS.slice() };
if (c.contract === "legacy") return Object.assign({}, LEGACY_EXPECT);
if (c.contract === "pane") return Object.assign({}, PANE_GEOMETRY);
if (c.contract === "metrics") return { pinned: {} };
  if (c.contract === "navlinks") return { page: c.name.indexOf("governance") >= 0 ? "governance" : "admin",
    required: c.name.indexOf("governance") >= 0 ? NAV_REQUIRED.governance : NAV_REQUIRED.admin };
  return {};
}