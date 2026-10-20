import re, urllib.request, json, time
UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36"
def get(u):
    return urllib.request.urlopen(urllib.request.Request(u, headers={"User-Agent": UA}), timeout=25).read().decode("utf-8","replace")
def entries(rp, sub=""):
    url = "https://github.com/" + rp + ("/tree/HEAD/" + sub if sub else "")
    h = get(url)
    hrefs = re.findall(r'href="/' + re.escape(rp) + r'/(?:tree|blob)/([0-9a-f]{7,40})/([^"]*)"', h)
    sha = hrefs[0][0] if hrefs else None
    pref = (sub + "/") if sub else ""
    tree, blob = [], []
    for s, p in hrefs:
        if s != sha or not p.startswith(pref): continue
        rest = p[len(pref):]
        if not rest or "/" in rest: continue
        kind = "blob" if re.search(r'/(?:blob)/' + re.escape(s) + "/" + re.escape(p) + '"', h) else "tree"
        (blob if kind == "blob" else tree).append(rest)
    return {"url": url, "tree": tree, "blob": blob}
T = [("wshobson/agents","plugins/agent-teams"), ("wshobson/agents","plugins/agent-teams/skills"), ("wshobson/agents","plugins/pptx-deck-creation/skills"),
     ("skalesapp/skales",""), ("Colgate13/claude-cdp-debugger",""), ("tykimos/cheliped-skills","")]
out = {}
for rp, sub in T:
    try:
        e = entries(rp, sub); out[rp + "::" + sub] = e
        print("%-42s tree=%s blob=%s" % (rp + "::" + sub, e["tree"][:8], e["blob"][:8]))
    except Exception as ex:
        out[rp + "::" + sub] = repr(ex); print("%-42s ERR %s" % (rp + "::" + sub, repr(ex)[:80]))
    time.sleep(0.4)
json.dump(out, open(r"D:/workdesktop/docs/skill-audit/data/layout-probe.json","w",encoding="utf-8"), ensure_ascii=False, indent=2)