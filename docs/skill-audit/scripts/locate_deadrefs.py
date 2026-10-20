import os, json
ROOT = "D:/global_skills"
missing = {
  "A-dynamic-workflow": ["multi_agent_collab.md"],
  "dingtalk-misc": ["intent-guide.md"],
  "dingtalk-shared": ["profile.md"],
  "hyperframes-media": ["transcript-guide.md"],
  "openclaw-fenjue-weekly": ["prompt_optimizer.md"],
  "prompt-system-audit": ["reply_footer.md"],
  "wps-knowledgebase": ["workflow.md"],
}
index = {}
for root, dirs, files in os.walk(ROOT):
    for f in files:
        index.setdefault(f, []).append(os.path.join(root, f).replace(os.sep, "/"))
out = {}
for skill, names in missing.items():
    for n in names:
        hits = [h for h in index.get(n, []) if "/" + skill + "/" not in h]
        out.setdefault(skill, {})[n] = hits[:6]
        print("%-26s %-24s 其它技能处的同名文件: %s" % (skill, n, hits[:4] if hits else "（全仓都不存在）"))
    refdir = os.path.join(ROOT, skill, "references")
    print("    references/ 实有:", sorted(os.listdir(refdir))[:14] if os.path.isdir(refdir) else "（无 references 目录）")
open(r"D:/workdesktop/docs/skill-audit/data/deadref-locations.json","w",encoding="utf-8").write(json.dumps(out, ensure_ascii=False, indent=2))