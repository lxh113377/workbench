#!/usr/bin/env node
// frontend-gate.mjs —— 一条命令的前端验收门禁（开箱即用、零提示、零点击）。
//
// 为什么要有它：前端验收此前要手工三件事 —— 起探针实例、从 ui-probe.url 抄 token、
// 再把 token 贴进 cdp_probe.mjs 的命令行。任何一步都要人参与，一旦抄错就变成
// 「取不到数」而不是「测得差」。本脚本把这三步合成一条命令，并负责收尾清理。
//
// 用法：
//   node tests/ui/frontend-gate.mjs                    # 自动起隔离实例跑全量
//   node tests/ui/frontend-gate.mjs --base http://127.0.0.1:17890 --token <t>   # 打已有实例
//   node tests/ui/frontend-gate.mjs --md <文件>        # 同时落一份 markdown 回执
//   node tests/ui/frontend-gate.mjs --no-preflight     # 跳过残留清理（默认会清）
//
// 退出码与 cdp_probe 一致：0 全绿 / 2 有 fail / 3 只有 unverified。
import { spawn, spawnSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync, existsSync, readdirSync, statSync } from "node:fs";
import { setTimeout as delay } from "node:timers/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

// 从脚本自身位置推导仓根（2026-10-23 可移植性修复）：
// 此前这里是硬编码的 "D:\\workdesktop" —— 换机器 / CI 检出到其它路径就直接崩，
// 也让前端验收永远无法接进 CI（判据存在但只在某台机器上能跑）。
// 本文件位于 <ROOT>/tests/ui/，上溯两级即仓根。
// 用 Node 标准的 fileURLToPath 转路径（Windows 盘符/空格/中文都不会出错）：
// 早期写法 new URL(...).pathname 在 Windows 上得到 /D:/... 需再剥前导斜杠，正则易写错；
// fileURLToPath 一步到位。
const HERE = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(HERE, "..", "..");
// --exe：允许指向候选构建（例如 xtmp 下的探针副本），默认仍是正式发布件。
// 用途：视觉/前端改动先在候选构建上跑门禁，绿了再换发布件，避免"台架绿、发布件红"。
const EXE = (() => { const i = process.argv.indexOf("--exe"); return i >= 0 && process.argv[i + 1] ? path.resolve(process.argv[i + 1]) : path.join(ROOT, "dist", "workbench.exe"); })();
const PROBE = path.join(ROOT, "tests", "ui", "cdp_probe.mjs");
const NO_PREFLIGHT = process.argv.includes("--no-preflight");
// --selftest-expect-fail 穿透到探针：由探针故意篡改读数，证明判据层能被证伪。
const EXPECT_FAIL = process.argv.includes("--selftest-expect-fail");
const SELFTEST_FLAG = EXPECT_FAIL ? ["--selftest-expect-fail"] : [];

// preflight —— 无人值守的前置清理（实测教训，2026-10-19）：
// 上一轮探针若被中断，Edge 进程会带着超大 profile 常驻（实测单次 13 个进程、1GB 级），
// 再叠加另一个会话的 Playwright 浏览器，机器 CPU 直接 100%，新起的探针 Page.navigate 全超时，
// 读数变成一片 unverified —— 那是「没测成」，但很容易被误读成「页面坏了」。
// 处置：起探针之前，把本仓自己的 headless profile（dist/ui-shots/*/cdp-profile-*）连同其浏览器一起收掉。
// 只动自己的产物目录，不碰别人的 Playwright/桌面浏览器。
function preflight() {
  if (NO_PREFLIGHT) { console.log("[gate] 跳过 preflight（--no-preflight）"); return; }
  const shots = path.join(ROOT, "dist", "ui-shots");
  if (!existsSync(shots)) return;
  const dirs = [];
  for (const ent of readdirSync(shots)) {
    const p = path.join(shots, ent);
    let st; try { st = statSync(p); } catch (e) { continue; }
    if (!st.isDirectory()) continue;
    for (const sub of readdirSync(p)) {
      if (sub.startsWith("cdp-profile-")) dirs.push(path.join(p, sub));
    }
    if (ent.startsWith("cdp-profile-")) dirs.push(p);
  }
  if (!dirs.length) { console.log("[gate] preflight：无残留探针 profile"); return; }
  const names = dirs.map((d) => path.basename(d));
  const r = spawnSync("powershell", ["-NoProfile", "-NonInteractive", "-Command",
    "Get-CimInstance Win32_Process -Filter \"Name='msedge.exe'\" | Where-Object { $_.CommandLine -match 'cdp-profile-' } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }"],
    { encoding: "utf8" });
  let removed = 0;
  for (const d of dirs) { const r2 = spawnSync("cmd", ["/c", "rmdir", "/s", "/q", d], { stdio: "ignore" }); if (r2.status === 0) removed++; }
  console.log("[gate] preflight：清理残留探针浏览器进程 + profile 目录 " + removed + "/" + names.length + "（避免上一轮把我的读数拖成 unverified）");
}

