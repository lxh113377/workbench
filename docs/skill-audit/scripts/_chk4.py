import json
d=json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json',encoding='utf-8'))
from collections import Counter
print('gen=',d['generated_at'],dict(Counter(r['overall'] for r in d['rows'])))
for r in d['rows']:
    if r['local_skill'] in ('brainstorming','verification-before-completion'):
        print('===',r['local_skill'],r['overall'])
        for k in ('leg_version','leg_drift','leg_candidates'):
            v=r[k]; print('  ',k,v['state'],'-',v['note'][:130])