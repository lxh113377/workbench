import json
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
for n in ('test-driven-development','first-principles-decomposer'):
    r = next((x for x in d['rows'] if x['local_skill']==n), None)
    if r:
        print('===', n, r['overall'])
        for k in r:
            if k.startswith('leg_'):
                print('   %-16s %-11s %s' % (k, r[k]['state'], r[k]['note'][:70]))