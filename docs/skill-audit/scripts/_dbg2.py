import json
d=json.load(open(r'D:/workdesktop/docs/skill-audit/data/score-matrix-batchA.json',encoding='utf-8'))
print('skills=',len(d['skills']))
for s in d['skills']:
    print(s['local_skill'], 'cand=', s['candidate_count'], 'best=', s['best'])
    for c in s['candidates'][:3]:
        print('   ', c['repo'], 'ubytes=', c.get('ubytes'), 'F=', c.get('F'), 'E=', c.get('E'), 'D=', c.get('D'), 'total=', c.get('total_measured'))