import json, urllib.request, re, datetime
UA='Mozilla/5.0'
P = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(P, encoding='utf-8'))
def tag_of(rp):
    try:
        req = urllib.request.Request('https://github.com/' + rp + '/releases.atom', headers={'User-Agent': UA})
        xml = urllib.request.urlopen(req, timeout=20).read().decode('utf-8', 'replace')
        m = re.search(r'<entry>.*?<title>(.*?)</title>', xml, re.S)
        return m.group(1).strip() if m else None
    except Exception:
        return None
PINS = {
  'dogfood': ('vercel-labs/agent-browser', 'skill-data/dogfood/SKILL.md',
            'same_origin_upstream(轮13 重钉: 轮12 测试 exec 误全量重建覆盖)',
            ' | 2026-10-20 裁决=吸收 依据=networkidle 反模式修正已入库(27752cd)，漂移腿合并口径曾 GREEN(0.966/独有0)'),
  'electron': ('vercel-labs/agent-browser', 'skill-data/electron/SKILL.md',
            'same_origin_upstream(轮13 重钉: 轮12 exec 覆盖回归修复)',
            ' | 2026-10-20 复核=与上游字节完全一致(SHA256 同)→不迁移，纳入防回退清单'),
  'agent-browser': ('vercel-labs/agent-browser', 'skill-data/core/SKILL.md',
            'same_origin_upstream(轮13 重钉: 轮12 exec 覆盖回归修复)',
            ' | 轮11 曾钉回 v0.39.1；本仓已 v3.0.0 薄壳化(并行会话 e6b5032c)——委托上游=结构上的终解'),
  'security-audit': ('github/awesome-copilot', 'skills/agent-owasp-compliance/SKILL.md',
            'same_origin_upstream(轮13 重钉: 吸收源；trailofbits 为方法论出处非件级上游)',
            ' | 2026-10-20 裁决=吸收(ASI十项 b8c7440)；trailofbits 误配曾致假 RED 0.395'),
}
fixed = []
for e in reg['watchlist']:
    n = e['local_skill']
    if n in PINS:
        rp, path, role, noteadd = PINS[n]
        e['upstream_repo'] = rp; e['upstream_path'] = path; e['upstream_role'] = role
        e['recorded_version'] = tag_of(rp) if rp == 'vercel-labs/agent-browser' else e.get('recorded_version')
        if rp == 'github/awesome-copilot': e['recorded_version'] = e.get('recorded_version')
        e['last_checked'] = '2026-10-20'
        if noteadd.strip() not in (e.get('notes') or ''): e['notes'] = (e.get('notes') or '') + noteadd
        fixed.append(n)
json.dump(reg, open(P, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('re-pinned:', fixed)
# 附：轮5-12 裁决注记对可能被覆盖条目的补写检查（缺哪条补哪条）
NEED = {
  'verification-before-completion': '裁决=吸收 依据=漂移腿RED(独有9)→吸收上游9节→复验GREEN(独有0)',
  'brainstorming': '裁决=吸收 依据=漂移腿RED(独有8)→吸收上游7节→复验漂移GREEN(独有0)',
  'code-review': '裁决=吸收 依据=字节比0.236→1.045',
  'debugging-fixing': '裁决=吸收 依据=字节比0.586→1.434',
  'refactoring': '裁决=吸收 依据=字节比0.355→1.201',
  'testing': '裁决=保持分叉',
  'ci-pipeline-debugging': '裁决=保持分叉',
  'docx': '裁决=保持本地(不替换)',
}
lost = []
for e in reg['watchlist']:
    key = e['local_skill']
    if key in NEED and NEED[key] not in (e.get('notes') or ''):
        e['notes'] = (e.get('notes') or '') + ' | 2026-10-20(轮13 补录覆盖丢失) ' + NEED[key]
        lost.append(key)
json.dump(reg, open(P, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('verdict notes restored:', lost)