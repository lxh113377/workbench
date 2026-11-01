const fs = require("fs");
const src = fs.readFileSync("D:/workdesktop/workbench/internal/templates/defaults.go", "utf8").split("\n");
const fns = [];
for (let i = 0; i < src.length; i++) if (/^func quality\w+Tpl\(/.test(src[i])) fns.push({ name: src[i].replace(/^func (\w+)\(.*/, "$1"), start: i });
const CIRC = "①②③④⑤⑥⑦⑧⑨⑩⑪⑫⑬⑭⑮⑯⑰⑱⑲⑳㉑㉒㉓";
const sets = {};
for (let k = 0; k < fns.length; k++) {
  const e = k + 1 < fns.length ? fns[k + 1].start : src.length;
  const s = new Set();
  for (let i = fns[k].start; i < e; i++) {
    const t = (src[i] || "").trim();
    if (!t.startsWith("\"")) continue;
    for (const c of CIRC) if (t.includes("\"" + c + " ") || t.includes("\"" + c + " **")) s.add(c);
  }
  sets[fns[k].name] = s;
}
const MAP = { "qualityAuditTpl": "quality-audit", "qualityVerifyTpl": "quality-verify", "qualityGateSpecTpl": "quality-gate-spec", "qualityHygieneTpl": "quality-hygiene" };
const bad = [];
for (let k = 0; k < fns.length; k++) {
  const e = k + 1 < fns.length ? fns[k + 1].start : src.length;
  for (let i = fns[k].start; i < e; i++) {
    const t = (src[i] || "").trim();
    const m = t.match(/`(quality-[a-z-]+|review-five-axis)`\s*[的]?\s*(.)/);
    if (!m) continue;
    const target = m[1], circ = m[2];
    if (!CIRC.includes(circ)) continue;
    const fnName = Object.keys(MAP).find(k2 => MAP[k2] === target);
    const has = fnName && sets[fnName] && sets[fnName].has(circ);
    if (!has) bad.push({ line: i + 1, from: MAP[fns[k].name] || fns[k].name, to: target, circ });
  }
}
console.log("XREF_OK dangling=" + bad.length + " templates=" + Object.keys(sets).length);
if (bad.length) for (const b of bad) console.log("  - " + b.line + " " + b.from + " -> " + b.to + " " + b.circ);
