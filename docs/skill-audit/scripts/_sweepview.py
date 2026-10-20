import json
d=json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json',encoding='utf-8'))
from collections import Counter
print('generated=',d['generated_at'],'rows=',len(d['rows']),'states=',dict(Counter(r['overall'] for r in d['rows'])),'http=',d['http_calls'])
print('names=',[r['local_skill'] for r in d['rows']][:40])