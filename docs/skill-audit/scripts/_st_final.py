import subprocess
r = subprocess.run(['python', r'D:/global_skills/A-memory-start/references/upgrade_footer_gate.py', '--selftest'], capture_output=True)
out = (r.stdout + r.stderr).decode('utf-8', 'replace')
print('rc =', r.returncode)
fails = [l for l in out.splitlines() if '[FAIL]' in l]
print('FAIL lines:', len(fails))
for l in fails: print(l)
tail = [l for l in out.splitlines() if 'selftest:' in l]
print('SUMMARY:', tail)