import json, time, urllib.request, datetime
UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36"
REPOS = ["anthropics/skills","obra/superpowers","obra/superpowers-skills","addyosmani/agent-skills","github/awesome-copilot","tfriedel/claude-office-skills","secondsky/claude-skills","Jeffallan/claude-skills","magnus919/agent-skills","nolabs-ai/claude-extensions","ReeperReepx/Security-Audit-Claude-Skill","itgoyo/awesome-claude-code-skills","wshobson/agents","laurigates/claude-plugins","yonatangross/orchestkit","biaobiao2233/project-continuity"]
def get(url, timeout=25):
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return r.status, r.read().decode("utf-8", "replace")
def ungh(rp):
    last = None
    for attempt in range(4):
        try:
            st, txt = get("https://ungh.cc/repos/" + rp)
            d = json.loads(txt)
            if isinstance(d, dict) and "repo" in d:
                return d["repo"], "ungh.cc", attempt + 1
            last = txt[:120]
        except Exception as e:
            last = repr(e)
        time.sleep(1.5 * (attempt + 1))
    return None, "ungh.cc failed: " + str(last), 4
def shields(rp, what):
    try:
        st, txt = get("https://img.shields.io/github/" + what + "/" + rp + ".json")
        d = json.loads(txt)
        return d.get("value") or d.get("message"), "shields.io"
    except Exception as e:
        return None, repr(e)
out = {"schema": "gh-repo-facts-v1", "fetched_at": datetime.datetime.now().isoformat(timespec="seconds"), "repos": []}
for rp in REPOS:
    rec = {"repo": rp}
    d, src, tries = ungh(rp)
    rec["source"] = src
    rec["attempts"] = tries
    if d:
        rec.update({"stars": d.get("stars"), "forks": d.get("forks"), "pushed_at": d.get("pushedAt"),
                    "updated_at": d.get("updatedAt"), "created_at": d.get("createdAt"),
                    "default_branch": d.get("defaultBranch"), "description": (d.get("description") or "")[:200]})
    else:
        v, s2 = shields(rp, "stars")
        rec["stars_shield"] = v
        rec["source"] = s2
        v2, _ = shields(rp, "last-commit"); rec["last_commit_shield"] = v2
    out["repos"].append(rec)
    print(json.dumps(rec, ensure_ascii=False)[:300])
    time.sleep(0.5)
open(r"D:/workdesktop/docs/skill-audit/data/gh-repo-facts.json","w",encoding="utf-8").write(json.dumps(out, ensure_ascii=False, indent=2))