# -*- coding: utf-8 -*-
"""score_matrix_batchA.py - batch-A（会话/浏览器/办公文档面）五维评分矩阵。

复用同一套打分规则（与 scripts/score_matrix.py 逐字一致），只换输入：
  候选面来自 data/gh-search-candidates.json（GitHub search 实测）+ 手工确认的官方/同源件
  入口件字节来自 data/batchA-path-verify*.json（raw.githubusercontent 200 实测）
  本地字节来自 data/inventory.json
"""
import json, datetime, os
BASE = "D:/workdesktop/docs/skill-audit"
NOW = datetime.date(2026, 10, 20)

def band(v, bands, default=None):
    if v is None: return default
    for lo, sc in bands:
        if v >= lo: return sc
    return default
inv = json.load(open(BASE + "/data/inventory.json", encoding="utf-8"))
local_bytes = {i["name"]: i.get("skill_md_bytes") for i in inv["items"] if i.get("has_skill_md")}
ver = {}
for f in ["data/batchA-path-verify.json", "data/batchA-path-verify2.json", "data/batchA-path-verify3.json"]:
    p = os.path.join(BASE, f)
    if not os.path.isfile(p): continue
    d = json.load(open(p, encoding="utf-8"))
    checked = d.get("checked", []) if isinstance(d, dict) else d
    for c in checked:
        if c.get("http") == 200 and c.get("bytes"):
            ver[(c["repo"], c["path"])] = c["bytes"]

def sc(local, repo, path, stars, lic, last, reason, conf):
    lb = local_bytes.get(local)
    ub = ver.get((repo, path))
    ratio = round(ub / lb, 3) if (ub and lb) else None
    F = None
    if ratio is not None:
        F = max(1, min(5, 3 + (2 if ratio >= 1.5 else 1 if ratio >= 1.0 else -1 if ratio < 0.6 else 0)))
    E = band(ub, [(65536, 1), (32768, 2), (16384, 3), (8192, 4), (0, 5)])
    L = (lic or "").upper()
    C = max(1, min(5, 3 + (1 if L in ("MIT", "APACHE-2.0", "BSD-3-CLAUSE", "ISC") else (-1 if (L in ("CC-BY-SA-4.0", "AGPL-3.0", "GPL-3.0", "NOASSERTION") or not L) else 0))))
    D = band(ub, [(15360, 5), (8192, 4), (3072, 3), (0, 2)])
    S = band(stars, [(100000, 5), (10000, 4), (1000, 3), (100, 2), (0, 1)])
    age = None
    if last:
        try: age = (NOW - datetime.date.fromisoformat(str(last)[:10])).days
        except ValueError: age = None
    if S is not None and age is not None and age > 180:
        S = max(1, S - 1)
    vals = [v for v in (F, E, C, D, S) if v is not None]
    return {"repo": repo, "evidence": path, "stars": stars, "license": lic, "last_activity": last,
            "match_reason": reason, "confidence": conf, "ubytes": ub, "ratio_up_over_local": ratio,
            "F": F, "E": E, "C": C, "D": D, "S": S, "total_measured": (sum(vals) if vals else None), "dims_measured": len(vals)}

