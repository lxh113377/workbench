import importlib.util as u
spec=u.spec_from_file_location('m',r'D:/workdesktop/docs/skill-audit/scripts/quarterly_skill_sweep.py'); m=u.module_from_spec(spec); spec.loader.exec_module(m)
for tag, loc in [('dbg', r'D:/global_skills/debugging-fixing/SKILL.md'), ('simp', r'D:/global_skills/refactoring/SKILL.md'), ('crq', r'D:/global_skills/code-review/SKILL.md'), ('bs', r'D:/global_skills/brainstorming/SKILL.md'), ('vbc', r'D:/global_skills/verification-before-completion/SKILL.md')]:
    try:
        up=open(r'D:/workdesktop/docs/skill-audit/data/_upstream_'+tag+'.md',encoding='utf-8').read()
    except Exception: continue
    lo=open(loc,encoding='utf-8').read()
    print(tag,'missing_norm=',sorted(m.heading_set(up,True)-m.heading_set(lo,True))[:6])