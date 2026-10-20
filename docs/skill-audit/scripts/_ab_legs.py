import importlib.util as u
spec=u.spec_from_file_location('m',r'D:/workdesktop/docs/skill-audit/scripts/quarterly_skill_sweep.py')
m=u.module_from_spec(spec); spec.loader.exec_module(m)
from pathlib import Path
import json
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
e = [x for x in reg['watchlist'] if x['local_skill']=='agent-browser'][0]
lo, mb, note = m.load_local_merged(Path(e['local_skill_md']))
up, upn, c2 = m.fetch_upstream_text(e['upstream_repo'], e['upstream_path'], False)
st, nt = m.leg_drift(lo, note, up, upn)
print('main_bytes=', mb, '| merged note=', note)
print('drift:', st, '|', nt[:150])
val, vn, src, c = m.live_version(e['upstream_repo'], False)
vs, vn2 = m.leg_version(e['recorded_version'], val, vn)
print('version leg:', vs, '|', vn2[:110], '| source=', src)