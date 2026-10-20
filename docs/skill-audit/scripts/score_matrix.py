# -*- coding: utf-8 -*-
# V10.1 | 2026-10-20 轮10 | 生成时自动回填候选星值（watchlist 优先，无则探 ungh）+ 自动标 stars_scope 两级口径；修派生件被重生成覆盖回零的坑
"""score_matrix.py - 五维对比评估矩阵（可复算，规则全部显式）。

输入：
  raw/batch-*.json              候选（repo/stars/last_activity/license/entry_path/confidence）
  data/inventory.json           本地技能体量
  data/candidate-path-verify.json  候选入口件 HTTP 实测字节（raw.githubusercontent 200）
  data/upstream-skill-sizes.json   本地 vs 上游同名件字节对表

五维定义与打分规则（每条都可在磁盘/网络上复算，禁手改）：
  F 功能完整性 = 候选入口件字节 vs 本地 SKILL.md 字节的比值：
      首档 3 分；比值 >=1.0 +1；>=1.5 +2；<0.6 -1；无实测字节 => null（未取证，不猜）
  E 运行效率   = 候选入口件字节绝对值（越小注入越便宜）：<=8KB 5 / <=16KB 4 / <=32KB 3 / <=64KB 2 / >64KB 1
  C 兼容性     = 许可与格式：基础 3（同为 SKILL.md 目录式即格式兼容）；
      许可为 MIT/Apache/BSD/ISC 等宽松 +1；CC-BY-SA/GPL/AGPL/NOASSERTION/None 名义 -1（需署名或权属不明）
  D 文档完善度 = 入口件字节档位（有实质正文）：>=15KB 5 / >=8KB 4 / >=3KB 3 / <3KB 2；无实测 => null
  S 社区支持度 = stars 档位：>=100k 5 / >=10k 4 / >=1k 3 / >=100 2 / <100 1；
      最近活跃距今 >180 天 -1；已归档直接记 1；无 stars 实测 => null

输出：data/score-matrix.json + 03-对比评估矩阵.md
"""
import json, re, glob, datetime, os
BASE = "D:/workdesktop/docs/skill-audit"
NOW = datetime.date(2026, 10, 19)

def band(v, bands, default=None):
    if v is None:
        return default
    for lo, sc in bands:
        if v >= lo:
            return sc
    return default

def load(p, default=None):
    try:
        return json.load(open(p, encoding="utf-8"))
    except Exception:
        return default

inv = load(BASE + "/data/inventory.json")
local_bytes = {i["name"]: i.get("skill_md_bytes") for i in (inv or {}).get("items", []) if i.get("has_skill_md")}
paths = load(BASE + "/data/candidate-path-verify.json", {"checked": []})
for _f in ["batchA-path-verify.json", "batchA-path-verify2.json", "batchA-path-verify3.json"]:
    _d = load(BASE + "/data/" + _f, None)
    if isinstance(_d, dict):
        paths = {"checked": list(paths.get("checked", [])) + list(_d.get("checked", []))}
    elif isinstance(_d, list):
        paths = {"checked": list(paths.get("checked", [])) + _d}
path_bytes = {}
for c in paths.get("checked", []):
    if c.get("http") == 200 and c.get("local_skill"):
        path_bytes[(c["repo"], c["path"])] = c.get("bytes")
sizes = load(BASE + "/data/upstream-skill-sizes.json", {"pairs": []})
pair_ratio = {}
for p in sizes.get("pairs", []):
    if p.get("size_ratio_local_over_upstream") is not None:
        pair_ratio[p["name"]] = {"local": p.get("local_bytes"), "up": p.get("upstream_bytes"),
                                 "ratio": p["size_ratio_local_over_upstream"], "path": p.get("upstream_path")}

bymatch = {}
for c in paths.get("checked", []):
    if c.get("bytes") and c.get("local_skill"):
        bymatch.setdefault(c["local_skill"], []).append(c)

