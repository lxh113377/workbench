import json
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
np = [e for e in reg['watchlist'] if not e.get('upstream_path')]
print('no upstream_path:', len(np))
for e in np:
    print('%-28s repo=%-40s tag=%s' % (e['local_skill'], e.get('upstream_repo') or 'NONE', e.get('recorded_version') or 'NONE'))