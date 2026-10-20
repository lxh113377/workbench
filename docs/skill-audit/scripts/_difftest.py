import subprocess
def sh(args): return subprocess.run(args, capture_output=True).stdout.decode('utf-8','replace')
for f in ['algorithmic-art', 'mcp-builder', 'writing-plans', 'A-skill-manager']:
    d = sh(['git','-C',r'D:/global_skills','diff','--unified=1','--', f+'/SKILL.md'])
    lines = [l for l in d.splitlines() if (l.startswith('+') or l.startswith('-')) and not l.startswith('++') and not l.startswith('--')]
    print('===', f, 'changed_lines=', len(lines))
    for l in lines[:6]:
        print('   ', l[:120])
    has_v = [l for l in lines if 'version' in l.lower()[:40]]
    print('   version-related:', len(has_v))