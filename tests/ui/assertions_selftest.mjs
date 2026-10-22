// tests/ui/assertions_selftest.mjs —— 判据层的离线自证（不需要浏览器/工作台）。
// 两个方向都要证：
//   A. 合法读数 ⇒ failures 为空（判据不是「一律报错」的噪音制造机）
//   B. 篡改任一契约项 / 缺字段 ⇒ failures 非空且能说清哪一项（判据能被证伪）
// 运行：node tests/ui/assertions_selftest.mjs   （全绿 exit 0；任一条没抓到 | 误报 exit 1）
import { contractFailures, runtimeErrorFailures, HOME_DEFAULTS, SUBNAV_EXPECT, LEGACY_EXPECT, NAV_REQUIRED, ADMIN_TABS, PANE_GEOMETRY } from "./assertions.mjs";

const good = {
  // 2026-10-19 改版：anchors 1→3（可见子页面入口条）；textVisibleLen 基线 2→12
  //（按钮「工作台」3 + 入口「管理面板/模型池/治理」9）。
  home: { buttons: 1, anchors: 3, divs: 0, images: 0, inputs: 0, paragraphs: 0, tables: 0,
    textLen: 21, textVisibleLen: 12, overflowX: 0, btn: { insideViewport: true } },
  metrics: { overflowX: 0, clipped: 145, clippedH: 0 },
  tabs: { finalActiveTabs: 1, finalActivePanes: 1, errors: [], overflowX: 0,
    tabs: ADMIN_TABS.map((n) => ({ tab: n, activeCountWhenMeasured: 1, visiblePanes: 1, anomalies: [] })) },
  flow: { burstStartCalls: 1, final: { state: "running" }, spinnerInStoppedState: 0, transitions: [{ state: "idle" }], statesSeen: ["idle"] },
  rings: { startCallsFromSingleManualClick: 1,
    stoppedTimeline: [{ tag: "100ms", spinner: "0", arc: "0.36" }, { tag: "1400ms", spinner: "0", arc: "0" }],
    busyTimeline: [{ tag: "busy+50ms", spinner: "0", arc: "0.26" }, { tag: "busy+400ms", spinner: "0", arc: "1" }] },
  busy: { stableBusyState: { ariaBusy: "true", spinner: "1" }, busyWindow: [{ tag: "busy+100ms" }] },
  interact: { errors: [], steps: [{ step: "refresh" }] },
  subnav: { hashOnLoad: "#settings", tabFromHashOnLoad: "settings", tabOnLoad: "settings", activeTabsOnLoad: 1, activePanesOnLoad: 1,
    hashAfterClick: "#queue", tabAfterClick: "queue", hashAfterBack: "#settings", tabAfterBack: "settings",
    activeTabsAfterBack: 1, activePanesAfterBack: 1, overflowX: 0 },
  pane: { errors: [], overflowX: 0, navRatio: 20.3, panes: [
    { tab: "overview", paneHeight: 388, firstBlockTop: 313, navBottom: 183, overlap: 0, tabsHeight: 46, tabItemHeight: 46, paneTextLen: 109, activeTabs: 1, visiblePanes: 1 },
    { tab: "queue", paneHeight: 571, firstBlockTop: 209, navBottom: 183, overlap: 0, tabsHeight: 46, tabItemHeight: 46, paneTextLen: 137, activeTabs: 1, visiblePanes: 1 },
    { tab: "outputs", paneHeight: 1017, firstBlockTop: 209, navBottom: 183, overlap: 0, tabsHeight: 46, tabItemHeight: 46, paneTextLen: 3172, activeTabs: 1, visiblePanes: 1 },
    { tab: "templates", paneHeight: 843, firstBlockTop: 209, navBottom: 183, overlap: 0, tabsHeight: 46, tabItemHeight: 46, paneTextLen: 321, activeTabs: 1, visiblePanes: 1 },
    { tab: "settings", paneHeight: 1333, firstBlockTop: 209, navBottom: 183, overlap: 0, tabsHeight: 46, tabItemHeight: 46, paneTextLen: 337, activeTabs: 1, visiblePanes: 1 } ] },
  legacy: { hashOnLoad: "#logs", tabFromHashOnLoad: "settings", tabOnLoad: "settings", activeTabsOnLoad: 1, activePanesOnLoad: 1,
    hashAfterClick: "#queue", tabAfterClick: "queue", hashAfterBack: "#logs", tabAfterBack: "settings",
    activeTabsAfterBack: 1, activePanesAfterBack: 1, overflowX: 0 },
  navlinks: { page: "admin", requiredAdmin: NAV_REQUIRED.admin, requiredGovernance: NAV_REQUIRED.governance,
    links: [{ path: "/governance?token=t" }, { path: "/modelpool?token=t" }, { path: "/?token=t" }],
    missingOnAdmin: [], missingOnGovernance: ["/admin?token="], missingRelevant: [],
    landedOnTarget: true, hasTopbar: true, tabCount: 10 },
};
const expectFor = { home: HOME_DEFAULTS, subnav: SUBNAV_EXPECT, legacy: LEGACY_EXPECT, navlinks: { page: "admin" }, tabs: { tabs: ADMIN_TABS.slice() }, pane: PANE_GEOMETRY };

