import urllib.request, hashlib
UA='Mozilla/5.0'
for tag, rp, path in [
    ('brtest', 'addyosmani/agent-skills', 'skills/browser-testing-with-devtools/SKILL.md'),
    ('owasp', 'github/awesome-copilot', 'skills/agent-owasp-compliance/SKILL.md'),
    ('cicd', 'addyosmani/agent-skills', 'skills/ci-cd-and-automation/SKILL.md'),
    ('secdbg', 'addyosmani/agent-skills', 'skills/debugging-and-error-recovery/SKILL.md'),
]:
    u = 'https://raw.githubusercontent.com/%s/HEAD/%s' % (rp, path)
    try:
        b = urllib.request.urlopen(urllib.request.Request(u, headers={'User-Agent': UA}), timeout=30).read()
        open(r'D:/workdesktop/docs/skill-audit/data/_upstream_' + tag + '.md', 'wb').write(b)
        print(tag, 'bytes', len(b), 'sha', hashlib.sha256(b).hexdigest()[:12])
    except Exception as e:
        print(tag, 'ERR', repr(e)[:90])