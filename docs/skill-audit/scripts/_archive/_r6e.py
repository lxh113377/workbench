import json
P = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(P, encoding='utf-8'))
m = json.load(open(r'D:/workdesktop/docs/skill-audit/data/score-matrix.json', encoding='utf-8'))
mx = {}
for s in m['skills']:
    for c in s.get('candidates', []):
        if c.get('repo') and c.get('stars') is not None: mx[c['repo']] = c['stars']
n = 0
for e in reg['watchlist']:
    for c in (e.get('candidates') or []):
        rp = c.get('repo')
        if rp in mx and c.get('stars') != mx[rp]:
            c['stars'] = mx[rp]
            n += 1
json.dump(reg, open(P, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('watchlist stars synced:', n)