import importlib.util as u
spec=u.spec_from_file_location('m',r'D:/workdesktop/docs/skill-audit/scripts/quarterly_skill_sweep.py'); m=u.module_from_spec(spec); spec.loader.exec_module(m)
from pathlib import Path
UP = {'brainstorming':('obra/superpowers','skills/brainstorming/SKILL.md'), 'code-review':('addyosmani/agent-skills','skills/code-review-and-quality/SKILL.md'), 'debugging-fixing':('addyosmani/agent-skills','skills/debugging-and-error-recovery/SKILL.md'), 'refactoring':('addyosmani/agent-skills','skills/code-simplification/SKILL.md'), 'security-audit':('github/awesome-copilot','skills/agent-owasp-compliance/SKILL.md')}
out = {}
for k,(rp,path) in UP.items():
    lo, mb, note = m.load_local_merged(Path('D:/global_skills/%s/SKILL.md' % k))
    trip = m.fetch_upstream_text(rp, path, False)
    up = trip[0]
    st, nt = m.leg_drift(lo, note, up, 'x')
    out[k] = {'main': mb, 'state': st, 'note': nt}
    print('%-16s main=%6d %s %s' % (k, mb, st, nt[:80]))
import json; json.dump(out, open(r'D:/workdesktop/docs/skill-audit/data/presplit-drift-baseline.json','w',encoding='utf-8'), ensure_ascii=False, indent=2)