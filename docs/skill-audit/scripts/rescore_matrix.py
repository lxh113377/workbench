import json, datetime
p = r'D:/workdesktop/docs/skill-audit/data/score-matrix.json'
m = json.load(open(p, encoding='utf-8'))
NOW = datetime.date(2026, 10, 20)
def band(v, bands, default=None):
    if v is None: return default
    for lo, sc in bands:
        if v >= lo: return sc
    return default
changed = []
for s in m['skills']:
    for c in s.get('candidates', []):
        # S 分按 stars 重算（>180天未活跃 -1）
        if c.get('stars') is not None:
            S = band(c['stars'], [(100000,5),(10000,4),(1000,3),(100,2),(0,1)])
            age = None
            if c.get('last_activity'):
                try: age = (NOW - datetime.date.fromisoformat(str(c['last_activity'])[:10])).days
                except ValueError: age = None
            if age is not None and age > 180: S = max(1, S-1)
            if c.get('S') != S:
                c['S'] = S
                changed.append((s['local_skill'], c['repo'], S))
        # 重算 total_measured 与 best
        vals = [c.get(k) for k in ('F','E','C','D','S') if c.get(k) is not None]
        c['total_measured'] = sum(vals) if vals else None
        c['dims_measured'] = len(vals)
    cands = [c for c in s.get('candidates', []) if c.get('total_measured') is not None]
    if cands:
        b = max(cands, key=lambda x: x['total_measured'])
        s['best'] = {'repo': b['repo'], 'total': b['total_measured']}
json.dump(m, open(p,'w',encoding='utf-8'), ensure_ascii=False, indent=2)
print('S rescored:', len(changed))
for x in changed: print('   %-28s %-34s S=%s' % x)
print()
for n in ('spike','scope-creep-detector','audit-runner-safe-aggregate','gstack','ppt-master'):
    s = next((x for x in m['skills'] if x['local_skill']==n), None)
    if s: print('%-28s best=%s total=%s' % (n, s['best']['repo'], s['best']['total']))