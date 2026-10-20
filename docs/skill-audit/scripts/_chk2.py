import json
for f in ['score-matrix.json','score-matrix-batchA.json']:
    d=json.load(open(r'D:/workdesktop/docs/skill-audit/data/'+f,encoding='utf-8'))
    names=[s['local_skill'] for s in d['skills']]
    print(f, len(names))
    print('  has slides:', 'slides' in names)
    print('  ', names)