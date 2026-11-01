// setup/long-func-scan.js — 长函数精确统计（大括号配对口径，2026-11-01 round 192）。
//
// 为什么要有这个文件：五轴报告 P1-1 需要一条「可复跑命令」作为证据。历史 awk 版按
// 「行号差 + 只认顶格 }」累计，实测对多行签名/嵌套块算错（本轮实测多出一个假阳性
// types.go:88=195，该函数真实 <180 行）；而把长 node -e 内联进 markdown 又极易在抄写中
// 断行残缺（本轮就产出了一条断的，自查「报告里的可复跑命令是否真能跑」时抓到——
// 这正是模板反虚构条款点名的失效形态，这次是「命令本体残缺」而非「文件不存在」）。
// 落盘成受版本控制的脚本，报告只引用一行命令 ⇒ 证据可跑、可 diff、可被门禁生态复用。
//
// 用法: node setup/long-func-scan.js [阈值行数，默认 180]   （在仓库根跑）
// 输出口径: 函数起行到配平闭括号的行数（含两端），> 阈值才报；排除 _test.go。
const fs = require("fs");
const cp = require("child_process");
const TH = Number(process.argv[2] || 180);
const files = cp.execSync('git ls-files "workbench/**/*.go"', { encoding: "utf8" })
  .replace(/\r/g, "").trim().split("\n").filter(f => f && !f.endsWith("_test.go"));
const hits = [];
for (const rel of files) {
  const L = fs.readFileSync(rel, "utf8").replace(/\r/g, "").split("\n");
  for (let i = 0; i < L.length; i++) {
    const m = L[i].match(/^func (\([^)]*\) )?([A-Za-z_][A-Za-z0-9_]*)\(/);
    if (!m) continue;
    let depth = 0, started = false, end = i;
    for (let j = i; j < L.length; j++) {
      for (const ch of L[j]) { if (ch === "{") { depth++; started = true; } else if (ch === "}") depth--; }
      if (started && depth === 0) { end = j; break; }
    }
    const len = end - i + 1;
    if (len > TH) hits.push({ name: m[2], file: rel, start: i + 1, len });
  }
}
hits.sort((a, b) => b.len - a.len);
for (const h of hits) console.log(`${h.name} @ ${h.file}:${h.start} = ${h.len} 行`);
console.log(`> ${TH} 行共 ${hits.length} 处（口径：大括号配对，tracked workbench 非测试 .go）`);
