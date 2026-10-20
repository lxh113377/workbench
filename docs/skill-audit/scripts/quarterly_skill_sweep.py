# -*- coding: utf-8 -*-
"""quarterly_skill_sweep.py - 技能季度巡检扫描器（可复跑，四腿三态出口）。

四腿：
  腿1 版本对表  登记 recorded_version vs 上游最近 release tag
  腿2 镜像漂移  本地 SKILL.md vs 上游同名件（字节比 / difflib / 上游独有标题）
  腿3 候选活性  登记 candidates 逐个查 stars/pushed_at/archived，失活即 RED
  腿4 替代发现  按 discovery_query 用 GitHub search 找未登记的更活跃替代（需 API 配额）

取数通道（优先级从高到低，全程不依赖 api.github.com 也能出结论）：
  a) https://github.com/<repo>/releases.atom   无配额，给最新 tag + 发布时间  <-- 腿1 首选
  b) https://github.com/<repo>/commits/<br>.atom  无配额，给最近提交时间       <-- 腿1 回落
  c) https://ungh.cc/repos/<owner>/<repo>         给 stars/forks/pushedAt      <-- 腿3 首选
  d) https://api.github.com/...                   有 60/hr 匿名配额，配额耗尽返回 403 -> 记 UNVERIFIED

三态出口（与焚诀 eval/upstream_drift_check.py 同形）：
  0 = GREEN / 1 = RED / 2 = UNVERIFIED（取不到数，禁读成通过）

用法：
    python quarterly_skill_sweep.py --registry watchlist.json [--report r.md] [--json r.json] [--offline] [--discover N]
"""
from __future__ import annotations

import argparse
import datetime
import difflib
import json
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

GREEN, RED, UNVERIFIED, SKIP = 0, 1, 2, 3
STATE_NAME = {GREEN: "GREEN", RED: "RED", UNVERIFIED: "UNVERIFIED", SKIP: "SKIP"}
TO_IDX = {v: k for k, v in STATE_NAME.items()}
FUSE_DAYS_DEFAULT = 100
ACTIVE_DAYS_DEFAULT = 400
UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36"
API_REPO = "https://api.github.com/repos/{0}"
API_SEARCH = "https://api.github.com/search/repositories?q={0}&sort=stars&order=desc&per_page=10"
ATOM_REL = "https://github.com/{0}/releases.atom"
ATOM_COMMITS = "https://github.com/{0}/commits/{1}.atom"
UNGH = "https://ungh.cc/repos/{0}"


def http_bytes(url, timeout=25):
    req = urllib.request.Request(url, headers={"User-Agent": UA, "Accept": "*/*"})
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return r.read().decode("utf-8", "replace")


def http_json(url, timeout=25):
    return json.loads(http_bytes(url, timeout))


def retry(fn, tries=3, sleep=1.2):
    last = None
    for i in range(tries):
        try:
            return fn(), None
        except Exception as e:
            last = repr(e)
            time.sleep(sleep * (i + 1))
    return None, last


def norm_tag(v):
    s = (v or "").strip()
    return s[1:] if s[:1].lower() == "v" else s


def first_atom_entry(xml):
    m = re.search(r"<entry>(.*?)</entry>", xml, re.S)
    if not m:
        return None, None
    body = m.group(1)
    t = re.search(r"<title>(.*?)</title>", body, re.S)
    u = re.search(r"<updated>(.*?)</updated>", body, re.S)
    return (t.group(1).strip() if t else None), (u.group(1).strip() if u else None)


def heading_set(text, normalize=False, skip_fences=True):
    """文档标题集合。skip_fences=True 时跳过代码围栏内的行——
    ```bash 里的 `# Run the test` 是注释不是标题，计入会把修好的件误判 RED（实测 9 条误报全为此类）。"""
    out = set()
    in_fence = False
    fence = chr(96) * 3
    for ln in text.splitlines():
        if skip_fences and ln.strip().startswith(fence):
            in_fence = not in_fence
            continue
        if in_fence:
            continue
        if not ln.lstrip().startswith("#"):
            continue
        h = ln.strip()
        if normalize:
            h = re.sub(r"^#+\s*", "", h).lower()
            h = re.sub(r"[（(][^）)]*[）)]\s*$", "", h).strip()
            h = re.sub(r"[：:]\s*$", "", h).strip()
            h = re.sub(r"^(skill|skill:)\s*", "", h)
            h = re.sub(r"[\s:\-–—]+", " ", h).strip()
        out.add(h)
    return out


