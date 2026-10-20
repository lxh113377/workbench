import json, urllib.request, urllib.error, datetime, time, difflib
UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36"
def raw(rp, path):
    url = "https://raw.githubusercontent.com/" + rp + "/HEAD/" + path
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=25) as r:
        return r.read().decode("utf-8", "replace"), url
PAIRS = [
  ("brainstorming", "obra/superpowers", "skills/brainstorming/SKILL.md", "D:/global_skills/brainstorming/SKILL.md"),
  ("writing-plans", "obra/superpowers", "skills/writing-plans/SKILL.md", "D:/global_skills/writing-plans/SKILL.md"),
  ("executing-plans", "obra/superpowers", "skills/executing-plans/SKILL.md", "D:/global_skills/executing-plans/SKILL.md"),
  ("test-driven-development", "obra/superpowers", "skills/test-driven-development/SKILL.md", "D:/global_skills/test-driven-development/SKILL.md"),
  ("verification-before-completion", "obra/superpowers", "skills/verification-before-completion/SKILL.md", "D:/global_skills/verification-before-completion/SKILL.md"),
  ("systematic-debugging", "obra/superpowers", "skills/systematic-debugging/SKILL.md", None),
  ("docx", "anthropics/skills", "skills/docx/SKILL.md", "D:/global_skills/docx/SKILL.md"),
  ("pdf", "anthropics/skills", "skills/pdf/SKILL.md", "D:/global_skills/pdf/SKILL.md"),
  ("xlsx", "anthropics/skills", "skills/xlsx/SKILL.md", "D:/global_skills/xlsx/SKILL.md"),
  ("pptx", "anthropics/skills", "skills/pptx/SKILL.md", None),
  ("frontend-design", "anthropics/skills", "skills/frontend-design/SKILL.md", "D:/global_skills/frontend-design/SKILL.md"),
  ("mcp-builder", "anthropics/skills", "skills/mcp-builder/SKILL.md", "D:/global_skills/mcp-builder/SKILL.md"),
  ("doc-coauthoring", "anthropics/skills", "skills/doc-coauthoring/SKILL.md", "D:/global_skills/doc-coauthoring/SKILL.md"),
  ("theme-factory", "anthropics/skills", "skills/theme-factory/SKILL.md", "D:/global_skills/theme-factory/SKILL.md"),
  ("code-review-and-quality", "addyosmani/agent-skills", "skills/code-review-and-quality/SKILL.md", "D:/global_skills/code-review/SKILL.md"),
  ("debugging-and-error-recovery", "addyosmani/agent-skills", "skills/debugging-and-error-recovery/SKILL.md", "D:/global_skills/debugging-fixing/SKILL.md"),
  ("code-simplification", "addyosmani/agent-skills", "skills/code-simplification/SKILL.md", "D:/global_skills/refactoring/SKILL.md"),
]
def heads(t):
    return [x.strip() for x in t.splitlines() if x.lstrip().startswith("#")]
out = {"schema": "upstream-skill-sizes-v1", "fetched_at": datetime.datetime.now().isoformat(timespec="seconds"), "pairs": []}
for name, rp, path, local in PAIRS:
    rec = {"name": name, "upstream_repo": rp, "upstream_path": path, "local_path": local}
    try:
        up, url = raw(rp, path)
        rec["upstream_bytes"] = len(up.encode("utf-8"))
        rec["upstream_headings"] = len(heads(up))
        rec["upstream_url"] = url
    except Exception as e:
        rec["upstream_error"] = repr(e)
    if local:
        try:
            lt = open(local, encoding="utf-8", errors="replace").read()
            rec["local_bytes"] = len(lt.encode("utf-8"))
            rec["local_headings"] = len(heads(lt))
            if rec.get("upstream_bytes"):
                rec["size_ratio_local_over_upstream"] = round(rec["local_bytes"] / rec["upstream_bytes"], 3)
        except Exception as e:
            rec["local_error"] = repr(e)
    out["pairs"].append(rec)
    print("%-30s up=%-7s local=%-7s ratio=%s" % (name, rec.get("upstream_bytes"), rec.get("local_bytes"), rec.get("size_ratio_local_over_upstream")))
    time.sleep(0.3)
open(r"D:/workdesktop/docs/skill-audit/data/upstream-skill-sizes.json","w",encoding="utf-8").write(json.dumps(out, ensure_ascii=False, indent=2))