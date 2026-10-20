import json
p = r'D:/workdesktop/docs/skill-audit/data/score-matrix-batchA.json'
d = json.load(open(p, encoding='utf-8'))
print('batchA skills:', [s['local_skill'] for s in d['skills']])