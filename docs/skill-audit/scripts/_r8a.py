import json
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
m = json.load(open(r'D:/workdesktop/docs/skill-audit/data/score-matrix.json', encoding='utf-8'))
ent = {e['local_skill']: e for e in reg['watchlist']}
print('=== matrix-vs-watchlist field drift ===')
issues = []
for s in m['skills']:
    n = s['local_skill']
    e = ent.get(n)
    if not e:
        issues.append((n, 'NOT_IN_WATCHLIST', '', ''))
        continue
    # 1) matrix candidates 的 repo 应与 watchlist candidates 一致（集对齐）
    mrep = set(c.get('repo') for c in s.get('candidates') or [] if c.get('repo'))
    wrep = set(c.get('repo') for c in (e.get('candidates') or []) if c.get('repo'))
    if mrep != wrep:
        issues.append((n, 'CAND_REPO_SET_DIFF', str(sorted(mrep - wrep)), str(sorted(wrep - mrep))))
    # 2) matrix 的 best repo 若=upstream_repo，则 recorded_version 应存在（同源件）
    best = (s.get('best') or {}).get('repo')
    if best and e.get('upstream_repo') and best == e['upstream_repo'] and not e.get('recorded_version'):
        issues.append((n, 'SAME_ORIGIN_NO_BASELINE', best, ''))
print('issues:', len(issues))
for i in issues: print('   ', i)
print()
print('=== entry_kind vs matrix evidence consistency ===')
cnt = 0
for s in m['skills']:
    n = s['local_skill']
    e = ent.get(n)
    kind = (e or {}).get('entry_kind') or ''
    if kind.startswith('candidate_pool'):
        # 候选池条目：matrix 的 remote_note 应含 batch 说明，且不应有 same_origin 标注的 best=upstream
        rn = (s.get('remote_note') or '')[:40]
        has_ev = any(c.get('evidence') for c in s.get('candidates') or [])
        if not rn: cnt += 1
print('candidate_pool entries missing remote_note:', cnt)
print()
print('=== watchlist internal completeness ===')
miss_path = [e['local_skill'] for e in reg['watchlist'] if not e.get('upstream_path') and e.get('upstream_repo')]
print('has repo but no path:', len(miss_path), miss_path[:20])
miss_ver = [e['local_skill'] for e in reg['watchlist'] if not e.get('recorded_version') and e.get('upstream_repo')]
print('has repo but no baseline:', len(miss_ver), miss_ver[:20])