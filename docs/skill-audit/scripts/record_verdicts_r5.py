import json
p = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(p, encoding='utf-8'))
V = {
  'security-audit': ' | 2026-10-20 裁决=吸收 依据=本地七Phase仅覆盖Web向OWASP Top10，agent向ASI Top10为真缺口→吸收16节，字节比0.996→1.628(commit b8c7440)',
  'dogfood': ' | 2026-10-20 裁决=吸收 依据=上游417B差异经hunk定位为networkidle反模式修正，本地CLI实测支持--url/--text/--fn→落地(commit 27752cd)；保留本地中文description',
  'testing': ' | 2026-10-20 裁决=保持分叉 依据=候选browser-testing-with-devtools是浏览器向，本地已含property-based/mutation/coverage；浏览器QA已由dogfood+browser-cdp+agent-browser+gstack承载，整灌=重复资产（缺23标题中18个属浏览器域）',
  'ci-pipeline-debugging': ' | 2026-10-20 裁决=保持分叉 依据=候选ci-cd-and-automation是「建CI」向(11332B/缺23标题含Basic CI/Preview/Feature flags)，本地是「排障六类归因决策树」向，定位不同非能力缺口；字节比已1.141',
}
for e in reg['watchlist']:
    n = e['local_skill']
    if n in V:
        e['notes'] = (e.get('notes') or '') + V[n]
    e['last_checked'] = '2026-10-20'
json.dump(reg, open(p, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('verdicts set for', len(V), 'entries')