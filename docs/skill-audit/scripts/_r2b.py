import json
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
no_tag = [e for e in reg['watchlist'] if not e.get('recorded_version')]
print('no recorded_version:', len(no_tag))
for e in no_tag[:8]:
    print(' ', e['local_skill'], '| repo=', e.get('upstream_repo'), '| role=', (e.get('upstream_role') or '')[:40])