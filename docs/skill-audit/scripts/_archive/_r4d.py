import urllib.request, re
UA='Mozilla/5.0'
u = 'https://raw.githubusercontent.com/addyosmani/agent-skills/HEAD/skills/test-driven-development/SKILL.md'
up = urllib.request.urlopen(urllib.request.Request(u, headers={'User-Agent':UA}), timeout=25).read().decode('utf-8','replace')
# 列出剩余未吸收的标题（本地已吸收 4 块）
absorbed = {'Discover the Stack First','The TDD Cycle','Step 1: RED','Step 2: GREEN','Step 3: REFACTOR','The Prove-It Pattern','The Test Pyramid','Test Sizes'}
hs = []
infence = False
for ln in up.splitlines():
    if ln.strip().startswith('```'): infence = not infence; continue
    if infence: continue
    m = re.match(r'^#{2,4}\s+(.+?)\s*$', ln)
    if m and m.group(1) not in absorbed: hs.append(m.group(1))
print('remaining upstream headings:', len(hs))
for h in hs: print('  -', h)