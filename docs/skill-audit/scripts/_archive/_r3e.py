import json
from collections import Counter
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
print('gen=', d['generated_at'], dict(Counter(r['overall'] for r in d['rows'])))
red = [r['local_skill'] for r in d['rows'] if r['overall']=='RED']
print('RED list:', red)