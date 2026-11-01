# 待并入 patch：newconv_ref_gate.py 的 SELF_FACE 缺 workdesktop 项目面

## 现象（2026-11-02 实测）
- `python newconv_ref_gate.py --paths D:/workdesktop/AGENTS.md ...` ⇒ 全报 `ORPHAN`；`[GATE:ref-fail]`。
- 但实测腿 `newconv_probe.py --channel cli --path D:/workdesktop/AGENTS.md --token f4a19b02 --repeat 2` ⇒
  `found=1 / hits=2 / tools=0 / PROBE_RC=0`，答案逐字 `CANARY=f4a19b02`。
  ⇒ **静态腿假红**：它判定不可引用，实测新会话确实引用得到。

## 根因
`newconv_ref_gate.py` 的 `SELF_FACE`（L38-43）硬编码两张注入面：
  1. `C:/Users/37533/.dsh/AGENTS.md`（global 壳）
  2. `C:/Users/37533/Desktop/workspace/焚诀/AGENTS.md`（**焚诀**项目根，surface_id=fenjue-project-agents）
机制注释自己写「项目根 AGENTS.md 被 stat 注入」，但只登记了焚诀那一个项目根。
`D:/workdesktop/AGENTS.md` 同样被宿主 stat 注入（本会话系统提示原文可见其内容）⇒ 它应是一个 SELF 面，
却因不在 SELF_FACE 名单里被判 ORPHAN。**这是工具口径覆盖不全，不是 workdesktop 产物不可引用。**

## 建议改法（供闸的 owner 决定，本会话不改他人 owner 文件 — D7.2）
SELF_FACE 增加一项（或把「项目根 AGENTS.md」参数化为按当前工作区解析，别写死单个项目）：
```
  {"surface_id": "workdesktop-project-agents",
   "path": "D:/workdesktop/AGENTS.md",
   "mechanism": "项目根 AGENTS.md 被 stat 注入"},
```
更彻底的方案：SELF_FACE 的项目根项由「会话工作区根 + AGENTS.md」动态得出，这样任何项目的根 AGENTS.md
都自动成面，不必逐项目登记（否则每接入一个新仓库都要补一次硬编码）。

## 本会话为何不改
`newconv_ref_gate.py` mtime 距本轮 15 分钟、`rule46*.part1.md` 距 8 分钟 ⇒ 闸的 owner 活跃编辑中，
按 D7.2 停写他人 owner 文件，先落本 patch。所审 sha 见下。

所审 sha256(前12) = f2fae8c482b6（若此值已变，说明 owner 已改，本 patch 需重新比对）

