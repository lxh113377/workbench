import urllib.request, re
UA='Mozilla/5.0'
u = 'https://raw.githubusercontent.com/addyosmani/agent-skills/HEAD/skills/doubt-driven-development/SKILL.md'
up = urllib.request.urlopen(urllib.request.Request(u, headers={'User-Agent':UA}), timeout=25).read().decode('utf-8','replace')
lo = open('D:/global_skills/first-principles-decomposer/references/upstream-doubt-driven.md', encoding='utf-8').read()
lo += open('D:/global_skills/first-principles-decomposer/SKILL.md', encoding='utf-8').read()
import glob
for f in glob.glob('D:/global_skills/first-principles-decomposer/references/*.md'):
    lo += open(f, encoding='utf-8').read()
def heads(t):
    hs = []; inf = False
    for ln in t.splitlines():
        if ln.strip().startswith('```'): inf = not inf; continue
        if inf: continue
        m = re.match(r'^#{2,4}\s+(.+?)\s*$', ln)
        if m: hs.append(m.group(1))
    return hs
def nm(h): return re.sub(r'[^a-z0-9]','',h.lower())
lh = set(nm(h) for h in heads(lo))
rem = [h for h in heads(up) if nm(h) not in lh]
print('remaining:', len(rem))
for h in rem: print('  -', h)