let bad = 0;
function want(name, cond, detail) { if (!cond) { bad++; console.log("FAIL " + name + " " + detail); } else console.log("ok   " + name); }
function fails(contract, data, expect, nm) { return contractFailures({ contract, name: nm, data, expect }); }

// A. 合法读数不得报错
for (const contract of Object.keys(good)) {
  const f = fails(contract, good[contract], expectFor[contract] || {}, "clean-" + contract);
  want("clean/" + contract, f.length === 0, JSON.stringify(f));
}

// A2. 窄屏页签 38px 是**有意设计**（46→38 省导航高度），必须判绿 —— 否则下次谁压到 38 都会假红。
// 与之相对，「被压成 0」在上面的 tamper 列表里必须判红。
{
  const narrow = { ...good.pane, panes: good.pane.panes.map((x) => ({ ...x, tabsHeight: 38 })) };
  const f = fails("pane", narrow, PANE_GEOMETRY, "clean-pane-narrow-tabs");
  want("clean/pane 窄屏页签 38px 合规", f.length === 0, JSON.stringify(f));
}

// B. 篡改必须被抓到
const tamper = [
  ["home.divs=1", "home", { ...good.home, divs: 1 }, HOME_DEFAULTS],
  ["home.btn 越界", "home", { ...good.home, btn: { insideViewport: false } }, HOME_DEFAULTS],
  ["home 缺字段", "home", { buttons: 1 }, HOME_DEFAULTS],
  ["metrics 横向溢出", "metrics", { overflowX: 37, clippedH: 0 }, {}],
  ["metrics 元素级裁切(clippedH)", "metrics", { overflowX: 0, clippedH: 3 }, {}],
  ["home sr-only 文案污染", "home", { ...good.home, textVisibleLen: 99 }, HOME_DEFAULTS],
  ["rings 停止态弧线残留(终值)", "rings", { ...good.rings, stoppedTimeline: [{ tag: "1400ms", spinner: "0", arc: "1" }] }, {}],
  ["rings busy 期无快环", "rings", { ...good.rings, busyTimeline: [{ tag: "busy+400ms", spinner: "0", arc: "0.1" }] }, {}],
  ["rings 手动兜底重入", "rings", { ...good.rings, startCallsFromSingleManualClick: 3 }, {}],
  ["tabs 半激活", "tabs", { ...good.tabs, finalActiveTabs: 2 }, {}],
  ["tabs console 报错", "tabs", { ...good.tabs, errors: ["boom"] }, {}],
  ["tabs 页签异常词", "tabs", { ...good.tabs, tabs: [{ tab: "overview", activeCountWhenMeasured: 1, visiblePanes: 1, anomalies: ["undefined"] }] }, { tabs: ADMIN_TABS.slice() }],
  // 简化后「又加回页签」必须被抓到（老大要的是简洁，不是更多入口）
  ["tabs 页签又被加回(10 个)", "tabs", { ...good.tabs, tabs: [...ADMIN_TABS, "logs", "evaluate", "workspace", "autonomy", "governance"].map((n) => ({ tab: n, activeCountWhenMeasured: 1, visiblePanes: 1, anomalies: [] })) }, { tabs: ADMIN_TABS.slice() }],
  ["tabs 丢了主入口", "tabs", { ...good.tabs, tabs: good.tabs.tabs.slice(0, 3) }, { tabs: ADMIN_TABS.slice() }],
  ["flow 重入 5 次", "flow", { ...good.flow, burstStartCalls: 5 }, {}],
  ["flow 终态 stopped", "flow", { ...good.flow, final: { state: "stopped" } }, {}],
  ["rings 停止态有旋转环(终值)", "rings", { ...good.rings, stoppedTimeline: [{ tag: "1400ms", spinner: "1", arc: "0" }] }, {}],
  ["busy ariaBusy 假", "busy", { ...good.busy, stableBusyState: { ariaBusy: "false", spinner: "1" } }, {}],
  ["busy 环不转", "busy", { ...good.busy, stableBusyState: { ariaBusy: "true", spinner: "0" } }, {}],
  ["interact 空跑", "interact", { errors: [], steps: [] }, {}],
  ["subnav back 没回 logs", "subnav", { ...good.subnav, tabAfterBack: "queue" }, SUBNAV_EXPECT],
  ["subnav 深链没生效", "subnav", { ...good.subnav, tabOnLoad: "overview", tabFromHashOnLoad: "overview" }, SUBNAV_EXPECT],
  // 子页被挡住的三种形态：内容钻进导航区 / 页签条被压成 0 高 / 某页渲染成空
  ["pane 导航区吃掉大半视口", "pane", { ...good.pane, navRatio: 68 }, PANE_GEOMETRY],
  ["pane 内容被导航区压住", "pane", { ...good.pane, panes: good.pane.panes.map((x, i) => i === 1 ? { ...x, overlap: 52, firstBlockTop: 131 } : x) }, PANE_GEOMETRY],
  ["pane 页签条被压成 0 高", "pane", { ...good.pane, panes: good.pane.panes.map((x) => ({ ...x, tabsHeight: 0, overlap: 46 })) }, PANE_GEOMETRY],
  ["pane 某页渲染为空", "pane", { ...good.pane, panes: good.pane.panes.map((x, i) => i === 3 ? { ...x, paneTextLen: 0, paneHeight: 0 } : x) }, PANE_GEOMETRY],
  ["legacy 旧书签成死路", "legacy", { ...good.legacy, tabOnLoad: "overview", tabFromHashOnLoad: "overview" }, LEGACY_EXPECT],
  ["legacy 旧书签丢 hash", "legacy", { ...good.legacy, hashOnLoad: "#settings" }, LEGACY_EXPECT],
  ["navlinks 缺治理链接", "navlinks", { ...good.navlinks, links: [{ path: "/modelpool?token=t" }, { path: "/?token=t" }] }, { page: "admin" }],
  ["navlinks 必需清单被清空", "navlinks", { ...good.navlinks, requiredAdmin: [] }, { page: "admin" }],
  ["navlinks governance 缺管理面板", "navlinks", { ...good.navlinks, page: "governance", links: [{ path: "/modelpool?token=t" }, { path: "/?token=t" }] }, { page: "governance" }],
  ["navlinks 落错页(实例到期)", "navlinks", { ...good.navlinks, landedOnTarget: false, page: "other", links: [], paths: [], missingRelevant: [], hasTopbar: false, tabCount: 0 }, { page: "admin" }],
  ["navlinks 空页无骨架", "navlinks", { ...good.navlinks, hasTopbar: false, tabCount: 0 }, { page: "admin" }],
  ["未知契约禁止默认通过", "no-such-contract", {}, {}],
];
for (const [label, contract, data, expect] of tamper) {
  const f = fails(contract, data, expect, "tampered");
  want("tamper/" + label, f.length > 0, "没抓到（判据钝）");
}
want("runtimeErrors=0 通过", runtimeErrorFailures(0).length === 0, JSON.stringify(runtimeErrorFailures(0)));
want("runtimeErrors=2 抓到", runtimeErrorFailures(2).length > 0, "没抓到");
want("runtimeErrors 缺读抓到", runtimeErrorFailures(undefined).length > 0, "没抓到");

console.log(bad === 0 ? "\n[assertions_selftest] 全部通过（判据层可证伪且不误报）" : "\n[assertions_selftest] " + bad + " 条不符");
process.exit(bad === 0 ? 0 : 1);