function arg(name, def) {
  const i = process.argv.indexOf("--" + name);
  return i >= 0 && process.argv[i + 1] ? process.argv[i + 1] : def;
}
// 期望用例数：必须与 cdp_probe.mjs 的 cases 数量严格一致。
// 2026-10-20：新增 legacy-hash-admin（旧书签映射）19 → 20；
//           再新增 pane-geometry ×2（子页是否被导航区挡住，宽/窄两档）20 → 22；
//           再补 pane-geometry-realwindow（594x350 = 用户真实窗口）22 → 23。
// 这个常量是「结果集完整」的判据，写小了就等于允许漏跑用例仍判 PASS（假绿）。
const EXPECTED_CASES = 23;

const MD = arg("md", "");
let BASE = arg("base", "");
let TOKEN = arg("token", "");
const stamp = new Date().toISOString().replace(/[:.]/g, "-").slice(0, 19);
const OUT = arg("out", path.join(ROOT, "dist", "ui-shots", "gate-" + stamp));
mkdirSync(OUT, { recursive: true });

let child = null;
let dir = null;

async function pickPort() {
  for (let p = 17931; p < 17981; p++) {
    const r = spawnSync("cmd", ["/c", "netstat -ano | findstr :" + p], { encoding: "utf8" });
    if (!r.stdout || !r.stdout.trim()) return p;
  }
  throw new Error("17931-17980 全被占用，无法起隔离探针实例");
}

