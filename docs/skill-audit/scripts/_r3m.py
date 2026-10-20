import json
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
rows = {r['local_skill']: r for r in d['rows']}
for n in ('test-driven-development','first-principles-decomposer'):
    r = rows[n]
    print('===', n, r['overall'])
    print('  drift:', r['leg_drift']['note'][:150])
unv = [r for r in d['rows'] if r['overall']=='UNVERIFIED']
print()
print('UNVERIFIED:', len(unv))
for r in unv: print('  %-30s ver=%s cand=%s disc=%s' % (r['local_skill'], r['leg_version']['state'], r['leg_candidates']['state'], r['leg_discover']['state']))