import urllib.request, hashlib
UA='Mozilla/5.0'
lo = r'D:/global_skills/agent-browser/SKILL.md'
lb = open(lo,'rb').read()
ver = [l for l in lb.decode('utf-8','replace').splitlines()[:8] if l.startswith('version')]
print('local bytes=', len(lb), 'sha=', hashlib.sha256(lb).hexdigest()[:12], ver)
u = 'https://raw.githubusercontent.com/vercel-labs/agent-browser/HEAD/skill-data/core/SKILL.md'
try:
    ub = urllib.request.urlopen(urllib.request.Request(u, headers={'User-Agent':UA}), timeout=30).read()
    same = hashlib.sha256(ub).hexdigest() == hashlib.sha256(lb).hexdigest()
    print('upstream core bytes=', len(ub), 'sha=', hashlib.sha256(ub).hexdigest()[:12], 'identical=', same)
except Exception as e:
    print('upstream FAIL', repr(e)[:90])