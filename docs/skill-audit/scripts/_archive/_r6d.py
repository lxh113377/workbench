import json
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
m = json.load(open(r'D:/workdesktop/docs/skill-audit/data/score-matrix.json', encoding='utf-8'))
mx = {}
for s in m['skills']:
    for c in s.get('candidates', []):
        if c.get('repo'): mx[c['repo']] = c.get('stars')
gap = []
for e in reg['watchlist']:
    for c in (e.get('candidates') or []):
        rp = c.get('repo')
        if rp and rp in mx and mx[rp] is not None and c.get('stars') != mx[rp]:
            gap.append((e['local_skill'], rp, c.get('stars'), mx[rp]))
print('watchlist vs matrix star gaps:', len(gap))
for g in gap[:15]: print('   ', g)