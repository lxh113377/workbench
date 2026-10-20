import urllib.request, hashlib, importlib.util as u
UA='Mozilla/5.0'
u1='https://raw.githubusercontent.com/vercel-labs/agent-browser/HEAD/skill-data/dogfood/SKILL.md'
b=urllib.request.urlopen(urllib.request.Request(u1,headers={'User-Agent':UA}),timeout=30).read()
open(r'D:/workdesktop/docs/skill-audit/data/_upstream_dog.md','wb').write(b)
print('upstream bytes', len(b), hashlib.sha256(b).hexdigest()[:12])
lo=open(r'D:/global_skills/dogfood/SKILL.md',encoding='utf-8').read()
print('local bytes', len(lo.encode('utf-8')))
spec=u.spec_from_file_location('m',r'D:/workdesktop/docs/skill-audit/scripts/quarterly_skill_sweep.py'); m=spec.loader.exec_module.__self__ if False else u.module_from_spec(spec); spec.loader.exec_module(m)
up=b.decode('utf-8','replace')
print('missing in local:', sorted(m.heading_set(up,True)-m.heading_set(lo,True)))
print('local-only:', sorted(m.heading_set(lo,True)-m.heading_set(up,True)))