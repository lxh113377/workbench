# 会话记录 · 子页面被挡住（ds 端 · 2026-10-20 二轮）

## 老大反馈
「除了总览界面没问题，任务，产出，模板，设施子页面都被挡住了，修好并检查」。

## 根因（先量后改，两轮查错方向）
一开始按「z-index / 元素遮挡」查：elementFromPoint 全部命中 pane 内部、z-index 正常 ⇒ 全绿。
真正原因不是遮挡，是**可见比例**：
- 老大窗口 1040×613 **物理像素**；本机屏幕 175% 缩放（DPR=1.75，1920×1200 报成 1463×914）
  ⇒ 页面真实 CSS 视口只有 **594×350**。
- 该视口下顶栏+页签吃掉 **239px = 68% 屏高**，子页只剩 **99px**。
- 总览首屏是 4 个矮 KPI 数字能露出；其余四页首屏是卡片标题区，几乎全在视口外 ⇒「被挡住」。

## 修法
1. `@media (max-width:720px)` 重做窄屏排版：
   - 顶栏三行堆叠 → 单行（品牌 font-size:0 只留 9px 色块、状态与按钮同排）
   - 页面标题 30px → 15px（`clamp(22px,6.4vw,30px)` 删掉）
   - 页签 44~46 → 38px；`.tabs` 自身 min-height 46 → 38
   - 结果：导航 **239→78px（-67%）**，内容可见 **99→222px（+124%）**
2. 修 `@media (pointer:coarse), (max-width:720px)` 误伤：拆成只有真触屏才 44px
3. 修 modelpool 顶栏 4 按钮在 390 下撑宽 18px：`.topactions` 改 wrap + min-width:0

## 判据（这次是真闸门）
新增 pane 契约（probes/pane_geometry.js）逐页签量：
- `overlap ≤ 1px`（内容不许钻进导航区）
- `tabsHeight ≥ 30`（页签条不许被压扁；38px 是窄屏有意设计，判据下限放到 30）
- **`navRatio ≤ 45%`**（导航占视口比例 —— 这才是「被挡住」的根因面）
门禁 20→23 例，新增 594×350（问题现场）档。
双向反证：v1.3.2 → FAIL（navRatio=55.1、模板页 paneTextLen=0）；v1.3.3 → PASS 23/23（navRatio=22.3%）。

## 踩坑
1. **「被挡住」≠「被遮挡」**。层叠查法是错方向，几何比例才是。以后这类反馈先量「可见比例」。
2. cdp_probe 的 `mobile: c.w<600` 会让 390 视口被算成 mobile，页面 clientWidth 可能被撑大；
   定位溢出要同时打印 `scrollWidth/clientWidth/innerWidth` 三者。
3. 探针脚本改 `--exe` 参数后，第一次测量仍读到旧值 —— 因为残留的 workbench 探针进程锁着文件，
   `go build` 失败但脚本继续跑了旧二进制。**build 一定要检查 rc**。

## 产物
- 文档 `docs/子页面被挡住-根因与修复-2026-10-20.md`
- 回执 `docs/前端门禁回执-子页遮挡-r3.md`、`docs/前端门禁回执-发布件-子页遮挡.md`
- 发布件 v1.3.3（SHA256 C92E074F…62CC3），备份 workbench.exe.bak-pre-simple5-*
- 真实窗口截图 `dist/ui-shots/app-window-v133/`
