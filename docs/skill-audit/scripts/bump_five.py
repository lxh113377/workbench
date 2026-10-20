import re, os, json
ROOT='D:/global_skills'
CUR = {'brainstorming': '1.1.0', 'code-review': '1.1.0', 'debugging-fixing': '2.4.0', 'refactoring': '2.1.0', 'security-audit': '2.0.0'}
out = {}
for name, want in CUR.items():
    p = os.path.join(ROOT, name, 'SKILL.md')
    raw = open(p,'rb').read()
    t = raw.decode('utf-8-sig')
    m = re.search(r'^version:[ \t]*(\S+)[ \t]*$', t, re.M)
    if not m:
        out[name] = 'NO-VERSION-LINE'; continue
    cur = m.group(1)
    a, b, c = cur.split('.'); nxt = '%s.%s.%d' % (a, b, int(c) + 1)
    t2 = t[:m.start(1)] + nxt + t[m.end(1):]
    bom = raw[:3] == b'\xef\xbb\xbf'
    data = t2.encode('utf-8')
    if bom: data = b'\xef\xbb\xbf' + data
    open(p,'wb').write(data)
    out[name] = cur + ' -> ' + nxt + (' (frontmatter 实测 %s)' % cur if cur == want else ' (NOTE: 实际值=%s 非预期 %s，以实际递增)' % (cur, want))
print(json.dumps(out, ensure_ascii=False, indent=1))