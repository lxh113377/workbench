import json
m = json.load(open(r'D:/workdesktop/docs/skill-audit/data/score-matrix.json', encoding='utf-8'))
print('generated_at:', m.get('generated_at'), '| activity_refreshed_at:', m.get('activity_refreshed_at'))
miss = [(s['local_skill'], c.get('repo')) for s in m['skills'] for c in s.get('candidates', []) if c.get('stars') is None]
print('missing stars:', len(miss), miss[:12])
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/batchD-entry-verify.json', encoding='utf-8'))
print('batchD entries:', len(d['checked']))
# 这些缺星值的候选是否来自 batchD（其 stars 在 evidence 里但 matrix 生成时未带）
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
wm = {}
for e in reg['watchlist']:
    for c in (e.get('candidates') or []):
        if c.get('repo') and c.get('stars') is not None: wm[c['repo']] = c['stars']
print('watchlist has stars for them:', [(n, rp, wm.get(rp)) for n, rp in miss[:12]])