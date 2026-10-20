import json
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
for r in d['rows']:
    if r['overall'] == 'UNVERIFIED':
        print('UNVERIFIED:', r['local_skill'])
        for k in r:
            if k.startswith('leg_'):
                print('   %-16s %-11s %s' % (k, r[k]['state'], r[k]['note'][:80]))
red = [r['local_skill'] for r in d['rows'] if r['overall']=='RED']
print('RED:', red)