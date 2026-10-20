# -*- coding: utf-8 -*-
"""skill_inventory.py - 本地技能仓库全量盘点（可复跑，源文件不含反斜杠以避开序列化坑）。

产出 inventory.json + 01-现状盘点.md；判据全部可在磁盘/日志上复算。
"""
from __future__ import annotations

import argparse
import datetime
import json
import os
import re
import sys
from pathlib import Path

SKILLS_ROOT = Path("D:/global_skills")
FJ = Path(os.environ.get("FENJUE_ROOT", "C:/Users/37533/Desktop/workspace/焚诀"))
TRUTH = FJ / "eval" / "truth_constants.json"
ROUTE_TRACE = FJ / "eval" / "route_trace.jsonl"
MEMORY_ROOT = Path("D:/global_memory/memory")
SOFT_LIMIT = 12288
EXCLUDE_DIRS = {".temp", "_trash", "_bak", "_my-skills", ".git"}
BOM = chr(0xFEFF)
BS = chr(92)
SEP_CLS = "[" + BS + BS + "/]"


def parse_frontmatter(text):
    lines = text.splitlines()
    fm = {"triggers": []}
    if not lines:
        return fm
    first = lines[0].strip().lstrip(BOM).strip()
    if first != "---":
        return fm
    end = None
    for i in range(1, len(lines)):
        if lines[i].strip() == "---":
            end = i
            break
    if end is None:
        return fm
    body = lines[1:end]
    for ln in body:
        for key in ("name", "version", "description", "domain", "last_updated", "requires"):
            if ln.startswith(key + ":") and key not in fm:
                fm[key] = ln.split(":", 1)[1].strip().strip(chr(34)).strip(chr(39))
    tri = None
    for i, ln in enumerate(body):
        if ln.strip() == "triggers:":
            tri = i
            break
    if tri is not None:
        for ln in body[tri + 1:]:
            s = ln.strip()
            if not s:
                continue
            if s.startswith("- "):
                fm["triggers"].append(s[2:].strip().strip(chr(34)).strip(chr(39)))
            elif not ln[:1].isspace():
                break
    return fm


def index_reference_files(skills_root):
    """全仓 references/*.md 索引：文件名 -> 出现该文件的技能列表（用于判"是否全仓都没有"）。"""
    idx = {}
    for d in Path(skills_root).iterdir():
        ref = d / "references"
        if not d.is_dir() or not ref.is_dir():
            continue
        try:
            for f in ref.rglob("*.md"):
                idx.setdefault(f.name, []).append(d.name)
        except OSError:
            continue
    return idx


def dead_refs(text, refdir, skills_root=None, ref_index=None):
    """返回 (dead, loose, declared) 三桶。

    dead     = 断链：引用写了，但**全仓任何技能目录下都找不到**该文件；
    loose    = 松散引用：文件存在于别的技能，但本处写法无法唯一定位（如「加载 A-memory-start 的
               `references/x.md`」这种技能名写在反引号外的写法）——需人工改写成可解析路径；
    declared = 占位模板（split-NN.md / X.md）与引用点 ±60 字符内显式声明「缺失/历史遗留/拆分而来」的引用。

    解析顺序：① 跨技能相对路径 ../<skill>/references/<name>.md 与 <skill>/references/<name>.md；
              ② 本技能 <skill>/references/<name>.md；③ 全仓回退（命中且唯一 → loose，未命中 → dead）。"""
    dead, loose, declared = [], [], []
    rx = re.compile("references" + SEP_CLS + "([A-Za-z0-9_._-]+[.]md)")
    neg = re.compile("缺失|不存在|已不存在|历史遗留|拆分而来|不是共享|non-?existent|missing|does not exist")
    ref_index = ref_index or {}
    for m in rx.finditer(text):
        name = m.group(1)
        if name in dead or name in loose or name in declared:
            continue
        if re.search(r"(?i)(?:^|[^A-Za-z])(?:NN|XX|X)[.]md$", name) or "NN" in name:
            declared.append(name)
            continue
        ctx = text[max(0, m.start() - 60):m.end() + 60]
        if neg.search(ctx):
            declared.append(name)
            continue
        seg = text[max(0, m.start() - 80):m.end()]
        mm = re.search(r"\.\./" + SEP_CLS + "*([A-Za-z0-9_-]+)" + SEP_CLS + "*references" + SEP_CLS + "+([A-Za-z0-9_._-]+[.]md)$", seg)
        if mm and str(skills_root):
            cand = Path(skills_root) / mm.group(1) / "references" / mm.group(2)
        else:
            mm2 = re.search(r"([A-Za-z0-9_-]+)" + SEP_CLS + "+references" + SEP_CLS + "+([A-Za-z0-9_._-]+[.]md)$", seg)
            if mm2 and str(skills_root):
                cand = Path(skills_root) / mm2.group(1) / "references" / mm2.group(2)
            else:
                cand = refdir / name
        if cand.exists():
            continue
        if ref_index.get(name):
            loose.append(name + " -> " + ",".join(sorted(ref_index[name])[:3]))
        else:
            dead.append(name)
    return dead, loose, declared