async function startIsolated() {
  const port = await pickPort();
  dir = path.join(OUT, "probe-runtime");
  mkdirSync(dir, { recursive: true });
  // detached + stdio ignore：探针实例不继承本进程的控制台，也不会被作业对象连坐。
  // 用 Start-Process 而不是裸 spawn：EXE 是 windowsgui 子系统程序，
  // 由 node 直接 spawn 时对宿主终端有继承关系（实测会多出一个不可控的父链）。
  // Start-Process 让它成为独立进程，我们用 taskkill /T /F 收工。
  // 把探针实例的 stdout/stderr 落到文件：实例起不来时，判据必须是「看到它的报错」，
  // 而不是含糊的「30s 内未写出 ui-probe.url」（本轮实测踩到过，无法定位）。
  const outLog = path.join(dir, "probe-stdout.txt");
  const errLog = path.join(dir, "probe-stderr.txt");
  // 为什么不再用 PowerShell 的 Start-Process 拉它（实测两坑）：
  //   ① 宿主环境里同时存在 NO_PROXY 与 no_proxy 两个键时，Start-Process 直接抛
  //      "Item has already been added"，探针根本没起来，门禁却像在等它；
  //   ② 更致命的是 Start-Process 的 -RedirectStandardOutput **会阻塞**等子进程退出，
  //      而探针要活 900 秒 —— 于是整个门禁挂在那里（实测卡 8 分钟无任何输出）。
  // 改用 WMI 的 Win32_Process.Create（异步、无父链、可拿 pid），并把探针自身的输出
  // 交给它自己写文件（--log 由 cdp 侧不需要；这里只需要 ui-probe.url 作为就绪信号）。
  const psCmd = "Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{ CommandLine = '" + ("\"" + EXE + "\" --ui-probe --dir \"" + dir + "\" --port " + port + " --seconds 900") + "'; CurrentDirectory = '" + path.dirname(EXE) + "' } | Select-Object -ExpandProperty ProcessId";
  const ps = spawn("powershell", ["-NoProfile", "-NonInteractive", "-Command", psCmd], { encoding: "utf8" });
  let pidOut = "";
  ps.stdout.on("data", (d) => (pidOut += d.toString()));
  await new Promise((r) => ps.on("close", r));
  const pid = parseInt(pidOut.trim(), 10);
  child = { pid: isNaN(pid) ? 0 : pid };
  const flag = path.join(dir, "ui-probe.url");
  // 注意：child 只是 pid 句柄，没有 exitCode —— 提前退出的判定改为「端口没人听」。
  for (let i = 0; i < 120; i++) {
    await delay(500);
    if (existsSync(flag)) {
      const lines = readFileSync(flag, "utf8").split(/\r?\n/).filter(Boolean);
      if (lines.length >= 2) return { base: lines[0].replace(/\/$/, ""), token: lines[1] };
    }
  }
  const err = existsSync(errLog) ? readFileSync(errLog, "utf8").slice(0, 400) : "（无 stderr 文件）";
  const so = existsSync(outLog) ? readFileSync(outLog, "utf8").slice(0, 400) : "（无 stdout 文件）";
  throw new Error("探针实例 60s 内未写出 ui-probe.url ｜ stderr=" + err.replace(/\s+/g, " ") + " ｜ stdout=" + so.replace(/\s+/g, " "));
}

function killTree(pid) {
  if (!pid) return;
  spawnSync("taskkill", ["/PID", String(pid), "/T", "/F"], { stdio: "ignore" });
}

