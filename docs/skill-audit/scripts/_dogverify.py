import importlib.util as u, os
spec=u.spec_from_file_location('m',r'D:/workdesktop/docs/skill-audit/scripts/quarterly_skill_sweep.py'); m=u.module_from_spec(spec); spec.loader.exec_module(m)
up=open(r'D:/workdesktop/docs/skill-audit/data/_upstream_dog.md',encoding='utf-8').read()
loc=r'D:/global_skills/dogfood/SKILL.md'
lo=open(loc,encoding='utf-8').read()
print('netidle left:', lo.count('networkidle'), 'domcontentloaded:', lo.count('domcontentloaded'))
print('bytes', os.path.getsize(loc), 'upstream', len(up.encode()))
print('missing headings:', sorted(m.heading_set(up,True)-m.heading_set(lo,True)))
print('triggers intact:', '触发词：dogfood' in lo)