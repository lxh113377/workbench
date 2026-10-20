import json
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
for r in d['rows']:
    if r['overall'] == 'UNVERIFIED':
        bad = [(k, r[k]['note'][:50]) for k in r if k.startswith('leg_') and r[k]['state'] in ('UNVERIFIED','RED')]
        print('%-26s %s' % (r['local_skill'], bad))