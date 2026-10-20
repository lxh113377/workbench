import subprocess
r = subprocess.run(['python', r'D:/global_skills/A-memory-start/references/upgrade_footer_gate.py', '--selftest'], capture_output=True)
out = (r.stdout + r.stderr).decode('utf-8', 'replace')
print('rc =', r.returncode)
for l in out.splitlines():
    if '[FAIL]' in l or 'selftest:' in l:
        print(l)