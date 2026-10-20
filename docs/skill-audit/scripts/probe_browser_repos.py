import urllib.request, json, time, re
UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36"
def get(u):
    return urllib.request.urlopen(urllib.request.Request(u, headers={"User-Agent": UA}), timeout=25).read().decode("utf-8","replace")
def raw(rp, path):
    u = "https://raw.githubusercontent.com/" + rp + "/HEAD/" + path.lstrip("/")
    with urllib.request.urlopen(urllib.request.Request(u, headers={"User-Agent": UA}), timeout=25) as r:
        return len(r.read())
def tree_entries(rp, sub=""):
    h = get("https://github.com/" + rp + ("/tree/HEAD/" + sub if sub else ""))
    hrefs = re.findall(r'href="/' + re.escape(rp) + r'/tree/([0-9a-f]{7,40})/([^"]*)"', h)
    if not hrefs: return []
    sha = hrefs[0][0]; pref = (sub + "/") if sub else ""; out = []
    for s, p in hrefs:
        if s != sha or not p.startswith(pref): continue
        rest = p[len(pref):]
        if not rest or "/" in rest: continue
        if rest not in out: out.append(rest)
    return out
R = ["vercel-labs/agent-browser", "garrytan/gstack", "browser-use/browser-use", "microsoft/playwright-mcp", "obra/superpowers-skills"]
out = {}
for rp in R:
    rec = {}
    try:
        rec["root"] = tree_entries(rp)
    except Exception as e:
        rec["root_err"] = repr(e)[:80]
    for sub in ["skill-data", "skills"]:
        try:
            rec[sub] = tree_entries(rp, sub)
        except Exception as e:
            rec[sub + "_err"] = repr(e)[:60]
    out[rp] = rec
    print(rp, json.dumps(rec, ensure_ascii=False)[:260])
    time.sleep(0.4)
json.dump(out, open(r"D:/workdesktop/docs/skill-audit/data/browser-repo-probe.json","w",encoding="utf-8"), ensure_ascii=False, indent=2)