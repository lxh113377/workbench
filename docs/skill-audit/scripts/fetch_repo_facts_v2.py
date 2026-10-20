import json, time, urllib.request
UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36"
def ungh(rp):
    for i in range(3):
        try:
            u = "https://ungh.cc/repos/" + rp
            with urllib.request.urlopen(urllib.request.Request(u, headers={"User-Agent": UA}), timeout=25) as r:
                d = json.loads(r.read().decode("utf-8","replace"))
            if isinstance(d, dict) and "repo" in d: return d["repo"]
        except Exception:
            pass
        time.sleep(1.2 * (i + 1))
    return None
REPOS = ["vercel-labs/agent-browser", "browser-use/browser-use", "microsoft/playwright-mcp", "garrytan/gstack",
         "op7418/guizang-ppt-skill", "Akxan/ppt-agent-skill", "skalesapp/skales", "JeecgBoot/jeecg-boot",
         "tykimos/cheliped-skills", "Colgate13/claude-cdp-debugger", "obra/superpowers-skills", "tfriedel/claude-office-skills",
         "LeoLin990405/claude-office-skills", "Pragmatic-Labs-AI/praxis", "rondoflow/rondoflow", "ray-amjad/claude-code-workflow-creator"]
out = {"schema": "gh-repo-facts-v2", "fetched_at": None, "repos": []}
import datetime; out["fetched_at"] = datetime.datetime.now().isoformat(timespec="seconds")
for rp in REPOS:
    d = ungh(rp)
    rec = {"repo": rp}
    if d:
        rec.update({"stars": d.get("stars"), "forks": d.get("forks"), "pushed_at": d.get("pushedAt"), "license": d.get("license"), "description": (d.get("description") or "")[:120]})
    else:
        rec["error"] = "ungh 未取到"
    out["repos"].append(rec)
    print("%-40s %8s★ %-11s %s" % (rp, rec.get("stars"), str(rec.get("pushed_at"))[:10], rec.get("license")))
    time.sleep(0.3)
json.dump(out, open(r"D:/workdesktop/docs/skill-audit/data/gh-repo-facts-v2.json","w",encoding="utf-8"), ensure_ascii=False, indent=2)