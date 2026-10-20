import json
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
m = json.load(open(r'D:/workdesktop/docs/skill-audit/data/score-matrix.json', encoding='utf-8'))
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
rows = {r['local_skill']: r for r in d['rows']}
pool = [e for e in reg['watchlist'] if (e.get('entry_kind') or '').startswith('candidate_pool')]
print('pool entries:', len(pool))
# 矩阵里这些技能的候选数 vs watchlist candidates 数
gap = []
for e in pool:
    n = e['local_skill']
    s = next((x for x in m['skills'] if x['local_skill']==n), None)
    mc = s.get('candidate_count', 0) if s else 0
    wc = len(e.get('candidates') or [])
    if mc != wc: gap.append((n, mc, wc))
print('matrix vs watchlist candidate count gap:', len(gap))
for g in gap[:12]: print('   ', g)
# 矩阵候选是否带 stars/license（活性字段）
nostars = []
for s in m['skills']:
    for c in s.get('candidates', []):
        if c.get('stars') is None: nostars.append((s['local_skill'], c.get('repo')))
print('matrix candidates without stars:', len(nostars))
print(nostars[:10])