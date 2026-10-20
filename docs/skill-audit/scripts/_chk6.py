import json
from collections import Counter
d=json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json',encoding='utf-8'))
print('gen=',d['generated_at'],'states=',dict(Counter(r['overall'] for r in d['rows'])))
gr=[r['local_skill'] for r in d['rows'] if r['overall']=='GREEN']
print('GREEN=',gr)