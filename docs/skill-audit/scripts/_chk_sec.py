import importlib.util as u, os
spec=u.spec_from_file_location('m',r'D:/workdesktop/docs/skill-audit/scripts/quarterly_skill_sweep.py'); m=u.module_from_spec(spec); spec.loader.exec_module(m)
up=open(r'D:/workdesktop/docs/skill-audit/data/_upstream_owasp.md',encoding='utf-8').read()
loc=r'D:/global_skills/security-audit/SKILL.md'
lo=open(loc,encoding='utf-8').read()
miss=sorted(m.heading_set(up,True)-m.heading_set(lo,True))
lb=os.path.getsize(loc); ub=len(up.encode())
print('bytes=',lb,'upstream=',ub,'ratio=',round(lb/ub,3),'missing=',miss)