def score_candidate(local, c):
    lb = local_bytes.get(local)
    ubytes = c.get("ubytes")
    for ch in bymatch.get(local, []):
        if ubytes:
            break
        if ch["repo"] == c.get("repo") and ch.get("bytes"):
            ubytes = ch["bytes"]; break
    ratio = c.get("ratio_up_over_local")
    if ratio is None and ubytes and lb:
        ratio = round(ubytes / lb, 3)
    F = None
    if ratio is not None:
        F = 3 + (2 if ratio >= 1.5 else 1 if ratio >= 1.0 else -1 if ratio < 0.6 else 0)
        F = max(1, min(5, F))
    E = band(ubytes, [(65536, 1), (32768, 2), (16384, 3), (8192, 4), (0, 5)])
    lic = (c.get("license") or "").upper()
    C = 3 + (1 if lic in ("MIT", "APACHE-2.0", "BSD-3-CLAUSE", "BSD-2-CLAUSE", "ISC") else (-1 if (lic in ("CC-BY-SA-4.0", "GPL-3.0", "AGPL-3.0", "NOASSERTION") or not lic) else 0))
    C = max(1, min(5, C))
    D = band(ubytes, [(15360, 5), (8192, 4), (3072, 3), (0, 2)])
    st = c.get("stars")
    S = band(st, [(100000, 5), (10000, 4), (1000, 3), (100, 2), (0, 1)])
    act = c.get("last_activity")
    age = None
    if act:
        try:
            age = (NOW - datetime.date.fromisoformat(str(act)[:10])).days
        except ValueError:
            age = None
    if S is not None and age is not None and age > 180:
        S = max(1, S - 1)
    return {"ubytes": ubytes, "ratio_up_over_local": ratio, "F": F, "E": E, "C": C, "D": D, "S": S,
            "activity_age_days": age, "evidence": c.get("entry_path") or c.get("evidence"), "confidence": c.get("confidence")}

skills = []
for f in sorted(glob.glob(BASE + "/raw/batch-*.json")):
    d = load(f, {})
    for s in d.get("skills", []):
        local = s["local_skill"]
        cands = []
        for c in s.get("candidates", []):
            sc = score_candidate(local, c)
            vals = [sc["F"], sc["E"], sc["C"], sc["D"], sc["S"]]
            measured = [v for v in vals if v is not None]
            sc["total_measured"] = sum(measured) if measured else None
            sc["dims_measured"] = len(measured)
            sc.update({"repo": c.get("repo"), "stars": c.get("stars"), "license": c.get("license"),
                       "last_activity": c.get("last_activity"), "match_reason": c.get("match_reason"),
                       "confidence": c.get("confidence")})
            cands.append(sc)
        cands.sort(key=lambda x: (-(x.get("total_measured") or 0), -(x.get("stars") or 0)))
        best = cands[0] if cands else None
        skills.append({"local_skill": local, "local_bytes": local_bytes.get(local),
                       "local_note": (s.get("local_evidence") or {}).get("note"),
                       "remote_note": s.get("notes"), "candidate_count": len(cands),
                       "best": ({"repo": best["repo"], "total": best.get("total_measured")} if best else None),
                       "candidates": cands})
MATRIX_MERGE = ["/data/score-matrix-batchA.json", "/data/score-matrix-batchD.json", "/data/score-matrix-batchE.json"]


def _total(s):
    return (s.get("best") or {}).get("total") or 0


