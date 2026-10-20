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

# V14.1 | 2026-10-20 轮14 | 索引键一律用目录名（磁盘事实），frontmatter name 降为 self_name；修幽灵条目
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