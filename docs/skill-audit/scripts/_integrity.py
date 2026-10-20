import ast
t = open(r'D:/global_skills/A-memory-start/references/upgrade_footer_gate.py', encoding='utf-8').read()
print('len', len(t), 'has main:', 'def main(' in t, 'has V1.9:', 'V1.9' in t, 'has V1.10:', 'V1.10' in t)
print('tail:', repr(t[-120:]))
try:
    ast.parse(t)
    print('AST OK')
except SyntaxError as e:
    print('SYNTAX ERROR at line', e.lineno, ':', e.msg)
    lines = t.splitlines()
    for i in range(max(0, e.lineno-4), min(len(lines), e.lineno+2)):
        print(i+1, '|', lines[i][:110])