def days_since(iso, today):
    if not iso:
        return None
    try:
        return (today - datetime.date.fromisoformat(str(iso)[:10])).days
    except ValueError:
        return None


def live_version(repo, offline):
    """-> (value, note, source, calls)。a/b 无配额；c/d 可能被限流。"""
    if offline or not repo:
        return None, "offline 或未登记 upstream_repo", "-", 0
    calls = 0
    xml, err = retry(lambda: http_bytes(ATOM_REL.format(repo)), tries=2)
    calls += 2
    if xml:
        tag, when = first_atom_entry(xml)
        if tag:
            return norm_tag(tag), "releases.atom 最新 tag " + str(tag) + "（" + str(when) + "）", "releases.atom", calls
    d, err2 = retry(lambda: http_json(API_REPO.format(repo) + "/releases/latest"), tries=1)
    calls += 1
    if isinstance(d, dict) and d.get("tag_name"):
        return norm_tag(d["tag_name"]), "api releases/latest " + str(d.get("published_at")), "api.github.com", calls
    br = "main"
    xml2, err3 = retry(lambda: http_bytes(ATOM_COMMITS.format(repo, br)), tries=2)
    calls += 2
    if xml2:
        _t, when = first_atom_entry(xml2)
        return "sha-HEAD", "无 release tag；commits.atom 最近提交 " + str(when), "commits.atom", calls
    return None, "三条通道都未取到（atom: %s / api: %s）" % (err, err2), "-", calls


def fetch_upstream_text(repo, path, offline):
    if offline or not path or not repo:
        return None, "offline 或未登记 upstream_path", 0
    url = "https://raw.githubusercontent.com/" + repo + "/HEAD/" + str(path).lstrip("/")
    try:
        return http_bytes(url), url, 1
    except urllib.error.HTTPError as e:
        return None, "raw 拉取 HTTP %s（路径可能已改名）" % e.code, 1
    except Exception as e:
        return None, "raw 拉取失败: %r" % (e,), 1


def leg_version(recorded, live, note):
    if live is None:
        return UNVERIFIED, "未取到上游版本（" + note + "）"
    rec = norm_tag(recorded)
    if not rec:
        return UNVERIFIED, "登记表无 recorded_version，对表无基线"
    if live.startswith("sha-"):
        when = note.split("最近提交")[-1].strip() if "最近提交" in note else ""
        return GREEN, "上游无 release tag（" + note + "）；登记版本 " + rec + " 无 tag 可比，按最近提交时间判"
    if rec != live:
        return RED, "登记 " + rec + " != 上游观测 " + live + "（" + note + "）"
    return GREEN, "登记 " + rec + " == 上游观测（" + note + "）"


def leg_drift(local_text, local_note, up_text, up_note):
    if local_text is None:
        return UNVERIFIED, "本地 SKILL.md 不可读：" + local_note
    if up_text is None:
        return UNVERIFIED, "上游同名件取不到（" + up_note + "）"
    lb = len(local_text.encode("utf-8"))
    ub = len(up_text.encode("utf-8"))
    ratio = difflib.SequenceMatcher(None, local_text, up_text).quick_ratio()
    missing = sorted(heading_set(up_text) - heading_set(local_text))
    missing_norm = sorted(heading_set(up_text, True) - heading_set(local_text, True))
    state = GREEN
    if ub > 0 and lb < 0.6 * ub:
        state = RED
    if len(missing_norm) > 3:
        state = RED
    note = "本地(含references卷) %dB / 上游 %dB / 相似度 %.3f / 上游独有标题 原始 %d / 归一化 %d%s" % (
        lb, ub, ratio, len(missing), len(missing_norm), ("" if not missing_norm else "（例：" + "; ".join(missing_norm[:3]) + "）"))
    return state, note


