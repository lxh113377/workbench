import json
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
m = json.load(open(r'D:/workdesktop/docs/skill-audit/data/score-matrix.json', encoding='utf-8'))
skills_in_matrix = set(s['local_skill'] for s in m['skills'])
pool = [e for e in reg['watchlist'] if (e.get('entry_kind') or '').startswith('candidate_pool')]
print('matrix skills:', len(skills_in_matrix))
print('candidate_pool entries:', len(pool))
missing = [e['local_skill'] for e in pool if e['local_skill'] not in skills_in_matrix]
print('pool not in matrix:', len(missing))
print(missing[:30])
print()
print('total watchlist:', len(reg['watchlist']))
notin = [e['local_skill'] for e in reg['watchlist'] if e['local_skill'] not in skills_in_matrix]
print('watchlist not in matrix:', len(notin), notin[:30])