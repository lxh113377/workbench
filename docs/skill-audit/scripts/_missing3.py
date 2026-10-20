import importlib.util as u
spec=u.spec_from_file_location('m',r'D:/workdesktop/docs/skill-audit/scripts/quarterly_skill_sweep.py'); m=u.module_from_spec(spec); spec.loader.exec_module(m)
for tag, loc in [('brtest', r'D:/global_skills/testing/SKILL.md'), ('owasp', r'D:/global_skills/security-audit/SKILL.md'), ('cicd', r'D:/global_skills/ci-pipeline-debugging/SKILL.md')]:
    up=open(r'D:/workdesktop/docs/skill-audit/data/_upstream_'+tag+'.md',encoding='utf-8').read()
    lo=open(loc,encoding='utf-8').read()
    miss=sorted(m.heading_set(up,True)-m.heading_set(lo,True))
    ub=len(up.encode()); lb=len(lo.encode())
    print('###',tag,'ratio=',round(lb/ub,3),'missing(',len(miss),'):')
    for x in miss: print('   ',x)