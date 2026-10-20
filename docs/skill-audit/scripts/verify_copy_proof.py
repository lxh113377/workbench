import urllib.request, json, hashlib, time
UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36"
def raw(rp, path):
    u = "https://raw.githubusercontent.com/" + rp + "/HEAD/" + path.lstrip("/")
    with urllib.request.urlopen(urllib.request.Request(u, headers={"User-Agent": UA}), timeout=25) as r:
        return r.read()
C = [
  ("dogfood-copy-check", "vercel-labs/agent-browser", "skill-data/dogfood/SKILL.md", "D:/global_skills/dogfood/SKILL.md"),
  ("electron-copy-check", "vercel-labs/agent-browser", "skill-data/electron/SKILL.md", "D:/global_skills/electron/SKILL.md"),
  ("agent-browser-copy-check", "vercel-labs/agent-browser", "skills/agent-browser/SKILL.md", "D:/global_skills/agent-browser/SKILL.md"),
  ("browser-use-qa", "browser-use/browser-use", "skills/qa/SKILL.md", None),
  ("superpowers-skills-testing", "obra/superpowers-skills", "skills/testing/SKILL.md", None),
]
out = []
for tag, rp, path, local in C:
    rec = {"tag": tag, "repo": rp, "path": path, "local_path": local}
    try:
        b = raw(rp, path)
        rec.update({"http": 200, "upstream_bytes": len(b), "upstream_sha256_16": hashlib.sha256(b).hexdigest()[:16]})
    except Exception as e:
        rec["error"] = repr(e)[:80]
    if local:
        try:
            lb = open(local, "rb").read()
            rec.update({"local_bytes": len(lb), "local_sha256_16": hashlib.sha256(lb).hexdigest()[:16],
                        "identical": rec.get("upstream_sha256_16") == hashlib.sha256(lb).hexdigest()[:16]})
        except Exception as e:
            rec["local_error"] = repr(e)[:60]
    out.append(rec)
    print("%-26s %-46s up=%-7s local=%-7s identical=%s" % (tag, path[-44:], rec.get("upstream_bytes"), rec.get("local_bytes"), rec.get("identical")))
    time.sleep(0.3)
json.dump({"schema":"copy-proof-v1","checked":out}, open(r"D:/workdesktop/docs/skill-audit/data/copy-proof.json","w",encoding="utf-8"), ensure_ascii=False, indent=2)