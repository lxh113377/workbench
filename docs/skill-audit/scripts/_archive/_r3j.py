import json
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
for e in reg['watchlist']:
    if e['local_skill'] in ('first-principles-decomposer','test-driven-development','A-dynamic-workflow','frontend-slides'):
        print(e['local_skill'], '->', e.get('upstream_repo'), '|', e.get('upstream_path'))