SKILLS = [
  ("A-memory-start", [
    sc("A-memory-start", "anthropics/skills", "skills/claude-api/SKILL.md", 182156, None, "2026-10-09", "官方 API 技能：只覆盖 API 用法，不覆盖本仓记忆门禁；属相邻非等价", "medium"),
    sc("A-memory-start", "yonatangross/orchestkit", "plugins/ork/skills/memory-fabric/SKILL.md", 296, "MIT", "2026-10-19", "memory-fabric 提供跨会话记忆编排；可借鉴其分层结构，但无本仓门禁契约", "medium"),
    sc("A-memory-start", "yonatangross/orchestkit", "plugins/ork/skills/memory/SKILL.md", 296, "MIT", "2026-10-19", "memory 技能提供记忆写入/召回约定，体量小，属补丁级参考", "low"),
  ]),
  ("A-skill-manager", [
    sc("A-skill-manager", "obra/superpowers", "skills/writing-skills/SKILL.md", 301816, "MIT", "2026-10-10", "上游 writing-skills 定义技能创作规范；本仓 A-skill-manager 是更高层（增删改/同步/漂移），可吸收其规范节", "high"),
    sc("A-skill-manager", "anthropics/skills", "skills/skill-creator/SKILL.md", 182156, None, "2026-10-09", "官方 skill-creator 给技能脚手架与校验流程；补足本仓缺的生成侧规范", "high"),
    sc("A-skill-manager", "yonatangross/orchestkit", "plugins/ork/skills/quality-gates/SKILL.md", 296, "MIT", "2026-10-19", "quality-gates 定义技能/交付门禁清单，可对照本仓门禁完整性", "medium"),
  ]),
  ("A-dynamic-workflow", [
    sc("A-dynamic-workflow", "yonatangross/orchestkit", "plugins/ork/skills/agent-orchestration/SKILL.md", 296, "MIT", "2026-10-19", "多 agent 编排技能；本仓该技能主攻本机并发事故护栏，非等价但可吸收编排节", "medium"),
    sc("A-dynamic-workflow", "wshobson/agents", "plugins/agent-teams/skills/task-coordination-strategies/SKILL.md", 41107, "MIT", "2026-10-19", "任务协调策略：角色分工与依赖编排，可对照本仓 guards 节", "medium"),
    sc("A-dynamic-workflow", "wshobson/agents", "plugins/agent-teams/skills/multi-reviewer-patterns/SKILL.md", 41107, "MIT", "2026-10-19", "多评审者并行模式：补本仓缺的「并行评审」编排范式", "medium"),
  ]),
  ("agent-browser", [
    sc("agent-browser", "vercel-labs/agent-browser", "skills/agent-browser/SKILL.md", 44260, None, "2026-10-19", "同源上游：CLI 极简入口（3457B）；本仓 29313B 为自建加厚版，需判定拆分而非替换", "high"),
    sc("agent-browser", "browser-use/browser-use", "skills/qa/SKILL.md", 118543, None, "2026-10-17", "高层 QA 编排；与本仓浏览器 CLI 定位不同，属互补", "medium"),
    sc("agent-browser", "microsoft/playwright-mcp", "README.md", 38379, None, "2026-10-19", "Playwright 官方 MCP：提供稳定浏览器协议层；本仓若走 MCP 可减少自维护代码", "medium"),
  ]),
  ("browser-cdp", [
    sc("browser-cdp", "microsoft/playwright-mcp", "README.md", 38379, None, "2026-10-19", "官方 MCP 已覆盖 CDP 会话复用，可作为 browser-cdp 的替代路径", "medium"),
    sc("browser-cdp", "Colgate13/claude-cdp-debugger", "README.md", 2, "MIT", "2026-05-12", "CDP 调试器技能；活跃度不足，仅作写法参照", "low"),
    sc("browser-cdp", "vercel-labs/agent-browser", "skills/agent-browser/SKILL.md", 44260, None, "2026-10-19", "上游 CLI 内含登录态复用与截图，可替代本仓部分 CDP 手工步骤", "medium"),
  ]),
  ("electron", [
    sc("electron", "vercel-labs/agent-browser", "skill-data/electron/SKILL.md", 44260, None, "2026-10-19", "与本地字节完全一致（6727B，SHA256 相同）——已是上游最新拷贝", "high"),
    sc("electron", "microsoft/playwright-mcp", "README.md", 38379, None, "2026-10-19", "官方 Electron 支持路径，可作长期替代", "medium"),
    sc("electron", "vercel-labs/agent-browser", "skill-data/dogfood/SKILL.md", 44260, None, "2026-10-19", "上游 dogfood 件（10845B vs 本地 10428B）——本地 dogfood 落后 417B，可吸收", "high"),
  ]),
  ("dogfood", [
    sc("dogfood", "vercel-labs/agent-browser", "skill-data/dogfood/SKILL.md", 44260, None, "2026-10-19", "同源上游：上游 10845B vs 本地 10428B，**已产生 417B 差异**，需逐节对表吸收", "high"),
    sc("dogfood", "browser-use/browser-use", "skills/qa/SKILL.md", 118543, None, "2026-10-17", "高层 QA 流程；与本仓探索式走查定位接近，可作流程层替代", "medium"),
  ]),
  ("docx", [
    sc("docx", "anthropics/skills", "skills/docx/SKILL.md", 182156, None, "2026-10-09", "官方同源：上游 6911B vs 本地 28922B（本地 4.19 倍）——需判定本地加厚是否仍覆盖官方能力", "high"),
    sc("docx", "tfriedel/claude-office-skills", "SKILL.md", 839, None, "2026-04-01", "第三方 Office 技能包（PPTX/DOCX/XLSX/PDF）；活跃度偏低", "low"),
  ]),
  # 注：曾计划登记 "slides"，但实测 D:/global_skills/slides 目录不存在 → 已删除该条目（防虚构技能）。
  ("html-ppt", [
    sc("html-ppt", "op7418/guizang-ppt-skill", "SKILL.md", 28244, "AGPL-3.0", "2026-08-07", "高星 HTML PPT 技能（43125B）；**AGPL-3.0 需评估传染性**", "high"),
    sc("html-ppt", "Akxan/ppt-agent-skill", "SKILL.md", 163, "MIT", "2026-05-11", "PPT 生成流程件；可作替代但体量与活跃度弱于本仓", "medium"),
    sc("html-ppt", "anthropics/skills", "skills/theme-factory/SKILL.md", 182156, None, "2026-10-09", "官方主题工厂：本地已字节一致，属「已同步」证明而非替代", "high"),
  ]),
  ("computer-use-guidance-windows", [
    sc("computer-use-guidance-windows", "skalesapp/skales", "README.md", 2200, "NOASSERTION", "2026-10-19", "活跃的 computer-use 项目（42998B README）；许可未声明，需评估", "medium"),
  ]),
  ("A-project-handoff", [
    sc("A-project-handoff", "yonatangross/orchestkit", "plugins/ork/skills/memory-fabric/SKILL.md", 296, "MIT", "2026-10-19", "跨会话记忆编排；可对照本仓 handoff 的分卷/指针做法", "medium"),
    sc("A-project-handoff", "yonatangross/orchestkit", "plugins/ork/skills/memory/SKILL.md", 296, "MIT", "2026-10-19", "记忆写入约定；补本仓缺的召回侧协议", "low"),
  ]),
  ("testing", [
    sc("testing", "addyosmani/agent-skills", "skills/browser-testing-with-devtools/SKILL.md", 108476, "MIT", "2026-10-13", "浏览器 DevTools 测试流程（14539B）：本仓测试面缺的端到端浏览器验证", "high"),
    sc("testing", "yonatangross/orchestkit", "plugins/ork/skills/browser-tools/SKILL.md", 296, "MIT", "2026-10-19", "浏览器工具集（24173B）：可替代本仓手写探针", "medium"),
  ]),
  ("ci-pipeline-debugging", [
    sc("ci-pipeline-debugging", "addyosmani/agent-skills", "skills/ci-cd-and-automation/SKILL.md", 108476, "MIT", "2026-10-13", "CI/CD 与自动化（11332B）：补本仓缺的平台侧命令与工作流写法", "high"),
  ]),
]