def load_retired():
    try:
        d = json.loads(TRUTH.read_text(encoding="utf-8"))
        r = d.get("retired_skills")
        if isinstance(r, list):
            return set(str(x) for x in r)
        if isinstance(r, dict):
            return set(str(x) for x in r.keys())
    except Exception as e:
        print("[warn] retired_skills read failed: %r" % (e,), file=sys.stderr)
    return set()


def route_top1_counts():
    counts = {}
    if not ROUTE_TRACE.exists():
        return counts
    try:
        with ROUTE_TRACE.open("r", encoding="utf-8", errors="replace") as fh:
            for line in fh:
                if chr(34) + "top1" + chr(34) not in line:
                    continue
                try:
                    d = json.loads(line)
                except Exception:
                    continue
                t = d.get("top1")
                if t:
                    counts[t] = counts.get(t, 0) + 1
    except Exception as e:
        print("[warn] route_trace read failed: %r" % (e,), file=sys.stderr)
    return counts


def memory_mentions(days=90, max_files=300):
    cutoff = datetime.datetime.now() - datetime.timedelta(days=days)
    files = []
    if MEMORY_ROOT.exists():
        for p in MEMORY_ROOT.iterdir():
            if p.suffix.lower() != ".md":
                continue
            try:
                st = p.stat()
            except OSError:
                continue
            if datetime.datetime.fromtimestamp(st.st_mtime) >= cutoff:
                files.append((st.st_mtime, p))
    files.sort(key=lambda x: -x[0])
    files = files[:max_files]
    counts = {"_files_scanned": len(files)}
    rx = re.compile("[A-Za-z][A-Za-z0-9_-]{2,40}")
    for _, p in files:
        try:
            txt = p.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        for m in rx.finditer(txt):
            n = m.group(0)
            counts[n] = counts.get(n, 0) + 1
    return counts


