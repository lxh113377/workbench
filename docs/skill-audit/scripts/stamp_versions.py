import json, os, re, sys
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/inventory.json', encoding='utf-8'))
nv = [i['dir'] for i in d['items'] if i.get('has_skill_md') and not i.get('version')]
root = r'D:/global_skills'
done, skipped = [], []
for name in nv:
    p = os.path.join(root, name, 'SKILL.md')
    try:
        raw = open(p, 'rb').read()
    except OSError as e:
        skipped.append((name, repr(e))); continue
    bom = raw.startswith(b'\xef\xbb\xbf')
    text = raw.decode('utf-8-sig', errors='replace')
    nl = '\r\n' if '\r\n' in text[:4000] else '\n'
    lines = text.splitlines()
    if not lines or lines[0].strip().lstrip('\ufeff') != '---':
        skipped.append((name, 'no frontmatter')); continue
    end = None
    for i in range(1, min(len(lines), 60)):
        if lines[i].strip() == '---':
            end = i; break
    if end is None:
        skipped.append((name, 'fm not closed')); continue
    if any(re.match(r'^version\s*:', l) for l in lines[1:end]):
        skipped.append((name, 'already has version')); continue
    # insert after last single-line frontmatter key, before closing ---
    ins = end
    # find triggers: multi-line block end
    for i in range(1, end):
        if lines[i].strip() == 'triggers:':
            j = i + 1
            while j < end and (lines[j].startswith((' ','\t')) or not lines[j].strip()):
                j += 1
            ins = j
            break
    new = lines[:ins] + ['version: 1.0.0'] + lines[ins:]
    out = nl.join(new) + (nl if text.endswith(('\n','\r\n')) else '')
    data = out.encode('utf-8')
    if bom: data = b'\xef\xbb\xbf' + data
    open(p, 'wb').write(data)
    done.append(name)
print('stamped=%d skipped=%d' % (len(done), len(skipped)))
for s in skipped: print('  SKIP', s)
json.dump({'stamped': done, 'skipped': [list(x) for x in skipped]}, open(r'D:/workdesktop/docs/skill-audit/data/version-stamp-log.json','w',encoding='utf-8'), ensure_ascii=False, indent=2)