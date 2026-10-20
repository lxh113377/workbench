import json, hashlib, urllib.request, urllib.error, datetime, time, os
UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36"
def raw(rp, path):
    url = "https://raw.githubusercontent.com/" + rp + "/HEAD/" + path.lstrip("/")
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=25) as r:
        b = r.read()
    return b, url
def sha(b):
    return hashlib.sha256(b).hexdigest()
bb = json.load(open(r"D:/workdesktop/docs/skill-audit/raw/batch-B.json", encoding="utf-8"))
pairs = []
for s in bb["skills"]:
    for c in s.get("candidates", []):
        if c.get("entry_path"):
            pairs.append((s["local_skill"], c["repo"], c["entry_path"], c.get("evidence_url")))
out = {"schema": "candidate-path-verify-v1", "generated_at": datetime.datetime.now().isoformat(timespec="seconds"), "checked": [], "summary": {}}
ok = bad = skip = 0
seen = set()
for local, rp, path, ev in pairs:
    key = (rp, path)
    if key in seen or len(seen) >= 120:
        continue
    seen.add(key)
    rec = {"local_skill": local, "repo": rp, "path": path}
    try:
        b, url = raw(rp, path)
        rec.update({"http": 200, "bytes": len(b), "sha256": sha(b)[:16], "raw_url": url})
        ok += 1
    except urllib.error.HTTPError as e:
        rec.update({"http": e.code, "error": "HTTP %s" % e.code}); bad += 1
    except Exception as e:
        rec.update({"error": repr(e)}); skip += 1
    out["checked"].append(rec)
    print("%-32s %-42s %s" % (local, rp + ":" + path[-45:], rec.get("http") or rec.get("error") or rec.get("bytes")))
    time.sleep(0.25)
out["summary"] = {"ok": ok, "http_error": bad, "other": skip, "distinct_checked": len(seen)}
open(r"D:/workdesktop/docs/skill-audit/data/candidate-path-verify.json", "w", encoding="utf-8").write(json.dumps(out, ensure_ascii=False, indent=2))
print("SUMMARY", json.dumps(out["summary"]))