def scan():
    retired = load_retired()
    ref_index = index_reference_files(SKILLS_ROOT)
    top1 = route_top1_counts()
    mention = memory_mentions()
    items = []
    for d in sorted(SKILLS_ROOT.iterdir(), key=lambda x: x.name):
        if not d.is_dir() or d.name in EXCLUDE_DIRS or d.name.startswith("."):
            continue
        sk = d / "SKILL.md"
        if not sk.exists():
            items.append({"name": d.name, "dir": d.name, "has_skill_md": False})
            continue
        try:
            text = sk.read_text(encoding="utf-8", errors="replace")
        except OSError as e:
            items.append({"name": d.name, "dir": d.name, "has_skill_md": True, "error": repr(e)})
            continue
        size = sk.stat().st_size
        total = 0
        for root, _dn, fn in os.walk(d):
            for f in fn:
                try:
                    total += os.path.getsize(os.path.join(root, f))
                except OSError:
                    pass
        refdir = d / "references"
        ref_files = 0
        if refdir.exists():
            ref_files = len([p for p in refdir.rglob("*") if p.is_file()])
        _dr = dead_refs(text, refdir, SKILLS_ROOT, ref_index)
        fm = parse_frontmatter(text)
                # 轮14 修幽灵条目：索引键一律用目录名（磁盘事实），frontmatter name 只作 self_name 记录。
        # 原实现 name 优先 ⇒ react-best-practices/ 目录（frontmatter name=vercel-react-best-practices）
        # 被记成不存在的 vercel-react-best-practices，实盘目录反而失踪，182/183 永远对不上。
        name = d.name
        self_name = fm.get("name") or d.name
        name_mismatch = bool(self_name and self_name != d.name)
        items.append({
            "name": name,
            "dir": d.name,
            "has_skill_md": True,
            "self_name": self_name,
            "name_mismatch": name_mismatch,
            "version": fm.get("version"),
            "description_len": len(fm.get("description") or ""),
            "triggers_count": len(fm.get("triggers") or []),
            "skill_md_bytes": size,
            "dir_bytes": total,
            "ref_files": ref_files,
            "over_soft_limit": size > SOFT_LIMIT,
            "retired_on_disk": (name in retired) or (d.name in retired),
            "dead_refs": _dr[0],
            "loose_refs": _dr[1],
            "declared_refs": _dr[2],
            "route_top1_count": top1.get(name, 0),
            "memory_mentions": mention.get(name, 0),
        })
    byname = {}
    for it in items:
        byname.setdefault(it["name"], []).append(it["dir"])
    dups = {k: v for k, v in byname.items() if len(v) > 1}
    return {
        "schema": "skill-inventory-v1",
        "generated_at": datetime.datetime.now().isoformat(timespec="seconds"),
        "skills_root": str(SKILLS_ROOT),
        "facts_source": {"truth_constants": str(TRUTH), "route_trace": str(ROUTE_TRACE),
                         "memory_root": str(MEMORY_ROOT)},
        "soft_limit_bytes": SOFT_LIMIT,
        "retired_count": len(retired),
        "route_trace_skills": len(top1),
        "items": items,
        "duplicates": dups,
    }


