import json
inv = json.load(open(r'D:/workdesktop/docs/skill-audit/data/inventory.json', encoding='utf-8'))
items = inv['items']
withmd = [i for i in items if i.get('has_skill_md')]
over = [i for i in items if i.get('over_soft_limit')]
dead = [i['name'] for i in items if i.get('dead_refs')]
loose = [i['name'] for i in items if i.get('loose_refs')]
decl = [i['name'] for i in items if i.get('declared_refs')]
nover = [i['name'] for i in items if not i.get('version')]
top1 = [i for i in items if (i.get('route_top1_count') or 0) > 0]
print('total=%d with_md=%d' % (len(items), len(withmd)))
print('over12k=%d dead=%d loose=%d declared=%d nover=%d top1=%d' % (len(over), len(dead), len(loose), len(decl), len(nover), len(top1)))
print('declared list:', decl)
print('over12k count by size:', sorted((i['skill_md_bytes'] for i in over), reverse=True)[:10])
print('retired_on_disk:', [i['name'] for i in items if i.get('retired_on_disk')])