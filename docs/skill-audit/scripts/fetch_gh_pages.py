import json, urllib.request, re, sys
UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36"
def get(url, timeout=25):
    req = urllib.request.Request(url, headers={"User-Agent": UA, "Accept": "text/html,application/xhtml+xml"})
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return r.status, r.read().decode("utf-8", "replace")
repos = ["anthropics/skills","obra/superpowers","addyosmani/agent-skills","github/awesome-copilot","tfriedel/claude-office-skills","secondsky/claude-skills","Jeffallan/claude-skills","itgoyo/awesome-claude-code-skills","magnus919/agent-skills","laurigates/claude-plugins","nolabs-ai/claude-extensions","ReeperReepx/Security-Audit-Claude-Skill"]
out = {}
for rp in repos:
    rec = {}
    try:
        st, html = get("https://github.com/" + rp)
        rec["status"] = st; rec["html_bytes"] = len(html)
        ts = re.search(r"<title>(.*?)</title>", html, re.S)
        rec["title"] = (ts.group(1).strip()[:120] if ts else None)
        stx = re.search(r">([0-9,\.]+k?)</span>\s*</a>\s*<a[^>]*stargazers", html)
        if not stx:
            stx = re.search(r"stargazers[^>]*>[^0-9]*([0-9,\.]+k?)", html)
        rec["stars_html"] = (stx.group(1) if stx else None)
        lx = re.search(r"([A-Za-z0-9\-\.]+) license", html)
        rec["license_html"] = (lx.group(1) if lx else None)
        rec["has_skills_dir"] = bool(re.search(r"href=\"/" + re.escape(rp) + r"/tree/[^\"]*?/skills", html))
        dirs = sorted(set(re.findall(r"href=\"/" + re.escape(rp) + r"/tree/[^\"/?]+/([A-Za-z0-9_\-\.]+)\"", html)))
        rec["top_dirs"] = dirs[:25]
        rec["updated_txt"] = None
    except Exception as e:
        rec["error"] = repr(e)
    out[rp] = rec
    print(rp, json.dumps(rec, ensure_ascii=False)[:400])
open(r"D:/workdesktop/docs/skill-audit/data/gh-pages-facts.json","w",encoding="utf-8").write(json.dumps(out, ensure_ascii=False, indent=2))