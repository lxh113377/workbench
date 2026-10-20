import importlib.util as u
spec=u.spec_from_file_location('m',r'D:/workdesktop/docs/skill-audit/scripts/quarterly_skill_sweep.py'); m=u.module_from_spec(spec); spec.loader.exec_module(m)
import os
for tag, loc in [('dbg', r'D:/global_skills/debugging-fixing/SKILL.md'), ('simp', r'D:/global_skills/refactoring/SKILL.md')]:
    up=open(r'D:/workdesktop/docs/skill-audit/data/_upstream_'+tag+'.md',encoding='utf-8').read()
    lo=open(loc,encoding='utf-8').read()
    miss=sorted(m.heading_set(up,True)-m.heading_set(lo,True))
    lb=len(lo.encode()); ub=len(up.encode())
    print(tag,'missing_norm=',len(miss),miss[:4],'bytes local=',lb,'up=',ub,'ratio=',round(lb/ub,3))