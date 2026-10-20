import json
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
ent = {e['local_skill']: e for e in reg['watchlist']}
e = ent['subagent-driven-development']
print(json.dumps(e, ensure_ascii=False, indent=1)[:1200])