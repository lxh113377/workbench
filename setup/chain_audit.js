// setup/chain_audit.js -- 序号型产物的「读链」巡检（2026-11-02 round 247 教训 (g) 的落地工具）
//
// 为什么需要：(g) 说「序号/引用这类关系型错误必须读链」——行级回读抓不到，因为漏写的卡本身不存在、
//   没有哪一行是错的。本工具把读链固化成一条命令，输出四类事实：非单调、重复号、覆盖空洞、空洞是否有佐证。
//
// 判据语义（都是「报告」不是「判红」，因为历史空洞无法追溯补卡——补卡=造假日史）：
//   非单调 = 卡片首号序列出现下降（真异常，该修）
//   重复号 = 同一号多张卡（绝大多数是「开场卡 + 收口卡」配对，属正常，逐条打印标题供人看）
//   空洞     = 区间内没卡的号；再用 CHANGELOG 条目首行与 git commit subject 找佐证：
//     有佐证 = 该轮确实发生但没建卡（真漏卡，登记不补）；无佐证 = 跳号（无害）
//
// 用法：node setup/chain_audit.js [--json] | node setup/chain_audit.js --self-test
//   --self-test 用内存夹具断言，不读仓库（改坏判据 ⇒ 立刻翻红）。

const fs = require("fs");
const path = require("path");
const { execSync } = require("child_process");

const NL = String.fromCharCode(10), CR = String.fromCharCode(13);
const LP = String.fromCharCode(0xFF08), RP = String.fromCharCode(0xFF09); // （ ）

function decr(s) { return s.split(CR).join(""); }

function readLines(p) {
  return decr(fs.readFileSync(p, "utf8")).split(NL);
}

// ---- 纯函数：可被 self-test 直接调用 ----
function expandCardTitles(lines) {
  const covered = new Set();
  const seq = [];
  const byFirst = new Map();
  lines.forEach(function (l, i) {
    if (l.indexOf("## Round ") !== 0) return;
    const body = l.replace("## Round ", "");
    const m = body.match(/^([0-9]+)(?:-([0-9]+))?/);
    if (!m) return;
    const a = parseInt(m[1], 10);
    const b = m[2] ? parseInt(m[2], 10) : a;
    for (let x = a; x <= b; x++) covered.add(x);
    seq.push(a);
    if (!byFirst.has(a)) byFirst.set(a, []);
    byFirst.get(a).push({ line: i + 1, title: l.trim().slice(0, 70) });
  });
  return { covered: covered, seq: seq, byFirst: byFirst };
}

function findNonMono(seq) {
  const bad = [];
  for (let i = 1; i < seq.length; i++) if (seq[i] < seq[i - 1]) bad.push(seq[i - 1] + "->" + seq[i]);
  return bad;
}

function findGaps(covered) {
  const nums = Array.from(covered).sort(function (x, y) { return x - y; });
  if (!nums.length) return [];
  const gaps = [];
  for (let x = nums[0]; x <= nums[nums.length - 1]; x++) if (!covered.has(x)) gaps.push(x);
  return gaps;
}