// 判据自检（2026-10-20 实测教训）：EXPECTED_CASES 必须等于探针里真实用例数，
// 否则「漏跑用例」会被判成 PASS。这里在起浏览器之前就先核对，避免整轮白跑。
(function checkCaseCount() {
  const src = readFileSync(PROBE, "utf8");
  const m = src.match(/const cases = \[([\s\S]*?)\n    \];/);
  if (!m) { console.log("[gate] 自检失败：无法从探针源码定位 cases 数组"); process.exit(3); }
  const n = (m[1].match(/\{ name: "/g) || []).length;
  if (n !== EXPECTED_CASES) {
    // 硬停：判据常量与实际用例数漂移时，后面跑出来的 PASS 不可信，直接不给结论。
    console.log("[gate] 判据自检失败：EXPECTED_CASES=" + EXPECTED_CASES + " 但探针用例数=" + n + "（漏跑将无法被发现）");
    process.exit(3);
  }
  console.log("[gate] 自检通过：EXPECTED_CASES=" + EXPECTED_CASES + " 与探针用例数一致");
})();

preflight();

try {
  if (!BASE) {
    const started = await startIsolated();
    BASE = started.base;
    TOKEN = started.token;
    console.log("[gate] 隔离实例已就绪 " + BASE + "（自起自停，无需人工）");
  }
  console.log("[gate] 目标=" + BASE + " 证据目录=" + OUT);
  // CDP 端口必须自己挑一个空的：默认 9222 是全局共享的，两个会话同时跑探针
  // 会互相把对方的浏览器实例当成自己的（实测「CDP 未就绪」的根因之一）。
  let cdpPort = 0;
  for (let p = 9411; p < 9461; p++) {
    const r0 = spawnSync("cmd", ["/c", "netstat -ano | findstr :" + p], { encoding: "utf8" });
    if (!r0.stdout || !r0.stdout.trim()) { cdpPort = p; break; }
  }
  if (!cdpPort) { console.log("[gate] 9411-9460 全被占用，无法起 CDP"); process.exitCode = 3; }
  const probeArgs = [PROBE, "--base", BASE, "--token", TOKEN, "--out", OUT, "--port", String(cdpPort)].concat(SELFTEST_FLAG);
  const r = spawnSync(process.execPath, probeArgs, { encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
  const out = (r.stdout || "") + (r.stderr || "");
  process.stdout.write(out);
  const rc = r.status === null ? 3 : r.status;
  let report = null;
  const rp = path.join(OUT, "cdp-report.json");
  if (existsSync(rp)) { try { report = JSON.parse(readFileSync(rp, "utf8")); } catch (e) {} }
  const results = (report && report.results) || [];
  const ok = results.filter((x) => x.status === "ok").length;
  const fail = results.filter((x) => x.status === "fail");
  const unver = results.filter((x) => x.status === "unverified");
  // 空结果集 / 期望用例数不符 ⇒ 门禁自身没测成（探针崩溃、报告缺字段、跑漏用例），
  // 绝不能因为「没有 fail」就判 PASS —— 那正是本任务要修的假绿，只不过发生在门禁层。

  const emptyish = results.length === 0 || results.length < EXPECTED_CASES;
  let verdict = emptyish ? "UNVERIFIED" : fail.length ? "FAIL" : unver.length ? "UNVERIFIED" : "PASS";
  // 反证模式（--selftest-expect-fail）：探针会故意篡改读数，正确的行为是门禁判 FAIL。
  // 若篡改后仍全绿，说明判据层是钝的 —— 那次门禁本身判 FAIL。
  if (EXPECT_FAIL) {
    verdict = fail.length ? "SELFTEST-PASS（判据层可证伪）" : "SELFTEST-FAIL（判据层钝了：篡改后仍全绿）";
  }
  let rcFinal = emptyish ? 3 : (fail.length ? 2 : unver.length ? 3 : 0);
  // 反证模式的退出码语义反过来：判据层成功证伪（fail>0）才算这次反证通过（rc=0）。
  if (EXPECT_FAIL) { rcFinal = fail.length ? 0 : 2; }
  const rcUsed = rcFinal;
  const lines = [
    "# 前端门禁回执 " + new Date().toISOString(),
    "",
    "- 目标: " + BASE,
    "- 判定: **" + verdict + "** ｜ ok=" + ok + " fail=" + fail.length + " unverified=" + unver.length + " ｜ 用例数=" + results.length + "/" + EXPECTED_CASES + " ｜ 退出码=" + rcUsed + (emptyish ? "（门禁自判：结果集不完整，按没测成处理）" : ""),
    "- 证据: " + rp,
    "",
  ];
  if (fail.length) {
    lines.push("## 断言不符（被测对象的问题）", "");
    for (const f of fail) lines.push("- `" + f.name + "` ｜ contract=" + f.contract + " ｜ " + (f.failures || []).join("；"));
    lines.push("");
  }
  if (unver.length) {
    lines.push("## 未测成（通道问题，不算通过也不算失败）", "");
    for (const u of unver) lines.push("- `" + u.name + "` ｜ " + u.error);
    lines.push("");
  }
  console.log("[gate] 判定=" + verdict + " ok=" + ok + " fail=" + fail.length + " unverified=" + unver.length);
  if (MD) { writeFileSync(MD, lines.join("\n"), "utf8"); console.log("[gate] 回执已写入 " + MD); }
  process.exitCode = rcUsed;
} catch (e) {
  console.log("[gate] UNVERIFIED（门禁自身没跑成，绝不当 PASS）: " + String(e).slice(0, 400));
  process.exitCode = 3;
} finally {
  if (child) { killTree(child.pid); console.log("[gate] 隔离实例已清理 pid=" + child.pid); }
}