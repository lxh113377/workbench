import subprocess, re
CASES = [
  ('verification-before-completion', 'e6c787da'),
  ('code-review', 'b3e189c'),
  ('debugging-fixing', 'f2dbf14'),
  ('refactoring', 'f2dbf14'),
  ('security-audit', 'b8c7440'),
]
def sh(a): return subprocess.run(a, capture_output=True).stdout.decode('utf-8', 'replace')
def fm(txt):
    lines = txt.splitlines()
    if not lines or lines[0].strip().lstrip('\ufeff') != '---': return None
    end = lines.index('---', 1)
    body = lines[1:end]
    name = re.findall(r'^name:[ ]*(.*)$', '\n'.join(body), re.M)
    desc = re.findall(r'^description:[ ]*(.*)$', '\n'.join(body), re.M)
    trig = [l[4:].strip() for l in body if l.startswith('  - ')]
    return {'name': name[:1], 'desc_len': [len(d) for d in desc], 'trig_n': len(trig), 'trig_sorted': sorted(trig)}
for name, sha in CASES:
    old = sh(['git','-C',r'D:/global_skills','show', sha + '~1:' + name + '/SKILL.md'])
    new = sh(['git','-C',r'D:/global_skills','show', 'HEAD:' + name + '/SKILL.md'])
    fo, fn = fm(old), fm(new)
    if not fo or not fn: print('%-30s fm-parse-fail old=%s new=%s' % (name, bool(fo), bool(fn))); continue
    same_trig = fo['trig_sorted'] == fn['trig_sorted']
    same_desc = fo['desc_len'] == fn['desc_len'] and fo['name'] == fn['name']
    print('%-30s triggers %s==%s same=%s | name/desc same=%s' % (name, fo['trig_n'], fn['trig_n'], same_trig, same_desc))