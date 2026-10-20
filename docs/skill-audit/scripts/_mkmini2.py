import json
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
sub = {'schema':'skill-watchlist-v1','fuse_days':100,'active_days':400,'watchlist':[e for e in reg['watchlist'] if e['local_skill'] in ('subagent-driven-development','agent-browser','electron','dogfood','security-audit','code-review')]}
json.dump(sub, open(r'D:/workdesktop/docs/skill-audit/data/_mini-reg.json','w',encoding='utf-8'), ensure_ascii=False, indent=1)
print('mini entries:', len(sub['watchlist']))