for _mf in MATRIX_MERGE:
    _p = BASE + _mf
    if not os.path.isfile(_p):
        continue
    _m = load(_p, {"skills": []})
    for s in _m.get("skills", []):
        _lb = local_bytes.get(s["local_skill"])
        for _c in (s.get("candidates") or []):
            _ub = _c.get("ubytes")
            _r = _c.get("ratio_up_over_local")
            if _r is None and _ub and _lb:
                _r = round(_ub / _lb, 3)
                _c["ratio_up_over_local"] = _r
            if _c.get("F") is None and _r is not None:
                _c["F"] = max(1, min(5, 3 + (2 if _r >= 1.5 else 1 if _r >= 1.0 else -1 if _r < 0.6 else 0)))
            _vals = [v for v in (_c.get("F"), _c.get("E"), _c.get("C"), _c.get("D"), _c.get("S")) if v is not None]
            _c["total_measured"] = sum(_vals) if _vals else None
            _c["dims_measured"] = len(_vals)
        _cs = s.get("candidates") or []
        _cs.sort(key=lambda x: (-(x.get("total_measured") or 0), -(x.get("stars") or 0)))
        s["candidate_count"] = len(_cs)
        s["best"] = ({"repo": _cs[0]["repo"], "total": _cs[0]["total_measured"]} if _cs else None)
        s["local_bytes"] = _lb
        _idx = None
        for _i, _x in enumerate(skills):
            if _x["local_skill"] == s["local_skill"]:
                _idx = _i
                break
        if _idx is None:
            print("merge ++", s["local_skill"], _total(s))
            skills.append(s)
        elif _total(s) > _total(skills[_idx]):
            print("merge *", s["local_skill"], _total(skills[_idx]), "->", _total(s))
            skills[_idx] = s
        else:
            print("merge =", s["local_skill"], "保持原值", _total(skills[_idx]))

# 轮11 断根：以 watchlist 为唯一准入口径过滤——raw/batch-*.json 与 MATRIX_MERGE 是历史候选源，
# 可能含已注销条目（executing-plans 第三次复活：轮7 删 watchlist、轮8 删矩阵本体、轮10 重生成又被 raw/batch-B.json 带回来）。
# 凡 local_skill 不在 watchlist 的，一律不进矩阵（这正是第 0 腿判据 2 的机器化）。
_reg_path = BASE + "/data/skill-watchlist.json"
_allowed = None
if os.path.isfile(_reg_path):
    _allowed = set(e["local_skill"] for e in json.load(open(_reg_path, encoding="utf-8"))["watchlist"])
if _allowed is not None:
    _before = len(skills)
    _dropped = [s["local_skill"] for s in skills if s["local_skill"] not in _allowed]
    skills = [s for s in skills if s["local_skill"] in _allowed]
    if _dropped:
        print("watchlist-gate dropped:", _dropped)
    else:
        print("watchlist-gate: all %d entries in registry" % len(skills))

skills.sort(key=lambda x: -(x["best"]["total"] if x["best"] else 0))

# 轮10 固化：生成后自动补候选星值（源＝watchlist 已实测值；没有再探 ungh.cc）
# 背景：轮6 手工刷新过 score-matrix.json，但轮8 重跑本脚本时被 batch 源覆盖回零——
# 「改派生件不改源」的老坑。固化为生成流程一步，杜绝再被覆盖。
def _known_stars():
    _m = {}
    _wp = BASE + "/data/skill-watchlist.json"
    if os.path.isfile(_wp):
        _reg = json.load(open(_wp, encoding="utf-8"))
        for _e in _reg.get("watchlist", []):
            for _c in (_e.get("candidates") or []):
                if _c.get("repo") and _c.get("stars") is not None:
                    _m[_c["repo"]] = _c["stars"]
    return _m

def _probe_stars(repo):
    try:
        import urllib.request as _u
        _req = _u.Request("https://ungh.cc/repos/" + repo, headers={"User-Agent": "Mozilla/5.0"})
        _d = json.loads(_u.urlopen(_req, timeout=15).read().decode("utf-8", "replace"))
        _r = _d.get("repo") or {}
        return _r.get("stars")
    except Exception:
        return None

