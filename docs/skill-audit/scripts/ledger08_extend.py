import json
p = r'D:/workdesktop/docs/skill-audit/08-全量检索台账.md'
t = open(p, encoding='utf-8').read()
ver = json.load(open(r'D:/workdesktop/docs/skill-audit/data/batchD-entry-verify.json', encoding='utf-8'))
vm = {x['local_skill']: x for x in ver['checked']}
add = ['', '## 入口件实测（轮12 batch-D，raw.githubusercontent 免配额通道）', '', '> 对 37 条 STRONG 的最强候选做入口件定位：14 条 HTTP 200 实测字节；23 条三候选路径全 404＝该仓无 SKILL.md 规范布局（记「未定位」，按 05 §六 不得凭仓库名进迁移队列，只保留 stars/活跃读数）。', '', '| 技能 | 候选仓 | 入口件 | 字节 | 状态 |', '|---|---|---|---|---|']
for q in [x for x in ver['checked'] if x.get('http') == 200]:
    add.append('| %s | %s | `%s` | %s | ✅200 |' % (q['local_skill'], q['repo'], q['resolved_path'], q['bytes']))
add.append('')
add.append('**未定位（23）**：' + ', '.join(sorted(x['local_skill'] for x in ver['checked'] if x.get('http') != 200)))
add.append('')
add.append('> 血缘坐实：kepano/obsidian-skills 一仓承载 5 条（defuddle/json-canvas/obsidian-bases/cli/markdown）——本仓 obsidian 族的上游即 kepano 官方技能包；其 defuddle 上游件仅 831B，本仓 defuddle 已由治理轮换芯，不构成回退理由。')
add.append('')
open(p, 'w', encoding='utf-8').write(t + chr(10).join(add))
print('ledger 08 extended with entry-verify column')