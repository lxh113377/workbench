import json, urllib.request, re
UA='Mozilla/5.0'
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
ent = {e['local_skill']: e for e in reg['watchlist']}
def headings(text):
    hs = []
    infence = False
    for ln in text.splitlines():
        if ln.strip().startswith('```'): infence = not infence; continue
        if infence: continue
        m = re.match(r'^#{2,4}\s+(.+?)\s*$', ln)
        if m: hs.append(m.group(1))
    return hs
def norm(s):
    return re.sub(r'[^a-z0-9]', '', s.lower())
for n in ('test-driven-development','first-principles-decomposer','A-dynamic-workflow','frontend-slides'):
    e = ent[n]
    rp, path = e['upstream_repo'], e['upstream_path']
    try:
        u = 'https://raw.githubusercontent.com/%s/HEAD/%s' % (rp, path.lstrip('/'))
        up = urllib.request.urlopen(urllib.request.Request(u, headers={'User-Agent':UA}), timeout=25).read().decode('utf-8','replace')
        lo = open(e['local_skill_md'], encoding='utf-8', errors='replace').read()
        import glob, os
        for f in glob.glob(os.path.dirname(e['local_skill_md']) + '/references/*.md'):
            lo += '\n' + open(f, encoding='utf-8', errors='replace').read()
        lh = set(norm(h) for h in headings(lo))
        uh = [h for h in headings(up) if norm(h) not in lh]
        print('===', n, '| upstream bytes', len(up), '| unique headings', len(uh))
        for h in uh[:14]: print('    +', h[:70])
    except Exception as ex:
        print(n, 'ERR', repr(ex)[:60])