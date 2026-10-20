import urllib.request, re
UA='Mozilla/5.0'
u = 'https://raw.githubusercontent.com/addyosmani/agent-skills/HEAD/skills/first-principles-decomposer/SKILL.md'
up = urllib.request.urlopen(urllib.request.Request(u, headers={'User-Agent':UA}), timeout=25).read().decode('utf-8','replace')
print('upstream bytes', len(up))
open(r'D:/workdesktop/xtmp/_fpd_up.md','w',encoding='utf-8').write(up)
# 提取 CLAIM/EXTRACT/DOUBT/RECONCILE/STOP 五步 + Rationalizations + Red Flags 段
i = up.find('Step 1: CLAIM')
print('--- from CLAIM ---')
print(up[i-200:i+2600])