OUT = {"schema": "skill-score-matrix-v1", "batch": "batch-A", "generated_at": datetime.datetime.now().isoformat(timespec="seconds"),
       "rules": {"F": "入口件/本地字节比：3+(>=1.5:+2/>=1.0:+1/<0.6:-1)", "E": "入口件字节：<=8K:5 <=16K:4 <=32K:3 <=64K:2 else 1",
                 "C": "格式兼容 3；宽松许可+1；CC-BY-SA/GPL/无声明-1", "D": "入口件字节：>=15K:5 >=8K:4 >=3K:3 else 2",
                 "S": "stars：>=100k:5 >=10k:4 >=1k:3 >=100:2 else 1；>180天未活跃 -1；未取证记 null"},
       "baseline_date": NOW.isoformat(), "skills": []}
for local, cands in SKILLS:
    cands.sort(key=lambda x: (-(x.get("total_measured") or 0), -(x.get("stars") or 0)))
    OUT["skills"].append({"local_skill": local, "local_bytes": local_bytes.get(local), "candidate_count": len(cands),
                          "best": ({"repo": cands[0]["repo"], "total": cands[0]["total_measured"]} if cands else None),
                          "candidates": cands})
json.dump(OUT, open(BASE + "/data/score-matrix-batchA.json", "w", encoding="utf-8"), ensure_ascii=False, indent=2)
print("skills=%d candidates=%d" % (len(OUT["skills"]), sum(s["candidate_count"] for s in OUT["skills"])))
for s in OUT["skills"]:
    print("%-32s cand=%d best=%s" % (s["local_skill"], s["candidate_count"], json.dumps(s["best"], ensure_ascii=False)))