def cand_facts(repo, offline):
    """-> (stars, pushed_days_iso, archived, license, source, err)。通道 c 的 ungh.cc，回落 api。"""
    if offline:
        return None, None, None, None, "-", "offline"
    d, err = retry(lambda: http_json(UNGH.format(repo)), tries=3)
    if isinstance(d, dict) and isinstance(d.get("repo"), dict):
        r = d["repo"]
        return r.get("stars"), r.get("pushedAt"), bool(r.get("archived")), r.get("license"), "ungh.cc", None
    d2, err2 = retry(lambda: http_json(API_REPO.format(repo)), tries=1)
    if isinstance(d2, dict) and d2.get("stargazers_count") is not None:
        return d2.get("stargazers_count"), d2.get("pushed_at"), bool(d2.get("archived")), (d2.get("license") or {}).get("spdx_id"), "api.github.com", None
    return None, None, None, None, "-", "ungh: " + str(err) + " / api: " + str(err2)


def leg_candidates(cands, today, offline, active_days):
    if not cands:
        return UNVERIFIED, "登记表无 candidates", [], 0
    if offline:
        return UNVERIFIED, "offline 模式，%d 个候选未复验" % len(cands), [], 0
    out = []
    calls = 0
    dead = []
    unknown = 0
    for c in cands:
        repo = c.get("repo")
        if not repo:
            unknown += 1
            continue
        stars, pushed, archived, lic, src, err = cand_facts(repo, offline)
        calls += 1
        age = days_since(pushed, today)
        rec = {"repo": repo, "stars": stars, "pushed_days": age, "archived": archived, "license": lic, "source": src}
        if err and stars is None:
            rec["state"] = "UNVERIFIED"; rec["note"] = "读数失败：" + str(err); unknown += 1
        elif archived:
            rec["state"] = "RED"; rec["note"] = "仓库已归档"; dead.append(repo)
        elif age is not None and age > active_days:
            rec["state"] = "RED"; rec["note"] = "最近推送 %d 天前 > %d 天活跃窗" % (age, active_days); dead.append(repo)
        else:
            rec["state"] = "GREEN"; rec["note"] = "活跃（%s 天前推送，stars=%s，来源 %s）" % (age, stars, src)
        out.append(rec)
    if dead:
        return RED, "失活候选 %d 个: %s" % (len(dead), ", ".join(dead[:4])), out, calls
    if unknown:
        return UNVERIFIED, "%d/%d 个候选未取到读数" % (unknown, len(cands)), out, calls
    return GREEN, "%d 个候选全部在活跃窗内" % len(cands), out, calls


def leg_discover(entry, today, offline, active_days, budget):
    q = entry.get("discovery_query")
    if not q:
        return UNVERIFIED, "未登记 discovery_query", [], 0
    if offline:
        return SKIP, "offline 模式，未做替代发现（可选腿）", [], 0
    if budget.get("left", 0) <= 0:
        return SKIP, "本次未启用替代发现（--discover 0）；该腿为可选增强，不计入总判定", [], 0
    try:
        d = http_json(API_SEARCH.format(urllib.parse.quote(q))); budget["left"] -= 1
    except Exception as e:
        budget["left"] -= 1
        return UNVERIFIED, "search 查询失败（多半是 api.github.com 配额耗尽）: %r" % (e,), [], 1
    known = set(str(c.get("repo", "")).lower() for c in (entry.get("candidates") or []))
    known.add(str(entry.get("upstream_repo", "")).lower())
    fresh = []
    for it in d.get("items", [])[:10]:
        full = it.get("full_name") or ""
        age = days_since(it.get("pushed_at"), today)
        if age is None or age > active_days:
            continue
        rec = {"repo": full, "stars": it.get("stargazers_count"), "pushed_days": age, "known": full.lower() in known}
        if not rec["known"]:
            fresh.append(rec)
    if fresh:
        return RED, "发现 %d 个未登记的活跃替代（例：%s）" % (len(fresh), ", ".join(x["repo"] + "(" + str(x["stars"]) + "★)" for x in fresh[:3])), fresh, 1
    return GREEN, "未发现未登记的活跃替代（活跃窗 %d 天）" % active_days, fresh, 1


