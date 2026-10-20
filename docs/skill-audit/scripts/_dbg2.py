t = open(r'D:/global_skills/A-memory-start/references/upgrade_footer_gate.py', encoding='utf-8').read()
i = t.find('plugin 限定名不得误伤')
print(t[max(0,i-1400):i+400])