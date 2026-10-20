import json
d=json.load(open(r'D:/workdesktop/docs/skill-audit/raw/batch-B.json',encoding='utf-8'))
print('skills=%d' % len(d['skills']))
for s in d['skills']:
    print('=== %s  (local=%s)' % (s['local_skill'], (s.get('local_evidence') or {}).get('skill_md_bytes')))
    for c in s['candidates']:
        print('   %-45s %8s★ %-14s %-22s %s' % (c['repo'], c.get('stars'), c.get('last_activity'), c.get('license'), c.get('confidence')))
    print('   NOTE:', (s.get('notes') or '')[:200])