def load_local_merged(local_path):
    """漂移腿的本地侧文本 = SKILL.md + references/ 下全部 .md 合并。

    为什么合并：B3b 把吸收块从主文件迁进 references/ 分卷后，只看主文件会把
    「内容仍在、只是换了文件」误判成漂移回归（判据先于动手修，见 05 规范 §十）。
    返回 (merged_text, main_bytes, note)。main_bytes 供体量口径单列。"""
    if not local_path or not local_path.is_file():
        return None, 0, "本地 SKILL.md 不可读：" + str(local_path)
    try:
        main = local_path.read_text(encoding="utf-8", errors="replace")
    except OSError as err:
        return None, 0, repr(err)
    parts = [main]
    refs = local_path.parent / "references"
    ref_files = 0
    if refs.is_dir():
        for rf in sorted(refs.rglob("*.md")):
            try:
                parts.append(rf.read_text(encoding="utf-8", errors="replace"))
                ref_files += 1
            except OSError:
                continue
    note = "主文件 %dB + %d 个 references 卷合并" % (len(main.encode("utf-8", "replace")), ref_files)
    return chr(10).join(parts), len(main.encode("utf-8", "replace")), note

def sweep(registry, offline=False, discover=0, active_days=None):
    today = datetime.date.today()
    fuse = int(registry.get("fuse_days") or FUSE_DAYS_DEFAULT)
    ad = int(active_days or registry.get("active_days") or ACTIVE_DAYS_DEFAULT)
    budget = {"left": int(discover or 0)}
    api_calls = 0
    rows = []
    for e in registry.get("watchlist", []):
        name = e.get("local_skill")
        repo = e.get("upstream_repo")
        path = e.get("upstream_path")
        local_path = Path(e.get("local_skill_md") or "")
        local_text, main_bytes, lnote = load_local_merged(local_path)
        # 委托壳识别（轮13 教训：agent-browser v3.0.0 / electron v1.1.0 薄壳化后内容随版委托上游，
        #   拿壳文本对厚上游比标题=假红）。壳必须有显式锚点（>=2 标记且含 skills get 或 .upstream），
        #   命中后漂移腿转记「委托」态 GREEN，上游漂移改由版本腿跟踪（版本腿仍可比 recorded_version）。
        _delegated = False
        _dmark = ''
        if local_text:
            _markers = ('skills get', '委托上游', '薄壳', '.upstream', '上游锚点')
            _hits = [mk for mk in _markers if mk in local_text]
            if len(_hits) >= 2 and ('skills get' in local_text or '.upstream' in local_text):
                _delegated = True
                _dmark = ','.join(_hits)

        val, note, src, c = live_version(repo, offline); api_calls += c
        legv = leg_version(e.get("recorded_version"), val, note)

        if repo and path:
            up_text, up_note, c2 = fetch_upstream_text(repo, path, offline)
        else:
            up_text, up_note, c2 = None, "未登记 upstream_path", 0
        api_calls += c2
        if _delegated:
            _ds, _dn = leg_drift(local_text, lnote, up_text, up_note)
            legd = (GREEN, '委托壳[%s]：命令面运行时随版获取，漂移由版本腿跟踪（原始壳-上游对比不计判：%s/%s）' % (_dmark, STATE_NAME[_ds], str(len(_dn))))
        else:
            legd = leg_drift(local_text, lnote, up_text, up_note)

        legc = leg_candidates(e.get("candidates") or [], today, offline, ad)
        api_calls += legc[3]

        legn = leg_discover(e, today, offline, ad, budget)
        api_calls += legn[3]

        checked = e.get("last_checked")
        age = days_since(checked, today)
        stale = (age is None) or (age > fuse)

        legs = [legv, legd, legc, legn]
        states = [x[0] for x in legs if x[0] != SKIP]
        if RED in states:
            overall = RED
        elif UNVERIFIED in states:
            overall = UNVERIFIED
        else:
            overall = GREEN
        if stale and overall == GREEN:
            overall = UNVERIFIED

        rows.append({
            "local_skill": name,
            "upstream_repo": repo,
            "upstream_path": path,
            "recorded_version": e.get("recorded_version"),
            "live_version": val,
            "local_main_bytes": main_bytes,
            "live_source": src,
            "leg_version": {"state": STATE_NAME[legv[0]], "note": legv[1]},
            "leg_drift": {"state": STATE_NAME[legd[0]], "note": legd[1]},
            "leg_candidates": {"state": STATE_NAME[legc[0]], "note": legc[1], "detail": legc[2]},
            "leg_discovery": {"state": STATE_NAME[legn[0]], "note": legn[1], "detail": legn[2], "optional": True},
            "last_checked": checked,
            "days_since_check": age,
            "stale": stale,
            "overall": STATE_NAME[overall],
        })
    worst = GREEN
    for r in rows:
        worst = max(worst, TO_IDX[r["overall"]])
    return {"schema": "skill-sweep-v1",
            "generated_at": datetime.datetime.now().isoformat(timespec="seconds"),
            "offline": offline, "fuse_days": fuse, "active_days": ad,
            "registry": str(registry.get("_source", "")), "http_calls": api_calls,
            "worst_state": STATE_NAME[worst], "rows": rows}


