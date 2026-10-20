import json
from collections import Counter
d=json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json',encoding='utf-8'))
print('gen=',d['generated_at'],dict(Counter(r['overall'] for r in d['rows'])))
print('GREEN=',[r['local_skill'] for r in d['rows'] if r['overall']=='GREEN'])
for r in d['rows']:
    if r['local_skill'] in ('debugging-fixing','refactoring'):
        print(r['local_skill'],r['overall'],'|',r['leg_drift']['state'],'-',r['leg_drift']['note'][:95])