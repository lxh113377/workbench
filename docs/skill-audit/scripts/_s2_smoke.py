import os, re
d = r'D:/global_skills/subagent-driven-development'
txt = open(os.path.join(d, 'SKILL.md'), encoding='utf-8').read()
# 冒烟：分卷引用可解析、无死链；退役名不得作为 name
refs = re.findall(r'`references/([^`]+\.md)`', txt)
bad = [r for r in refs if not os.path.isfile(os.path.join(d, 'references', r))]
print('refs:', refs, 'missing:', bad)
name = re.search(r'^name:\s*(.+)$', txt, re.M).group(1).strip()
print('name=', name, '| retired-name-safe:', name != 'executing-plans')
import json
tc = json.load(open(r'C:/Users/37533/Desktop/workspace/焚诀/eval/truth_constants.json', encoding='utf-8'))
print('retired list contains new name:', name in (tc.get('retired_skills') or []))
ok = not bad and name != 'executing-plans' and name not in (tc.get('retired_skills') or [])
print('SMOKE', 'PASS' if ok else 'FAIL')