import importlib.util as u, os
spec=u.spec_from_file_location('m',r'D:/workdesktop/docs/skill-audit/scripts/quarterly_skill_sweep.py'); m=u.module_from_spec(spec); spec.loader.exec_module(m)
up=open(r'D:/workdesktop/docs/skill-audit/data/_upstream_crq.md',encoding='utf-8').read()
lo=open(r'D:/global_skills/code-review/SKILL.md',encoding='utf-8').read()
miss=sorted(m.heading_set(up,True)-m.heading_set(lo,True))
print('missing_norm=',len(miss), miss[:6])
lb=len(lo.encode('utf-8')); ub=len(up.encode('utf-8'))
print('bytes local=',lb,'upstream=',ub,'ratio=',round(lb/ub,3))