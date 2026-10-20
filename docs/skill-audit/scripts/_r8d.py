import json
from collections import Counter
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
print('gen=', d['generated_at'], dict(Counter(r['overall'] for r in d['rows'])))
print('RED:', [r['local_skill'] for r in d['rows'] if r['overall']=='RED'])
print('UNVERIFIED:', [r['local_skill'] for r in d['rows'] if r['overall']=='UNVERIFIED'])
print('executing-plans gone:', not any(r['local_skill']=='executing-plans' for r in d['rows']))