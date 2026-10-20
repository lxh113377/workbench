import urllib.request, json
UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36"
def raw(rp, path):
    url = "https://raw.githubusercontent.com/" + rp + "/HEAD/" + path.lstrip("/")
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=25) as r:
        return len(r.read()), url
C = [
  ("task-coordination", "wshobson/agents", "plugins/agent-teams/skills/task-coordination-strategies/SKILL.md"),
  ("multi-reviewer", "wshobson/agents", "plugins/agent-teams/skills/multi-reviewer-patterns/SKILL.md"),
  ("pptx-slide-spec", "wshobson/agents", "plugins/pptx-deck-creation/skills/pptx-slide-specification/SKILL.md"),
  ("pptx-quality-gates", "wshobson/agents", "plugins/pptx-deck-creation/skills/pptx-quality-gates/SKILL.md"),
  ("skales-readme", "skalesapp/skales", "README.md"),
  ("cdp-debugger-readme", "Colgate13/claude-cdp-debugger", "README.md"),
  ("cheliped-readme", "tykimos/cheliped-skills", "README.md"),
  ("agent-orchestration-readme", "wshobson/agents", "plugins/agent-orchestration/README.md"),
]
out = []
for tag, rp, path in C:
    rec = {"tag": tag, "repo": rp, "path": path}
    try:
        n, u = raw(rp, path); rec.update({"http": 200, "bytes": n, "raw_url": u})
    except Exception as e:
        rec["error"] = repr(e)[:80]
    out.append(rec); print("%-26s %-52s %s %s" % (tag, path[-50:], rec.get("http") or "-", rec.get("bytes") or rec.get("error")))
json.dump({"schema":"batchA-path-verify3","checked":out}, open(r"D:/workdesktop/docs/skill-audit/data/batchA-path-verify3.json","w",encoding="utf-8"), ensure_ascii=False, indent=2)