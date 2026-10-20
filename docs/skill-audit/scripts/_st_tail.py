import subprocess
r = subprocess.run(['python', r'D:/global_skills/A-memory-start/references/upgrade_footer_gate.py', '--selftest'], capture_output=True)
out = (r.stdout + r.stderr).decode('utf-8', 'replace')
print('LEN', len(out), 'rc', r.returncode)
print(out[-1500:])