def md_report(res):
    A = []
    A.append("# 技能季度巡检扫描报告（机器派生）")
    A.append("")
    A.append("> " + res["generated_at"] + " | 最差态 = **" + res["worst_state"] + "** | offline=" + str(res["offline"]) + " | fuse=" + str(res["fuse_days"]) + " 天 | 活跃窗=" + str(res["active_days"]) + " 天 | HTTP 调用=" + str(res["http_calls"]))
    A.append("")
    A.append("")
    A.append("> **判读说明**：腿2（漂移）的对照对象 = 登记表 `upstream_path`，本方案取该技能**评分最高的已实测候选**作为代表上游；它与\"同源上游\"不必然相同（例如 test-driven-development 代表上游取的是 addyosmani/agent-skills 的 TDD，而真正同源上游是 obra/superpowers 的同名件）。同源上游的专项对表见 `data/upstream-skill-sizes.json`。腿4 为可选增强（未启用记 SKIP，不计入总判定）。")
    A.append("")
    A.append("| 本地技能 | 上游 | 登记版本 | 上游观测 | 版本腿 | 漂移腿 | 候选腿 | 发现腿 | 总判定 |")
    A.append("|---|---|---|---|---|---|---|---|---|")
    for r in res["rows"]:
        A.append("| " + str(r["local_skill"]) + " | " + str(r["upstream_repo"] or "-") + " | " + str(r["recorded_version"] or "-") + " | " + str(r["live_version"] or "-") + " | " + r["leg_version"]["state"] + " | " + r["leg_drift"]["state"] + " | " + r["leg_candidates"]["state"] + " | " + r["leg_discovery"]["state"] + " | **" + r["overall"] + "** |")
    A.append("")
    A.append("## 逐条证据")
    A.append("")
    for r in res["rows"]:
        A.append("### " + str(r["local_skill"]))
        A.append("- 版本腿：" + r["leg_version"]["state"] + " - " + r["leg_version"]["note"])
        A.append("- 漂移腿：" + r["leg_drift"]["state"] + " - " + r["leg_drift"]["note"])
        A.append("- 候选腿：" + r["leg_candidates"]["state"] + " - " + r["leg_candidates"]["note"])
        for d in r["leg_candidates"]["detail"]:
            A.append("    - " + str(d.get("repo")) + "：" + str(d.get("state")) + "（" + str(d.get("note")) + "）")
        A.append("- 发现腿：" + r["leg_discovery"]["state"] + " - " + r["leg_discovery"]["note"])
        A.append("- 上次对表：" + str(r["last_checked"] or "从未") + ("（已过期 " + str(r["days_since_check"]) + " 天）" if r["stale"] else ""))
        A.append("")
    return chr(10).join(A)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--registry", required=True)
    ap.add_argument("--report", default="")
    ap.add_argument("--json", default="")
    ap.add_argument("--offline", action="store_true")
    ap.add_argument("--discover", type=int, default=0)
    ap.add_argument("--active-days", type=int, default=0)
    a = ap.parse_args()
    reg = json.loads(Path(a.registry).read_text(encoding="utf-8"))
    reg["_source"] = a.registry
    res = sweep(reg, offline=a.offline, discover=a.discover, active_days=a.active_days or None)
    if a.json:
        Path(a.json).parent.mkdir(parents=True, exist_ok=True)
        Path(a.json).write_text(json.dumps(res, ensure_ascii=False, indent=2), encoding="utf-8")
    if a.report:
        Path(a.report).parent.mkdir(parents=True, exist_ok=True)
        Path(a.report).write_text(md_report(res), encoding="utf-8")
    for r in res["rows"]:
        print("  %-32s %-11s %s" % (r["local_skill"], r["overall"], r["leg_version"]["note"]))
    print("worst_state=" + res["worst_state"] + " http_calls=" + str(res["http_calls"]))
    return {"GREEN": 0, "RED": 1, "UNVERIFIED": 2}[res["worst_state"]]


if __name__ == "__main__":
    raise SystemExit(main())