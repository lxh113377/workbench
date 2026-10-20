import json, urllib.request, re, time, hashlib, datetime
UA = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36'
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
need = [e for e in reg['watchlist'] if not e.get('upstream_path') and e.get('upstream_repo')]
print('to locate:', len(need))
def get(url, timeout=25):
    req = urllib.request.Request(url, headers={'User-Agent': UA, 'Accept': 'text/html'})
    return urllib.request.urlopen(req, timeout=timeout).read().decode('utf-8', 'replace')
def tree_list(rp, sub):
    url = 'https://github.com/' + rp + '/tree/HEAD/' + (sub or '.')
    h = get(url)
    hrefs = re.findall(r'href="/' + re.escape(rp) + r'/tree/([0-9a-f]{7,40})/([^"]*)"', h)
    if not hrefs: return []
    sha = hrefs[0][0]
    prefix = (sub + '/') if sub else ''
    seen = []
    for s, p in hrefs:
        if s != sha: continue
        if prefix and not p.startswith(prefix): continue
        rest = p[len(prefix):] if prefix else p
        if not rest or '/' in rest: continue
        if rest not in seen: seen.append(rest)
    return seen
def norm(s):
    return re.sub(r'[^a-z0-9]', '', (s or '').lower())
def raw_ok(rp, path):
    try:
        u = 'https://raw.githubusercontent.com/' + rp + '/HEAD/' + path.lstrip('/')
        b = urllib.request.urlopen(urllib.request.Request(u, headers={'User-Agent': UA}), timeout=20).read()
        return len(b), hashlib.sha256(b).hexdigest()[:12]
    except Exception:
        return None
cache = {}
out = {'ran_at': datetime.datetime.now().isoformat(timespec='seconds'), 'located': [], 'unresolved': []}
for e in need:
    name, rp = e['local_skill'], e['upstream_repo']
    if rp not in cache:
        try:
            cache[rp] = tree_list(rp, 'skills')
        except Exception:
            cache[rp] = []
        time.sleep(0.4)
    subs = cache[rp]
    nt = norm(name)
    paths = []
    exact = [s for s in subs if norm(s) == nt]
    part = [s for s in subs if nt and (nt in norm(s) or norm(s) in nt)]
    for s in exact + part:
        paths.append('skills/' + s + '/SKILL.md')
    if rp not in cache:
        pass
    if 'root:' + rp not in cache:
        try:
            cache['root:' + rp] = tree_list(rp, '')
        except Exception:
            cache['root:' + rp] = []
        time.sleep(0.3)
    root = cache['root:' + rp]
    cache['root:' + rp] = root
    for s in root:
        if nt and (norm(s) == nt or nt in norm(s) or norm(s) in nt):
            paths.append(s + '/SKILL.md')
    paths += ['skills/' + name + '/SKILL.md', name + '/SKILL.md', 'SKILL.md']
    seenp, done = [], None
    for p in paths:
        if p in seenp: continue
        seenp.append(p)
        r = raw_ok(rp, p)
        if r:
            done = (p, r[0], r[1]); break
    if done:
        e['upstream_path'] = done[0]
        e['last_checked'] = '2026-10-20'
        if not e.get('recorded_version'):
            e['recorded_version'] = 'sha256:%s@%dB' % (done[2], done[1])
            e['baseline_kind'] = 'content-snapshot(无 release 通道的合集仓)'
        out['located'].append({'local_skill': name, 'repo': rp, 'path': done[0], 'bytes': done[1]})
        print('OK  %-26s %-34s %s' % (name, done[0][:34], done[1]))
    else:
        out['unresolved'].append({'local_skill': name, 'repo': rp})
        print('UNRES %-26s %s' % (name, rp))
json.dump(reg, open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json','w',encoding='utf-8'), ensure_ascii=False, indent=2)
json.dump(out, open(r'D:/workdesktop/docs/skill-audit/data/path-locate-run-2026-10-20.json','w',encoding='utf-8'), ensure_ascii=False, indent=1)
print('located=%d unresolved=%d' % (len(out['located']), len(out['unresolved'])))