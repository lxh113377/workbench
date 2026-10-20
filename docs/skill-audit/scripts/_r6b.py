import json
m = json.load(open(r'D:/workdesktop/docs/skill-audit/data/score-matrix.json', encoding='utf-8'))
for n in ('gstack','ppt-master'):
    s = next((x for x in m['skills'] if x['local_skill']==n), None)
    print('===', n, '| local note:', (s.get('remote_note') or '')[:50])
    for c in s['candidates']:
        print('   %-34s stars=%-8s lic=%-12s ev=%s conf=%s' % (c.get('repo'), c.get('stars'), c.get('license'), (c.get('evidence') or '-')[:26], c.get('confidence')))
        print('      match_reason:', (c.get('match_reason') or '')[:90])