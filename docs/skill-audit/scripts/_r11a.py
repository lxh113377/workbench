import json
m = json.load(open(r'D:/workdesktop/docs/skill-audit/data/score-matrix.json', encoding='utf-8'))
for s in m['skills']:
    if s['local_skill'] == 'executing-plans':
        print(json.dumps(s, ensure_ascii=False)[:500])
print('generated_at:', m.get('generated_at'))
print('skills count:', len(m['skills']))
# 是否 batchE 的 sdd 条目被误判？看 batchE 内容
e = json.load(open(r'D:/workdesktop/docs/skill-audit/data/score-matrix-batchE.json', encoding='utf-8'))
print('batchE skills:', [s['local_skill'] for s in e['skills']])
print('batchE sdd candidates:', json.dumps(e['skills'][0].get('candidates'), ensure_ascii=False)[:300])