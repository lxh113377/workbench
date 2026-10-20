import subprocess, json
paths = []
for n in ('brainstorming','code-review','debugging-fixing','refactoring','security-audit'):
    paths.append(n + '/SKILL.md')
for n, f in [('brainstorming','references/upstream-superpowers-v7.md'), ('code-review','references/upstream-agent-skills.md'), ('debugging-fixing','references/upstream-agent-skills.md'), ('refactoring','references/upstream-agent-skills.md'), ('security-audit','references/upstream-owasp-asi.md')]:
    paths.append(n + '/' + f)
r = subprocess.run(['git','-C',r'D:/global_skills','add','--']+paths, capture_output=True)
print('add rc', r.returncode, r.stderr.decode('utf-8','replace')[:150])
st = subprocess.run(['git','-C',r'D:/global_skills','status','--porcelain'], capture_output=True).stdout.decode('utf-8','replace')
staged = [l for l in st.splitlines() if l[:2] not in ('??','  ')]
print('staged=%d' % len(staged))
for l in staged: print(' ', l)
dirt = [l for l in st.splitlines() if l[:2] == ' M' or l[:2] == 'MM']
print('unstale dirty (must NOT be staged content):', len(dirt))