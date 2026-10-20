import importlib.util as u
spec=u.spec_from_file_location('m',r'D:/workdesktop/docs/skill-audit/scripts/quarterly_skill_sweep.py'); m=u.module_from_spec(spec); spec.loader.exec_module(m)
up,_n,_c = m.fetch_upstream_text('vercel-labs/agent-browser','skill-data/dogfood/SKILL.md',False)
lo=open(r'D:/global_skills/dogfood/SKILL.md',encoding='utf-8').read()
state,note = m.leg_drift(lo,'',up,'url')
print(state,'|',note)