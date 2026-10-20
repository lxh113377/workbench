import json
src = json.load(open(r'D:/workdesktop/docs/skill-audit/data/gh-search-candidates.json', encoding='utf-8'))
out = {'schema': 'skill-substitute-candidates-v1', 'generated_at': src.get('fetched_at'), 'batch': 'batch-A',
       'facts_method_note': 'GitHub search API（18 次，配额窗口内）+ raw.githubusercontent 入口件实测 + ungh.cc 元数据；api.github.com/repos 单仓查询在配额耗尽时段不可用，未取到的 stars 一律 null。',
       'skills': []}
MAP = {
  'A-memory-start': ('session_protocol', ['yonatangross/orchestkit']),
  'A-ask-questions': ('requirements_clarity', []),
  'A-project-better': ('project_direction', []),
  'A-project-handoff': ('project_memory', ['yonatangross/orchestkit']),
  'A-skill-manager': ('skill_manager', ['obra/superpowers', 'anthropics/skills']),
  'A-dynamic-workflow': ('workflow_orchestration', ['wshobson/agents', 'yonatangross/orchestkit']),
  'agent-browser': ('browser_automation', ['vercel-labs/agent-browser', 'browser-use/browser-use', 'microsoft/playwright-mcp']),
  'browser-cdp': ('browser_cdp', ['microsoft/playwright-mcp', 'vercel-labs/agent-browser', 'Colgate13/claude-cdp-debugger']),
  'electron': ('electron_automation', ['vercel-labs/agent-browser']),
  'gstack': ('headless_qa', ['garrytan/gstack']),
  'computer-use-guidance-windows': ('computer_use', ['skalesapp/skales']),
  'dogfood': ('app_dogfood', ['vercel-labs/agent-browser', 'browser-use/browser-use']),
  'screenshot': ('screenshot', []),
  'docx': ('office_docs', ['anthropics/skills', 'tfriedel/claude-office-skills']),
  'ppt-master': ('pptx_generation', ['anthropics/skills', 'wshobson/agents', 'Akxan/ppt-agent-skill']),
  'html-ppt': ('html_presentation', ['op7418/guizang-ppt-skill', 'Akxan/ppt-agent-skill']),
  'html-ppt': ('html_presentation', ['op7418/guizang-ppt-skill', 'Akxan/ppt-agent-skill']),
  'office-automation-pro': ('office_automation', []),
}
ver = {}
for f in ['batchA-path-verify.json', 'batchA-path-verify2.json', 'batchA-path-verify3.json']:
    d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/' + f, encoding='utf-8'))
    for c in (d.get('checked', []) if isinstance(d, dict) else d):
        if c.get('http') == 200 and c.get('bytes'):
            ver[c['repo']] = (c['path'], c['bytes'])
for local, (qkey, repos) in MAP.items():
    q = src['results'].get(qkey, {})
    cands = []
    for it in (q.get('items') or []):
        if it['repo'] in repos:
            path, nb = ver.get(it['repo'], (None, None))
            cands.append({'repo': it['repo'], 'url': 'https://github.com/' + it['repo'], 'stars': it.get('stars'),
                          'last_activity': (str(it.get('pushed_at'))[:10] if it.get('pushed_at') else None),
                          'license': it.get('license'), 'entry_path': path, 'match_reason': (it.get('description') or '')[:150],
                          'evidence_url': 'raw.githubusercontent.com/' + it['repo'] + '/HEAD/' + (path or ''),
                          'fetched_at': src.get('fetched_at'), 'confidence': ('high' if nb else 'low'), 'entry_bytes': nb})
    for r in repos:
        if any(c['repo'] == r for c in cands):
            continue
        path, nb = ver.get(r, (None, None))
        cands.append({'repo': r, 'url': 'https://github.com/' + r, 'stars': None, 'last_activity': None, 'license': None,
                      'entry_path': path, 'match_reason': '检索未命中该仓（由人工点名比对）', 'evidence_url': ('raw.githubusercontent.com/' + r + '/HEAD/' + (path or '')),
                      'fetched_at': src.get('fetched_at'), 'confidence': ('high' if nb else 'low'), 'entry_bytes': nb})
    out['skills'].append({'local_skill': local, 'local_evidence': {'skill_md_bytes': None, 'note': '本地字节见 data/inventory.json'},
                          'candidates': cands, 'notes': ('检索式 %s 的 total_count=%s；命中 %d 个候选' % (qkey, q.get('total_count'), len(cands)))})
json.dump(out, open(r'D:/workdesktop/docs/skill-audit/raw/batch-A.json','w',encoding='utf-8'), ensure_ascii=False, indent=2)
print('skills=%d candidates=%d' % (len(out['skills']), sum(len(s['candidates']) for s in out['skills'])))
for s in out['skills']:
    print('  %-32s cand=%d' % (s['local_skill'], len(s['candidates'])))