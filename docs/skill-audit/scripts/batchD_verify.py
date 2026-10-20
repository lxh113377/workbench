import json, re, time, urllib.request, hashlib, datetime, os
UA = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36'
def get(url, timeout=25):
    req = urllib.request.Request(url, headers={'User-Agent': UA, 'Accept': 'text/html'})
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return r.read().decode('utf-8', 'replace')
def raw(rp, path):
    url = 'https://raw.githubusercontent.com/' + rp + '/HEAD/' + path.lstrip('/')
    req = urllib.request.Request(url, headers={'User-Agent': UA})
    with urllib.request.urlopen(req, timeout=25) as r:
        return r.read()
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


def tree_subdirs_of(rp, sub):
    try:
        return tree_list(rp, sub)
    except Exception:
        return []


def _unused_tree_subdirs(rp):
    h = get('https://github.com/' + rp + '/tree/HEAD/skills')
    hrefs = re.findall(r'href="/' + re.escape(rp) + r'/tree/([0-9a-f]{7,40})/([^"]*)"', h)
    if not hrefs: return []
    sha = hrefs[0][0]
    seen = []
    for s, p in hrefs:
        if s != sha or not p.startswith('skills/'): continue
        rest = p[len('skills/'):]
        if not rest or '/' in rest: continue
        if rest not in seen: seen.append(rest)
    return seen
ev = json.load(open(r'D:/workdesktop/docs/skill-audit/data/batchC-search-evidence.json', encoding='utf-8'))
def is_strong(q):
    return any((t.get('stars') or 0) >= 100 and not t.get('archived') for t in (q.get('top') or []))
strong = [q for q in ev['queries'] if not q.get('error') and is_strong(q)]
def best_candidate(q):
    cands = [t for t in q['top'] if (t.get('stars') or 0) >= 100 and not t.get('archived')]
    return max(cands, key=lambda x: x.get('stars') or 0)
def norm(s):
    return re.sub(r'[^a-z0-9]', '', (s or '').lower())
tree_cache = {}
out = {'schema': 'batchD-entry-verify-v1', 'ran_at': datetime.datetime.now().isoformat(timespec='seconds'), 'checked': []}
for i, q in enumerate(strong):
    name = q['local_skill']; t = best_candidate(q); rp = t['repo']
    rec = {'local_skill': name, 'repo': rp, 'stars': t.get('stars'), 'pushed': t.get('pushed'), 'license': t.get('license'), 'tried': [], 'resolved_path': None, 'bytes': None, 'sha16': None, 'http': None}
    if rp not in tree_cache:
        tree_cache[rp] = tree_subdirs_of(rp, 'skills')
        time.sleep(0.4)
    subs = tree_cache[rp]
    paths = []
    if isinstance(subs, list):
        nt = norm(name)
        exact = [s for s in subs if norm(s) == nt]
        contain = [s for s in subs if nt and (nt in norm(s) or norm(s) in nt)]
        for s in exact + contain:
            paths.append('skills/' + s + '/SKILL.md')
    rootsubs = tree_subdirs_of(rp, '')
    for s in rootsubs:
        if nt and (norm(s) == nt or nt in norm(s) or norm(s) in nt):
            paths.append(s + '/SKILL.md')
    paths += ['skills/' + name + '/SKILL.md', name + '/SKILL.md', 'SKILL.md']
    seen_paths = []
    for p in paths:
        if p in seen_paths: continue
        seen_paths.append(p)
        try:
            b = raw(rp, p)
            rec.update({'resolved_path': p, 'bytes': len(b), 'sha16': hashlib.sha256(b).hexdigest()[:16], 'http': 200})
            break
        except urllib.error.HTTPError as he:
            rec['tried'].append((p, he.code))
            if he.code not in (404, 403): break
        except Exception as e:
            rec['tried'].append((p, repr(e)[:50])); time.sleep(1.5)
    out['checked'].append(rec)
    print('%-26s %-38s %s %s' % (name, rp, rec['resolved_path'] or 'UNRESOLVED', rec['bytes'] or ''))
    if (i + 1) % 8 == 0:
        out['progress'] = i + 1
        json.dump(out, open(r'D:/workdesktop/docs/skill-audit/data/batchD-progress.json', 'w', encoding='utf-8'), ensure_ascii=False, indent=1)
json.dump(out, open(r'D:/workdesktop/docs/skill-audit/data/batchD-entry-verify.json', 'w', encoding='utf-8'), ensure_ascii=False, indent=1)
ok = sum(1 for x in out['checked'] if x['http'] == 200)
print('resolved %d / %d' % (ok, len(out['checked'])))