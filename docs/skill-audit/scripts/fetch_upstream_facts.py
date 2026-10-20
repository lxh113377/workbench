# -*- coding: utf-8 -*-
"""fetch_upstream_facts.py - 锁定关键上游仓库实测事实（stars/last push/license/目录清单）。"""
import json, time, urllib.request, urllib.error, datetime
UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) skill-audit/1.0"
def gj(url):
    req = urllib.request.Request(url, headers={"User-Agent": UA, "Accept": "application/vnd.github+json"})
    with urllib.request.urlopen(req, timeout=25) as r:
        return json.loads(r.read().decode("utf-8", "replace"))
repos = ["obra/superpowers", "anthropics/skills", "addyosmani/agent-skills", "github/awesome-copilot", "obra/superpowers-skills", "wshobson/agents"]
out = {"schema": "upstream-facts-v1", "fetched_at": datetime.datetime.now().isoformat(timespec="seconds"), "repos": []}
calls = 0
for rp in repos:
    rec = {"repo": rp}
    try:
        d = gj("https://api.github.com/repos/" + rp); calls += 1
        rec.update({"stars": d.get("stargazers_count"), "pushed_at": d.get("pushed_at"), "updated_at": d.get("updated_at"),
                    "license": (d.get("license") or {}).get("spdx_id"), "archived": d.get("archived"),
                    "description": (d.get("description") or "")[:160], "default_branch": d.get("default_branch"),
                    "open_issues": d.get("open_issues_count"), "forks": d.get("forks_count")})
    except Exception as e:
        calls += 1
        rec["error"] = repr(e)
    try:
        c = gj("https://api.github.com/repos/" + rp + "/contents"); calls += 1
        rec["top_entries"] = [x.get("name") for x in c][:30]
    except Exception as e:
        calls += 1
        rec["top_error"] = repr(e)
    out["repos"].append(rec)
    time.sleep(0.4)
for rp in ["obra/superpowers", "anthropics/skills"]:
    rec = {"repo": rp, "kind": "skills_dir"}
    for sub in ["skills", "plugins"]:
        try:
            c = gj("https://api.github.com/repos/" + rp + "/contents/" + sub); calls += 1
            rec[sub] = [x.get("name") for x in c][:60]
        except Exception as e:
            calls += 1
            rec[sub] = repr(e)
    out["repos"].append(rec)
    time.sleep(0.4)
try:
    out["rate"] = {"calls_used_by_this_script": calls}
except Exception:
    pass
open(r"D:/workdesktop/docs/skill-audit/data/upstream-facts.json", "w", encoding="utf-8").write(json.dumps(out, ensure_ascii=False, indent=2))
print("calls=%d repos=%d" % (calls, len(out["repos"])))
for r in out["repos"]:
    if "stars" in r:
        print("%-28s %6s stars  pushed=%s  lic=%s  archived=%s" % (r["repo"], r.get("stars"), str(r.get("pushed_at"))[:10], r.get("license"), r.get("archived")))
    else:
        print("%-28s skills_dir: %s" % (r["repo"], str(r.get("skills"))[:160]))