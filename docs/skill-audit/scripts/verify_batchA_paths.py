import json, time, urllib.request, urllib.error
UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36"
def raw(rp, path):
    url = "https://raw.githubusercontent.com/" + rp + "/HEAD/" + path.lstrip("/")
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=25) as r:
        b = r.read()
    return len(b), url
PAIRS = [
  ("docx", "anthropics/skills", "skills/docx/SKILL.md"),
  ("pdf", "anthropics/skills", "skills/pdf/SKILL.md"),
  ("xlsx", "anthropics/skills", "skills/xlsx/SKILL.md"),
  ("slide-deck", "anthropics/skills", "skills/pptx/SKILL.md"),
  ("pptx-deck", "wshobson/agents", "plugins/pptx-deck-creation/commands/pptx.md"),
  ("playwright", "Jeffallan/claude-skills", "skills/playwright-expert/SKILL.md"),
  ("browser-devtools", "addyosmani/agent-skills", "skills/browser-testing-with-devtools/SKILL.md"),
  ("browser-tools", "yonatangross/orchestkit", "plugins/ork/skills/browser-tools/SKILL.md"),
  ("memory-fabric", "yonatangross/orchestkit", "plugins/ork/skills/memory-fabric/SKILL.md"),
  ("memory", "yonatangross/orchestkit", "plugins/ork/skills/memory/SKILL.md"),
  ("quality-gates", "yonatangross/orchestkit", "plugins/ork/skills/quality-gates/SKILL.md"),
  ("orchestration", "wshobson/agents", "plugins/agent-orchestration/commands/orchestrate.md"),
  ("team-orchestration", "yonatangross/orchestkit", "plugins/ork/skills/agent-orchestration/SKILL.md"),
  ("ci-cd", "addyosmani/agent-skills", "skills/ci-cd-and-automation/SKILL.md"),
  ("code-simplification", "addyosmani/agent-skills", "skills/code-simplification/SKILL.md"),
  ("code-review-quality", "addyosmani/agent-skills", "skills/code-review-and-quality/SKILL.md"),
  ("debugging-recovery", "addyosmani/agent-skills", "skills/debugging-and-error-recovery/SKILL.md"),
  ("teams", "wshobson/agents", "plugins/agent-teams/commands/team.md"),
]
out = {"schema": "batchA-path-verify-v1", "fetched_at": None, "checked": []}
import datetime
out["fetched_at"] = datetime.datetime.now().isoformat(timespec="seconds")
for tag, rp, path in PAIRS:
    rec = {"tag": tag, "repo": rp, "path": path}
    try:
        n, url = raw(rp, path); rec.update({"http": 200, "bytes": n, "raw_url": url})
    except urllib.error.HTTPError as e:
        rec.update({"http": e.code, "error": "HTTP %s" % e.code})
    except Exception as e:
        rec.update({"error": repr(e)})
    out["checked"].append(rec)
    print("%-24s %-38s %s %s" % (tag, rp, rec.get("http") or "-", rec.get("bytes") or rec.get("error")))
    time.sleep(0.3)
open(r"D:/workdesktop/docs/skill-audit/data/batchA-path-verify.json","w",encoding="utf-8").write(json.dumps(out, ensure_ascii=False, indent=2))