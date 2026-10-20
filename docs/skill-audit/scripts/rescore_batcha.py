import json, datetime
BASE = r'D:/workdesktop/docs/skill-audit/'
a = json.load(open(BASE + 'data/score-matrix-batchA.json', encoding='utf-8'))
NOW = datetime.date(2026, 10, 20)
def band(v, bands, default=None):
    if v is None: return default
    for lo, sc in bands:
        if v >= lo: return sc
    return default
inv = json.load(open(BASE + 'data/inventory.json', encoding='utf-8'))
lb_map = {i['name']: i.get('skill_md_bytes') for i in inv['items'] if i.get('has_skill_md')}
n = 0
for s in a['skills']:
    lb = lb_map.get(s['local_skill'])
    s['local_bytes'] = lb
    for c in s['candidates']:
        ub = c.get('ubytes')
        if c.get('F') is None and ub and lb:
            r = round(ub / lb, 3)
            c['ratio_up_over_local'] = r
            c['F'] = max(1, min(5, 3 + (2 if r >= 1.5 else 1 if r >= 1.0 else -1 if r < 0.6 else 0)))
        st = c.get('stars')
        S = c.get('S')
        if S is None:
            S = band(st, [(100000, 5), (10000, 4), (1000, 3), (100, 2), (0, 1)])
            act = c.get('last_activity')
            age = None
            if act:
                try: age = (NOW - datetime.date.fromisoformat(str(act)[:10])).days
                except ValueError: age = None
            if S is not None and age is not None and age > 180: S = max(1, S - 1)
            c['S'] = S
        vals = [v for v in (c.get('F'), c.get('E'), c.get('C'), c.get('D'), c.get('S')) if v is not None]
        c['total_measured'] = sum(vals) if vals else None
        c['dims_measured'] = len(vals)
        n += 1
    s['candidates'].sort(key=lambda x: (-(x.get('total_measured') or 0), -(x.get('stars') or 0)))
    s['candidate_count'] = len(s['candidates'])
    s['best'] = ({'repo': s['candidates'][0]['repo'], 'total': s['candidates'][0]['total_measured']} if s['candidates'] else None)
print('rescore', n)
json.dump(a, open(BASE + 'data/score-matrix-batchA.json','w',encoding='utf-8'), ensure_ascii=False, indent=2)
for s in a['skills']: print('  %-30s best=%s' % (s['local_skill'], json.dumps(s['best'], ensure_ascii=False)))