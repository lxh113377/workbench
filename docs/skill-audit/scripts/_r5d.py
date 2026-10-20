import json
inv = json.load(open(r'D:/workdesktop/docs/skill-audit/data/inventory.json', encoding='utf-8'))
items = inv['items']
nz = [i['name'] for i in items if (i.get('route_top1_count') or 0) > 0]
print('route_top1_count>0:', len(nz))
print('sdd in list:', 'subagent-driven-development' in nz)
print('sdd count:', [i.get('route_top1_count') for i in items if i['name']=='subagent-driven-development'])
z = [i['name'] for i in items if (i.get('route_top1_count') or 0) == 0]
print('zero-hit:', len(z))
print('route_trace_skills (top-level):', inv.get('route_trace_skills'))