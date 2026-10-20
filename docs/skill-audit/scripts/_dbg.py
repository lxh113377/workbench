import json
d=json.load(open(r'D:/workdesktop/docs/skill-audit/data/score-matrix.json',encoding='utf-8'))
for s in d['skills']:
    if s['local_skill'] in ('docx','html-ppt','A-memory-start'):
        print('===', s['local_skill'], 'best=', s['best'])
        for c in s['candidates']:
            print('   ', c.get('repo'), 'ubytes=', c.get('ubytes'), 'F=', c.get('F'), 'E=', c.get('E'), 'C=', c.get('C'), 'D=', c.get('D'), 'S=', c.get('S'), 'total=', c.get('total_measured'))