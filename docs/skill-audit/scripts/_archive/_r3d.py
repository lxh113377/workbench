import json, re
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
rows = {r['local_skill']: r for r in d['rows']}
def sim(r):
    m = re.search(r'相似度 ([0-9.]+)', r['leg_drift']['note'])
    return float(m.group(1)) if m else None
reclass = []
for e in reg['watchlist']:
    n = e['local_skill']
    r = rows.get(n)
    if not r: continue
    kind = e.get('entry_kind') or ''
    if kind.startswith('same_origin') or (not kind and r['leg_drift']['state']=='RED'):
        s = sim(r)
        uniq = re.search(r'上游独有标题 原始 (\d+)', r['leg_drift']['note'])
        u = int(uniq.group(1)) if uniq else 0
        # 判据：相似度<0.5 或 上游独有>=20 → 上游与本地非同源（自动选品伪同源）
        if (s is not None and s < 0.5) or u >= 20:
            reclass.append((n, s, u))
print('reclassify candidates:', len(reclass))
for x in reclass: print('  %-30s sim=%s uniq=%d' % x)