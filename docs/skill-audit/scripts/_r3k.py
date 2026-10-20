import urllib.request, re
UA='Mozilla/5.0'
cases = [
  ('first-principles-decomposer','addyosmani/agent-skills','skills/doubt-driven-development/SKILL.md'),
  ('test-driven-development','addyosmani/agent-skills','skills/test-driven-development/SKILL.md'),
  ('A-dynamic-workflow','wshobson/agents','plugins/agent-teams/skills/multi-reviewer-patterns/SKILL.md'),
  ('frontend-slides','zarazhangrui/frontend-slides','SKILL.md'),
]
for n, rp, path in cases:
    u = 'https://raw.githubusercontent.com/%s/HEAD/%s' % (rp, path.lstrip('/'))
    try:
        b = urllib.request.urlopen(urllib.request.Request(u, headers={'User-Agent':UA}), timeout=25).read()
        fn = 'D:/workdesktop/xtmp/_up_%s.md' % n
        open(fn,'wb').write(b)
        print('%-30s %6d B -> %s' % (n, len(b), fn))
    except Exception as ex:
        print(n, 'ERR', repr(ex)[:60])