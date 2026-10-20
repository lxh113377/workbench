import json
from collections import Counter
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
print('gen=', d['generated_at'], dict(Counter(r['overall'] for r in d['rows'])))
print('pollution flag:', 'rate_limit_pollution' in d)
print('RED:', [r['local_skill'] for r in d['rows'] if r['overall']=='RED'])
unv = [r['local_skill'] for r in d['rows'] if r['overall']=='UNVERIFIED']
print('UNVERIFIED:', unv)