def md_report(inv):
    its = [i for i in inv["items"] if i.get("has_skill_md")]
    over = [i for i in its if i.get("over_soft_limit")]
    dead = [i for i in its if i.get("dead_refs")]
    novers = [i for i in its if not i.get("version")]
    retired = [i for i in its if i.get("retired_on_disk")]
    used = sorted(its, key=lambda x: -(x.get("route_top1_count") or 0))
    L = []
    A = L.append
    A("# 01 · 本地技能仓库全量盘点报告（机器派生）")
    A("")
    A("> 由 `scripts/skill_inventory.py` 于 " + inv["generated_at"] + " 生成；技能根 = `" + inv["skills_root"] + "`。")
    A("> 判据全部可在磁盘/日志上复算；下表数字均为脚本输出，非手抄。")
    A("")
    A("## 一、总量")
    A("")
    A("| 指标 | 值 |")
    A("|---|---|")
    A("| 技能目录数 | " + str(len(inv["items"])) + " |")
    A("| 含 SKILL.md | " + str(len(its)) + " |")
    A("| 缺 SKILL.md | " + str(len(inv["items"]) - len(its)) + " |")
    A("| SKILL.md > 12KB soft | " + str(len(over)) + " |")
    A("| frontmatter 缺 version | " + str(len(novers)) + " |")
    A("| 存在死引用 | " + str(len(dead)) + " |")
    A("| 退役名册命中但盘上仍有目录 | " + str(len(retired)) + " |")
    A("| 重名技能 | " + str(len(inv["duplicates"])) + " |")
    A("| 曾成为 router top1 的技能 | " + str(inv["route_trace_skills"]) + " |")
    A("")
    A("## 二、真实路由使用量 Top 30（route_trace.jsonl top1 计数）")
    A("")
    A("| # | 技能 | top1 次数 | SKILL.md 字节 | 版本 |")
    A("|---|---|---|---|---|")
    for n, i in enumerate(used[:30], 1):
        A("| " + str(n) + " | " + i["name"] + " | " + str(i.get("route_top1_count") or 0) + " | " + str(i["skill_md_bytes"]) + " | " + str(i.get("version") or "-") + " |")
    A("")
    A("## 三、体量超限清单（> 12KB soft）")
    A("")
    A("| 技能 | SKILL.md 字节 | 超限倍数 | 分卷文件数 |")
    A("|---|---|---|---|")
    for i in sorted(over, key=lambda x: -x["skill_md_bytes"]):
        A("| " + i["name"] + " | " + str(i["skill_md_bytes"]) + " | " + ("%.2f" % (i["skill_md_bytes"] / SOFT_LIMIT)) + "x | " + str(i["ref_files"]) + " |")
    A("")
    loose = [i for i in its if i.get("loose_refs")]
    declared = [i for i in its if i.get("declared_refs")]
    if loose:
        A("## 四A、松散引用（文件存在于别的技能，但本处写法无法唯一定位）")
        A("")
        A("| 技能 | 松散引用（指向的技能）|")
        A("|---|---|")
        for i in loose:
            A("| " + i["name"] + " | " + ", ".join(i["loose_refs"][:4]) + " |")
        A("")
    if declared:
        A("## 四B、显式声明的历史引用 / 占位模板（单列，不计缺陷）")
        A("")
        A("| 技能 | 条目 |")
        A("|---|---|")
        for i in declared:
            A("| " + i["name"] + " | " + ", ".join(i["declared_refs"][:6]) + " |")
        A("")
    if dead:
        A("## 四C、真断链（全仓任何技能目录下都找不到）")
        A("")
        A("| 技能 | 死引用 |")
        A("|---|---|")
        for i in dead:
            A("| " + i["name"] + " | " + ", ".join(i["dead_refs"][:6]) + " |")
        A("")
    if retired:
        A("## 五、退役名册命中但盘上仍有目录")
        A("")
        A("| 技能 | 目录 | 版本 |")
        A("|---|---|---|")
        for i in retired:
            A("| " + i["name"] + " | " + i["dir"] + " | " + str(i.get("version") or "-") + " |")
        A("")
    A("## 六、全量明细")
    A("")
    A("| 技能 | ver | SKILL.md | 目录字节 | refs | 死引用 | top1 | 记忆提及 |")
    A("|---|---|---|---|---|---|---|---|")
    for i in sorted(its, key=lambda x: x["name"]):
        A("| " + i["name"] + " | " + str(i.get("version") or "-") + " | " + str(i["skill_md_bytes"]) + " | " + str(i["dir_bytes"]) + " | " + str(i["ref_files"]) + " | " + str(len(i["dead_refs"])) + " | " + str(i.get("route_top1_count") or 0) + " | " + str(i.get("memory_mentions") or 0) + " |")
    A("")
    return chr(10).join(L)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--json", default="")
    ap.add_argument("--md", default="")
    a = ap.parse_args()
    inv = scan()
    if a.json:
        Path(a.json).parent.mkdir(parents=True, exist_ok=True)
        Path(a.json).write_text(json.dumps(inv, ensure_ascii=False, indent=2), encoding="utf-8")
    if a.md:
        Path(a.md).parent.mkdir(parents=True, exist_ok=True)
        Path(a.md).write_text(md_report(inv), encoding="utf-8")
    its = [i for i in inv["items"] if i.get("has_skill_md")]
    print("skills_total=%d with_md=%d over12k=%d dead=%d loose=%d declared=%d nover=%d retired=%d dups=%d top1=%d" % (
        len(inv["items"]), len(its),
        len([i for i in its if i.get("over_soft_limit")]),
        len([i for i in its if i.get("dead_refs")]),
        len([i for i in its if i.get("loose_refs")]),
        len([i for i in its if i.get("declared_refs")]),
        len([i for i in its if not i.get("version")]),
        len([i for i in its if i.get("retired_on_disk")]),
        len(inv["duplicates"]), inv["route_trace_skills"]))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())