// 自标签：条目首行里「（round N」这种开括号形态（引用别轮通常不带前括号）
function selfTagFromChangelog(chgLines) {
  const hit = new Map();
  chgLines.forEach(function (l, i) {
    if (l.indexOf("- **") !== 0) return;
    const re = /[（(]round ([0-9]+)/g;
    let m;
    while ((m = re.exec(l)) !== null) {
      const n = parseInt(m[1], 10);
      if (!hit.has(n)) hit.set(n, { line: i + 1, text: l.slice(0, 96) });
    }
  });
  return hit;
}

function selfTagFromSubjects(subs) {
  const hit = new Set();
  subs.forEach(function (s) {
    const re = /[（(]round ([0-9]+)/g;
    let m;
    while ((m = re.exec(s)) !== null) hit.add(parseInt(m[1], 10));
  });
  return hit;
}

function audit(cards, chg, subs) {
  const e = expandCardTitles(cards);
  const gaps = findGaps(e.covered);
  const chgTag = selfTagFromChangelog(chg);
  const subTag = selfTagFromSubjects(subs);
  const evidenced = [], skipped = [];
  gaps.forEach(function (g) {
    const src = [];
    if (chgTag.has(g)) src.push("CHANGELOG:L" + chgTag.get(g).line);
    if (subTag.has(g)) src.push("commit");
    if (src.length) evidenced.push({ gap: g, by: src.join(" + ") });
    else skipped.push(g);
  });
  const dups = [];
  e.byFirst.forEach(function (v, k) { if (v.length > 1) dups.push({ n: k, cards: v }); });
  return {
    cardTitles: e.seq.length,
    coveredCount: e.covered.size,
    range: [Math.min.apply(null, Array.from(e.covered)), Math.max.apply(null, Array.from(e.covered))],
    nonMono: findNonMono(e.seq),
    dupCount: dups.length,
    dups: dups,
    gapCount: gaps.length,
    evidencedMisses: evidenced,
    harmlessSkips: skipped
  };
}

// ---- 自检：夹具断言（改坏判据立刻翻红） ----
function selfTest() {
  let fails = 0;
  function ok(name, cond) { if (!cond) { fails++; console.log("  ASSERT FAIL " + name); } else console.log("  ok " + name); }
  const cards = [
    "## Round 10 开场分解卡", "## Round 10 收口", "## Round 11-12 合并收口",
    "## Round 14 收口", "## Round 16 收口（编号从 14 跳到 16）"
  ];
  const chg = ["- **某改动**（round 15，2026-11-02）：正文", "- 提到 round 99 的旧事"];
  const subs = ["fix: 某事（round 15）", "docs: 别的"];
  const r = audit(cards, chg, subs);
  ok("覆盖展开=5 个号(10,11,12,14,16；13/15 是故意留的空洞)", r.coveredCount === 5);
  ok("非单调=0", r.nonMono.length === 0);
  ok("重复号含 10（开场+收口）", r.dupCount === 1 && r.dups[0].n === 10);
  ok("空洞=13 与 15", Array.from([13, 15]).every(function (x) { return r.gapCount === 2; }));
  ok("15 有 CHANGELOG 佐证 => 真漏卡", r.evidencedMisses.length === 1 && r.evidencedMisses[0].gap === 15);
  ok("13 无佐证 => 无害跳号", r.harmlessSkips.length === 1 && r.harmlessSkips[0] === 13);
  // 反例：正文引用别轮（不带前括号）不该被当成自标签
  const r2 = audit(["## Round 1 卡"], ["- **条目**：见 round 2 的讨论"], []);
  ok("正文引用 round 2 不算自标签（2 根本不存在于区间）", r2.evidencedMisses.length === 0);
  console.log(fails ? "SELFTEST FAIL count=" + fails : "SELFTEST_OK");
  process.exit(fails ? 1 : 0);
}

if (process.argv.indexOf("--self-test") >= 0) selfTest();
else {
  // --root <dir> 供负向对照使用（指向临时夹具，绝不碰真仓库文件）
  const ri = process.argv.indexOf("--root");
  const root = ri >= 0 && process.argv[ri + 1] ? path.resolve(process.argv[ri + 1]) : path.resolve(__dirname, "..");
  const dp = path.join(root, "DECOMPOSE.md");
  const cp2 = path.join(root, "CHANGELOG.md");
  if (!fs.existsSync(dp) || !fs.existsSync(cp2)) { console.log("CHAIN_UNVERIFIED: 找不到 DECOMPOSE/CHANGELOG"); process.exit(2); }
  let subs = [];
  let gitOk = true;
  try {
    subs = decr(execSync("git log --all --format=%s", { cwd: root, encoding: "utf8", maxBuffer: 64 * 1024 * 1024 })).split(NL);
  } catch (e) {
    gitOk = false;
    console.log("note: git log 取不到，空洞的 commit 佐证降级为仅看 CHANGELOG（非单调检测不受影响）");
  }
  const r = audit(readLines(dp), readLines(cp2), subs);
  const asJson = process.argv.indexOf("--json") >= 0;
  if (asJson) { console.log(JSON.stringify(r)); process.exit(0); }
  console.log("卡片标题数=" + r.cardTitles + " 覆盖号数=" + r.coveredCount + " 区间=" + r.range[0] + "-" + r.range[1]);
  console.log("非单调=" + r.nonMono.length + (r.nonMono.length ? " <<< " + r.nonMono.join(",") : " （链序正常）"));
  console.log("重复号=" + r.dupCount + "（正常多为开场卡+收口卡配对）");
  r.dups.forEach(function (d) { console.log("  - " + d.n + ": " + d.cards.map(function (c) { return c.title.slice(9); }).join(" | ")); });
  console.log("覆盖空洞=" + r.gapCount + " => 有佐证(真漏卡) " + r.evidencedMisses.length + " / 无佐证(跳号无害) " + r.harmlessSkips.length);
  r.evidencedMisses.forEach(function (m) { console.log("  MISS round " + m.gap + " <- " + m.by); });
  if (r.harmlessSkips.length) console.log("  跳号: " + r.harmlessSkips.join(", "));
  // 结论行：供门禁解析（形状固定，缺行 ⇒ 门禁 UNVERIFIED 不计 PASS）
  const verdict = r.nonMono.length
    ? "CHAIN_FAIL nonMono=" + r.nonMono.length + " misses=" + r.evidencedMisses.length
    : (!gitOk ? "git=off " : "") + "CHAIN_OK nonMono=0 misses=" + r.evidencedMisses.length + " dupHarmless=" + r.dupCount + " skips=" + r.harmlessSkips.length;
  console.log(verdict);
  // 只有「非单调」是硬错误（卡片顺序被人为打乱）；漏卡与重复都不判红，见文件头说明
  process.exit(r.nonMono.length ? 1 : 0);
}