_known = _known_stars()
_filled = _probed = 0
for _s in skills:
    for _c in _s.get("candidates", []):
        _rp = _c.get("repo")
        if not _rp:
            continue
        if _c.get("stars") is None:
            if _rp in _known:
                _c["stars"] = _known[_rp]; _filled += 1
            else:
                _v = _probe_stars(_rp)
                if _v is not None:
                    _c["stars"] = _v; _probed += 1
        # 套件仓星值两级口径（03 V6.1）
        if _c.get("stars") is not None and _c["stars"] >= 10000:
            _c["stars_scope"] = ("repo_level" if _c.get("evidence")
                                 else "repo_level_but_entry_unlocated(套件仓星值属整仓，入口件未定位，仅供参考、不得作为替换依据)")
            if not _c.get("evidence"):
                _c["confidence"] = "low"
print("stars backfill: from-watchlist=%d probed=%d" % (_filled, _probed))
out = {"schema": "skill-score-matrix-v1", "generated_at": datetime.datetime.now().isoformat(timespec="seconds"),
       "rules": {"F": "入口件/本地字节比：3 + (>=1.5:+2 / >=1.0:+1 / <0.6:-1)", "E": "入口件字节：<=8K:5 <=16K:4 <=32K:3 <=64K:2 else 1",
                 "C": "SKILL.md 格式兼容 3；宽松许可+1；CC-BY-SA/GPL/无声明-1", "D": "入口件字节：>=15K:5 >=8K:4 >=3K:3 else 2",
                 "S": "stars：>=100k:5 >=10k:4 >=1k:3 >=100:2 else 1；>180天未活跃 -1；未取证记 null"},
       "baseline_date": NOW.isoformat(), "skills": skills}
open(BASE + "/data/score-matrix.json", "w", encoding="utf-8").write(json.dumps(out, ensure_ascii=False, indent=2))

A = []
A.append("# 03 · 技能替代对比评估矩阵（五维量化）")
A.append("")
A.append("> 生成时间 " + out["generated_at"] + "；基线日 " + out["baseline_date"])
A.append("> 打分规则（可复算，禁手改）：" + json.dumps(out["rules"], ensure_ascii=False))
A.append("> `—` = 该维度未取到实测数（记 null，不猜分）；总分只累加实测维度，并在列内标注实测量。")
A.append("")
for s in skills:
    A.append("## " + s["local_skill"] + "  （本地 " + str(s["local_bytes"]) + "B）")
    A.append("")
    A.append("候选面：" + str(s.get("remote_note") or "-"))
    A.append("")
    A.append("| # | 候选仓库 | ★ | 许可 | 最近活跃 | 上游入口字节 | 字节比(上游/本地) | F | E | C | D | S | 实测总分 | 取证 |")
    A.append("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|")
    for n, c in enumerate(s["candidates"], 1):
        A.append("| " + str(n) + " | " + str(c["repo"]) + " | " + str(c["stars"] if c["stars"] is not None else "—") + " | " + str(c["license"] or "—") + " | " + str(c["last_activity"] or "—") + " | " + str(c["ubytes"] or "—") + " | " + str(c["ratio_up_over_local"] if c["ratio_up_over_local"] is not None else "—") + " | " + str(c["F"] if c["F"] is not None else "—") + " | " + str(c["E"] if c["E"] is not None else "—") + " | " + str(c["C"] if c["C"] is not None else "—") + " | " + str(c["D"] if c["D"] is not None else "—") + " | " + str(c["S"] if c["S"] is not None else "—") + " | **" + str(c["total_measured"] if c["total_measured"] is not None else "—") + "** (" + str(c["dims_measured"]) + "/5) | " + str(c["confidence"]) + " |")
    A.append("")
    for n, c in enumerate(s["candidates"], 1):
        A.append("- 候选" + str(n) + " 匹配理由：" + str(c["match_reason"]))
    A.append("")
open(BASE + "/03-对比评估矩阵.md", "w", encoding="utf-8").write(chr(10).join(A))
print("skills=%d candidates=%d" % (len(skills), sum(s["candidate_count"] for s in skills)))
for s in skills[:24]:
    b = s["best"]
    print("%-32s cand=%-2d best=%s" % (s["local_skill"], s["candidate_count"], json.dumps(b, ensure_ascii=False)))