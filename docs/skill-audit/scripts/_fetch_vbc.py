import urllib.request
UA='Mozilla/5.0'
u='https://raw.githubusercontent.com/obra/superpowers/HEAD/skills/verification-before-completion/SKILL.md'
b=urllib.request.urlopen(urllib.request.Request(u,headers={'User-Agent':UA}),timeout=25).read().decode('utf-8','replace')
open(r'D:/workdesktop/docs/skill-audit/data/_upstream_vbc.md','w',encoding='utf-8').write(b)
print('bytes',len(b.encode('utf-8')))
print(b[:3000])