import json, re, urllib.request, datetime, time
UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36"
def get(url, timeout=30):
    req = urllib.request.Request(url, headers={"User-Agent": UA, "Accept": "text/html"})
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return r.read().decode("utf-8", "replace")
def list_dir(rp, sub):
    """返回 (entries, head_sha, note)。entry.name 为相对该目录的条目名。"""
    url = "https://github.com/" + rp + "/tree/HEAD" + (("/" + sub) if sub else "")
    html = get(url)
    hrefs = re.findall(r'href="/' + re.escape(rp) + r'/tree/([0-9a-f]{7,40})/([^"]*)"', html)
    sha = hrefs[0][0] if hrefs else None
    prefix = (sub + "/") if sub else ""
    names = []
    for s, path in hrefs:
        if s != sha:
            continue
        if prefix and not path.startswith(prefix):
            continue
        rest = path[len(prefix):]
        if not rest or "/" in rest:
            continue
        if rest not in names:
            names.append(rest)
    return names, sha, ("OK" if names else "名单为空（可能目录名不对或页面结构变化）")
TARGETS = [
  ("anthropics/skills", "skills"),
  ("obra/superpowers", "skills"),
  ("addyosmani/agent-skills", "skills"),
  ("Jeffallan/claude-skills", "skills"),
  ("github/awesome-copilot", "skills"),
  ("wshobson/agents", "plugins"),
  ("laurigates/claude-plugins", ""),
  ("magnus919/agent-skills", ""),
  ("yonatangross/orchestkit", "plugins/ork/skills"),
  ("secondsky/claude-skills", "plugins"),
  ("tfriedel/claude-office-skills", ""),
  ("itgoyo/awesome-claude-code-skills", ""),
  ("nolabs-ai/claude-extensions", "skills"),
]
out = {"schema": "gh-tree-v1", "fetched_at": datetime.datetime.now().isoformat(timespec="seconds"), "trees": []}
for rp, sub in TARGETS:
    rec = {"repo": rp, "sub": sub, "url": "https://github.com/" + rp + "/tree/HEAD" + (("/" + sub) if sub else "")}
    try:
        names, sha, note = list_dir(rp, sub)
        rec.update({"head": sha, "count": len(names), "items": names, "note": note})
    except Exception as e:
        rec["error"] = repr(e)
    out["trees"].append(rec)
    print("%-38s n=%-3s head=%s" % (rp + "/" + sub, rec.get("count"), (rec.get("head") or "")[:8]))
    time.sleep(0.4)
open(r"D:/workdesktop/docs/skill-audit/data/gh-trees.json","w",encoding="utf-8").write(json.dumps(out, ensure_ascii=False, indent=2))