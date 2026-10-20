import urllib.request
UA='Mozilla/5.0'
u='https://raw.githubusercontent.com/obra/superpowers/HEAD/skills/brainstorming/SKILL.md'
b=urllib.request.urlopen(urllib.request.Request(u,headers={'User-Agent':UA}),timeout=25).read().decode('utf-8','replace')
open(r'D:/workdesktop/docs/skill-audit/data/_upstream_brainstorming.md','w',encoding='utf-8').write(b)
hs=[l.strip() for l in b.splitlines() if l.lstrip().startswith('#')]
print('upstream headings (%d):' % len(hs))
for h in hs: print('   ', h)
print('bytes', len(b.encode('utf-8')))