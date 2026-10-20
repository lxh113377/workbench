import re, urllib.request, json
UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36"
def get(u):
    return urllib.request.urlopen(urllib.request.Request(u, headers={"User-Agent": UA}), timeout=25).read().decode("utf-8","replace")
def list_dir(rp, sub):
    h = get("https://github.com/" + rp + "/tree/HEAD/" + sub)
    hrefs = re.findall(r'href="/' + re.escape(rp) + r'/tree/([0-9a-f]{7,40})/([^"]*)"', h)
    sha = hrefs[0][0] if hrefs else None
    pref = sub + "/" if sub else ""
    names = []
    for s, p in hrefs:
        if s != sha or not p.startswith(pref): continue
        rest = p[len(pref):]
        if not rest or "/" in rest: continue
        if rest not in names: names.append(rest)
    return names
out = {}
for sub in ["plugins/agent-orchestration", "plugins/agent-teams", "plugins/pptx-deck-creation"]:
    try:
        names = list_dir("wshobson/agents", sub)
        out[sub] = names
        print(sub, "->", names)
        for n in names[:6]:
            for cand in ["SKILL.md", "commands/" + n]:
                pass
    except Exception as e:
        out[sub] = repr(e)
        print(sub, "ERR", repr(e))
json.dump(out, open(r"D:/workdesktop/docs/skill-audit/data/wshobson-dirs.json","w",encoding="utf-8"), ensure_ascii=False, indent=2)