import datetime, json, re, time, urllib.request
UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36"
BASE = "D:/workdesktop/docs/skill-audit"
import os
MATRIX_FILES = ["data/score-matrix.json", "data/score-matrix-batchA.json"]
TODAY = datetime.date.today().isoformat()
def http_json(url, timeout=25, tries=3):
    last = None
    for i in range(tries):
        try:
            req = urllib.request.Request(url, headers={"User-Agent": UA})
            with urllib.request.urlopen(req, timeout=timeout) as r:
                return json.loads(r.read().decode("utf-8", "replace"))
        except Exception as e:
            last = repr(e); time.sleep(1.2 * (i + 1))
    return {"__error": last}
def atom_tag(repo):
    for _ in range(3):
        try:
            req = urllib.request.Request("https://github.com/" + repo + "/releases.atom", headers={"User-Agent": UA})
            with urllib.request.urlopen(req, timeout=20) as r:
                xml = r.read().decode("utf-8", "replace")
            m = re.search(r"<entry>.*?<title>(.*?)</title>", xml, re.S)
            if m:
                return m.group(1).strip()
            return None
        except Exception:
            time.sleep(1.0)
    return None
m = {"skills": []}
for _f in MATRIX_FILES:
    _p = os.path.join(BASE, _f)
    if os.path.isfile(_p):
        _m = json.load(open(_p, encoding="utf-8"))
        print("merge", _f, len(_m.get("skills", [])))
        m["skills"].extend(_m.get("skills", []))
DISCOVERY = {
    "code-review": "code review skill agent claude",
    "debugging-fixing": "systematic debugging skill agent",
    "testing": "testing skill agent property based mutation coverage",
    "test-driven-development": "test driven development skill agent",
    "refactoring": "refactoring code smells skill agent",
    "security-audit": "security audit skill agent owasp",
    "ci-pipeline-debugging": "ci pipeline debugging skill agent github actions",
    "writing-plans": "writing plans skill agent implementation plan",
    "brainstorming": "brainstorming skill agent requirements interview",
    "verification-before-completion": "verification before completion skill agent evidence",
    "executing-plans": "executing plans skill agent task tracking subagent",
    "spike": "technical spike skill agent prototype",
    "grill-me": "grill me skill agent design interview",
    "first-principles-decomposer": "first principles reasoning skill agent",
    "scope-creep-detector": "scope creep detector skill agent diff",
    "commit-archaeologist": "git history archaeology skill agent",
    "audit-runner-safe-aggregate": "audit runner aggregate pass fail gate skill",
}
withtag, notag, tags = [], [], {}
entries = []
SEEN = set()
for s in m["skills"]:
    local = s["local_skill"]
    if local in SEEN:
        continue
    SEEN.add(local)
    scored = [c for c in s["candidates"] if c.get("ubytes")]
    # 血缘优先（2026-10-20 轮10 教训自动闭环）：若本地 SKILL.md 自述 Provenance 指向某仓，代表上游必须选该仓，不被分数压制
    _prov = set()
    _sk = 'D:/global_skills/' + s['local_skill'] + '/SKILL.md'
    if os.path.isfile(_sk):
        try:
            _txt = open(_sk, encoding='utf-8', errors='replace').read(16384)
            if 'Provenance' in _txt or '吸收上游' in _txt or '受控吸收' in _txt:
                import re as _re
                for _m in _re.finditer(r'([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)', _txt):
                    _prov.add(_m.group(1))
        except OSError:
            pass
    _lin = [c for c in scored if c.get('repo') in _prov]
    rep = (_lin or scored or [None])[0]
    repo = rep["repo"] if rep else None
    tag = atom_tag(repo) if repo else None
    rec = {"repo": repo, "entry_path": (rep["evidence"] if rep else None), "stars": (rep["stars"] if rep else None),
           "license": (rep["license"] if rep else None), "score_total": (rep["total_measured"] if rep else None)}
    entries.append({"local_skill": local,
                    "local_skill_md": "D:/global_skills/" + local + "/SKILL.md",
                    "upstream_repo": repo,
                    "upstream_path": rec["entry_path"],
                    "ledger": [rec],
                    "recorded_version": tag,
                    "last_checked": TODAY,
                    "discovery_query": DISCOVERY.get(local, local.replace("-", " ") + " skill agent"),
                    "candidates": [{"repo": c["repo"], "entry_path": c["evidence"], "stars": c["stars"],
                                   "license": c["license"], "score_total": c["total_measured"]} for c in s["candidates"][:5]],
                    "notes": s.get("remote_note")})
    if tag:
        withtag.append(local); tags[local] = tag
    else:
        notag.append(local)
reg = {"schema": "skill-watchlist-v1",
       "_note": "技能上游/替代季度巡检登记表。upstream_* = 该技能评分最高的已实测候选（代表上游）；recorded_version = 本次对表基线（github releases.atom 实测）；ledger = 其余候选的即时读数快照；candidates = 季度复查对象。",
       "_policy": "上游不自动覆盖：扫描出 RED -> 人工裁决（吸收/保持分叉/弃用/替换）-> 再动手。",
       "_updated": TODAY,
       "fuse_days": 100,
       "active_days": 400,
       "watchlist": entries}
open(BASE + "/data/skill-watchlist.json", "w", encoding="utf-8").write(json.dumps(reg, ensure_ascii=False, indent=2))
print("entries=%d with_tag=%d no_tag=%d" % (len(entries), len(withtag), len(notag)))
# 聚类告警（轮10 升级建议自动闭环）：同上游仓被 ≥2 技能共享时提示按仓巡检，防整族漏登（obsidian 族/vercel-labs 族实证）
_cluster = {}
for e in entries:
    if e["upstream_repo"]:
        _cluster.setdefault(e["upstream_repo"], []).append(e["local_skill"])
for _rp, _ls in sorted(_cluster.items()):
    if len(_ls) >= 2:
        print("CLUSTER", _rp, "<-", ", ".join(_ls))
print("NO_TAG:", ", ".join(notag))