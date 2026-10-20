# -*- coding: utf-8 -*-
"""search_candidates.py - 用 GitHub search API 为 batch-A 技能检索候选（配额窗口内一次跑完）。"""
import json, time, urllib.parse, urllib.request, datetime
UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36"
API = "https://api.github.com/search/repositories?q={0}&sort=stars&order=desc&per_page=8"
QUERIES = {
  "session_protocol": "agent skill memory persistence session handoff",
  "project_memory": "claude code project memory handoff skill",
  "skill_manager": "agent skill registry manager marketplace claude",
  "workflow_orchestration": "multi agent orchestration skill claude workflow",
  "requirements_clarity": "requirements clarification interview skill agent",
  "project_direction": "project roadmap analysis skill agent codebase",
  "browser_automation": "browser automation agent skill playwright claude",
  "browser_cdp": "chrome devtools protocol agent skill cdp",
  "electron_automation": "electron app automation agent skill cdp",
  "headless_qa": "headless browser qa testing skill agent",
  "computer_use": "computer use windows desktop automation skill agent",
  "app_dogfood": "web app exploratory testing skill agent dogfood",
  "screenshot": "screenshot capture skill agent desktop",
  "office_docs": "office document skill claude docx pptx xlsx",
  "pptx_generation": "powerpoint generation skill agent pptx javascript",
  "html_presentation": "html presentation slides skill agent deck",
  "office_automation": "office automation skill email scheduled reports agent",
  "anthropic_official": "anthropics skills agent document pdf docx",
}
out = {"schema": "gh-search-candidates-v1", "fetched_at": datetime.datetime.now().isoformat(timespec="seconds"), "results": {}}
calls = 0
for key, q in QUERIES.items():
    url = API.format(urllib.parse.quote(q))
    rec = {"query": q, "items": []}
    try:
        req = urllib.request.Request(url, headers={"User-Agent": UA, "Accept": "application/vnd.github+json"})
        with urllib.request.urlopen(req, timeout=25) as r:
            d = json.loads(r.read().decode("utf-8", "replace"))
        calls += 1
        for it in (d.get("items") or [])[:8]:
            rec["items"].append({"repo": it.get("full_name"), "stars": it.get("stargazers_count"),
                                 "pushed_at": it.get("pushed_at"), "license": ((it.get("license") or {}).get("spdx_id")),
                                 "archived": it.get("archived"), "description": (it.get("description") or "")[:140]})
        rec["total_count"] = d.get("total_count")
    except Exception as e:
        calls += 1
        rec["error"] = repr(e)
    out["results"][key] = rec
    print("### %-24s total=%-7s got=%d" % (key, str(rec.get("total_count")), len(rec["items"])))
    for it in rec["items"][:5]:
        print("    %-42s %8s★ %-11s %s" % (it["repo"], it["stars"], str(it["pushed_at"])[:10], it["license"]))
    time.sleep(7)
out["api_calls"] = calls
open(r"D:/workdesktop/docs/skill-audit/data/gh-search-candidates.json", "w", encoding="utf-8").write(json.dumps(out, ensure_ascii=False, indent=2))
print("calls=", calls)