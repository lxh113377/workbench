import urllib.request, json
UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36"
def raw(rp, path):
    url = "https://raw.githubusercontent.com/" + rp + "/HEAD/" + path.lstrip("/")
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=25) as r:
        return len(r.read()), url
C = [
  ("pptx-deck", "wshobson/agents", "plugins/pptx-deck-creation/skills/pptx-deck-creation/SKILL.md"),
  ("agent-orchestration", "wshobson/agents", "plugins/agent-orchestration/commands/orchestrate.md"),
  ("agent-teams", "wshobson/agents", "plugins/agent-teams/skills/teams/SKILL.md"),
  ("guizang-ppt", "op7418/guizang-ppt-skill", "SKILL.md"),
  ("ppt-agent", "Akxan/ppt-agent-skill", "SKILL.md"),
  ("skales", "skalesapp/skales", "SKILL.md"),
  ("cdp-debugger", "Colgate13/claude-cdp-debugger", "SKILL.md"),
  ("cheliped", "tykimos/cheliped-skills", "SKILL.md"),
]
out = []
for tag, rp, path in C:
    rec = {"tag": tag, "repo": rp, "path": path}
    try:
        n, u = raw(rp, path); rec.update({"http": 200, "bytes": n})
    except Exception as e:
        rec["error"] = repr(e)[:90]
    out.append(rec); print("%-22s %-34s %s %s" % (tag, rp, rec.get("http") or "-", rec.get("bytes") or rec.get("error")))
json.dump(out, open(r"D:/workdesktop/docs/skill-audit/data/batchA-path-verify2.json","w",encoding="utf-8"), ensure_ascii=False, indent=2)