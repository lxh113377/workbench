import json, glob, os
targets = ('debugging-fixing','code-review','refactoring','security-audit','brainstorming','writing-plans','verification-before-completion','dogfood')
rows = {}
for f in glob.glob(r'D:/global_memory/skill_content/*.json'):
    try: d = json.load(open(f, encoding='utf-8'))
    except Exception: continue
    items = d if isinstance(d, list) else (d.get('skills', []) if isinstance(d, dict) else [])
    for s in items:
        if isinstance(s, dict) and s.get('name') in targets: rows[s['name']] = (os.path.basename(f), s.get('triggers', []))
for n in sorted(targets):
    if n not in rows:
        print('%-30s NOT-IN-DATA-LAYER (路由层本来就不认识它)' % n); continue
    p = r'D:/global_skills/%s/SKILL.md' % n
    t = open(p, encoding='utf-8', errors='replace').read()
    lines = t.splitlines()
    trig = []
    try:
        i = lines.index('triggers:')
        for l in lines[i+1:]:
            if l.startswith('  - '): trig.append(l[4:].strip())
            elif l.strip() and not l.startswith(' '): break
    except ValueError: pass
    same = sorted(trig) == sorted(rows[n][1])
    print('%-30s datafile=%-20s json=%d md=%d identical=%s' % (n, rows[n][0], len(rows[n][1]), len(trig), same))