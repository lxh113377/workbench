# 会话记录 · 首页子页面入口显形（ds 端 · 2026-10-19 23:10-23:40）

## 目标
「其它子页面的入口在哪?出现在这个页面上」（goal 新一轮）。老大附新视觉首页截图。
当轮指令高于既往口径（P0.19/#24）：旧「极简首页 = 1 按钮 + 1 隐藏后门」契约作废，入口改可见。

## 改动
- html.go indexPage：隐藏后门 `<a class="home-admin">·` 旁新增 `<nav class="home-entries">`
  三链接（管理面板/模型池/治理），各注入 token；占位符 6→8。
- page.go：indexHTML Sprintf 参数 6→8。
- assets_css.go：.home-main 改 flex-column（gap 26px）；新增 .home-entries/.home-entry
  仪器铭牌胶囊样式（hover 信号橙描边）；≤480px 收窄。
- 契约改写（3 个测试文件）：
  - index_contract_test.go：TestIndexPageMinimalContract/TestHiddenBackdoorIsDotOnly →
    TestIndexPageEntryContract（1 按钮 + 恰 3 链接 + 三 href 带 token + 禁 aria-hidden/tabindex=-1 + 禁表单）；
  - home_contract_test.go：链接数 1→3，禁止清单放行 <nav>；
  - subpage_contract_test.go：占位符 6→8。
- 前端判据：assertions.mjs HOME_DEFAULTS anchors 1→3、textVisibleLenMax 6→18；
  顺手修掉真缺陷——textVisibleLen 阈值曾硬编码 6 不读期望（自测假红暴露）。
  assertions_selftest.mjs 夹具同步（textVisibleLen 基线 2→12，篡改值 11→99 保分辨力）。

## 实测回执
- go vet ./... rc=0；go test ./... 27 包全 ok。
- frontend-gate（--exe dist\wb-entry-check.exe）19/19 PASS（gate-2026-10-19T15-29-02）。
- 隔离实例三入口逐一真实点击：admin/modelpool/governance 全部带 token 落地（pass×3）。
- 发布件换件重启（dist\workbench.exe 23:33，备份 workbench.exe.bak-pre-entrybar）；
  17890 活实例复验：三入口 visible+hit 面积达标、点「模型池」→ /modelpool?token=（pass）。
- 截图：xtmp/live-entrybar2-*/live-home.png（老大新首页形态）、dist/ui-shots/gate-*/cdp-home-desktop.png。

## 坑（记档）
1. 多层字符串传递吃转义：Go 源码里的 \" 与 \s 被 TS 模板层吃掉 → 用 fs.writeFileSync +
   反引号原始字符串 + 写完立即 go vet 验证；违 FS_STALE_VERSION 时改走 fs 直写。
2. 判据硬编码阈值（k.lte("textVisibleLen", 6) 不读 ex）= 改版必假红的定时炸弹，
   靠 assertions_selftest 抓到，已改为期望常量。
3. 换件流程：build 到 .new.exe → 停实例 → Move-Item 原子替换 → Unblock-File（MOTW）→
   Win32_Process.Create 拉起 → panel.url 确认。运行中的 exe 直接 build 会文件锁失败。
