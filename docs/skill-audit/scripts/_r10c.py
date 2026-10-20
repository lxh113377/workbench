import json
m = json.load(open(r'D:/workdesktop/docs/skill-audit/data/score-matrix.json', encoding='utf-8'))
miss = [(s['local_skill'], c.get('repo')) for s in m['skills'] for c in s.get('candidates', []) if c.get('stars') is None]
print('missing stars now:', len(miss), miss[:8])
sc = sum(1 for s in m['skills'] for c in s.get('candidates', []) if c.get('stars_scope'))
print('stars_scope annotated:', sc)