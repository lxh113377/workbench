// tests/ui/assertions.mjs —— 探针读数 → 判据的唯一判定层（Node 侧纯函数，无 I/O、无副作用）。
//
// 为什么必须单独成层（2026-10-19 假绿缺陷）：
//   旧 cdp_probe.mjs:216 无条件写 status:"ok"，:230-232 只统计非 ok ⇒ 「探针脚本没抛异常」
//   被当成「页面达标」。于是页面横向溢出、页签半激活、console 报错、按钮越界全部判绿。
// 本层的三条硬约束：
//   1) 异常 ≠ 不达标。抛异常/超时/取不到数 ⇒ unverified（没测成）；读数不达标 ⇒ fail（测得差）。
//   2) 读不到（undefined/null/NaN/类型不符）一律 fail 并带 "missing:" 前缀，禁止「读不到 = 通过」。
//   3) 期望值以显式常量出现在报告里（result.expect），判据可被反证、可被独立复核。

// ---------------------------------------------------------------- 显式期望常量

// 首页 metrics 契约：首页就该只有「一个启动按钮 + 一个隐藏管理入口」，不得有 div/img/input，
// 文案极短（按钮标签），且按钮必须在视口内、页面不得横向滚动。
// 2026-10-19 老大指令改版：首页入口从「1 隐藏后门」变为「3 个可见子页面入口」
//（管理面板/模型池/治理）。anchors 1→3；入口文字（各 ≤4 字）计入可见文本，
// textVisibleLenMax 6→18（按钮 4 + 三入口 4+3+2=9，留余量）。
export const HOME_DEFAULTS = { buttons: 1, anchors: 3, divs: 0, images: 0, inputs: 0, textVisibleLenMax: 18, overflowX: 0 };

// 子页几何契约（2026-10-20 老大反馈「任务/产出/模板/设置子页面都被挡住了」）：
// 每个页签的 pane 首个内容块必须完整落在导航区（顶栏 + 页签条）下沿之下。
// 阈值 1px：允许亚像素舍入，但「内容钻进导航底下」一眼都算缺陷。
// navRatioMax：导航区（顶栏+页签）占视口高度上限（百分比）。
// 实测基线：1440x900 = 20.3%，1040x613 = 27.7%，594x350 修复前 68% / 修复后 35%。
// 取 45% 作为硬上限：超过就意味着「子页只剩不到一半屏」，用户会报「被挡住了」。
// minTabsHeight：窄屏档页签有意压到 38px（46→38，为省导航高度），判据只守「没被压扁」
// —— 真正的 0 高度缺陷由 minTabsHeight 下限抓（38 > 30 安全边界）。
export const PANE_GEOMETRY = { maxOverlap: 1, minPaneHeight: 80, minTabsHeight: 30, navRatioMax: 45 };

// 管理面板页签清单（2026-10-20 信息架构简化：10 → 5）。
// 老大原话「页面乱七八糟的东西太多了，不够简洁」——页签数量本身是判据，防止又加回去。
export const ADMIN_TABS = ["overview", "queue", "outputs", "templates", "settings"];

// 管理面板 hash 深链三态契约：URL 的 hash 与激活页签必须始终一致（同一真源）。
// 2026-10-20 页签收敛后，深链用例改用仍然存在的 #settings（原 #logs 已并入设置页）。
export const SUBNAV_EXPECT = {
  hashOnLoad: "#settings", tabOnLoad: "settings",
  hashAfterClick: "#queue", tabAfterClick: "queue",
  hashAfterBack: "#settings", tabAfterBack: "settings",
  activeTabs: 1, activePanes: 1,
};

// 旧书签兼容契约（2026-10-20 页签合并）：老链接 #logs / #evaluate / #workspace 等
// 必须映射到合并后的页签（settings），hash 本身保持原样不清洗 —— 不把用户的历史记录改写掉。
export const LEGACY_EXPECT = {
  hashOnLoad: "#logs", tabOnLoad: "settings",
  hashAfterClick: "#queue", tabAfterClick: "queue",
  hashAfterBack: "#logs", tabAfterBack: "settings",
  activeTabs: 1, activePanes: 1,
};

// 子界面互通契约：每个子页面顶栏必须带 token 互链（缺一即为断头路）。
export const NAV_REQUIRED = {
  admin: ["/governance?token=", "/modelpool?token=", "/?token="],
  governance: ["/admin?token=", "/modelpool?token=", "/?token="],
};

// 钉住基线（负向断言）：来源 2026-10-19 基线实测，见 docs/前端探针判据修复-2026-10-19.md §3。
// 含义：值为 0 的地方「多出来就是缺陷」；值为 1 的表是页面唯一数据面。数字漂移即 fail，须解释或改 pin。
export const PINNED = {};

// ---------------------------------------------------------------- 通用判据

export function runtimeErrorFailures(n) {
  if (typeof n === "number" && Number.isFinite(n) && n === 0) return [];
  return ["runtimeErrors=" + safe(n) + " (期望 0 = Runtime.exceptionThrown + Log.entryAdded level=error)"];
}

// ---------------------------------------------------------------- 取值与断言工具

function safe(v) {
  if (v === undefined) return "undefined";
  if (typeof v === "string") return JSON.stringify(v);
  if (typeof v === "number" && !Number.isFinite(v)) return String(v);
  try { return JSON.stringify(v); } catch (e) { return String(v); }
}

export function at(obj, path) {
  const parts = String(path).split(".");
  let cur = obj;
  for (let i = 0; i < parts.length; i++) {
    if (cur === null || cur === undefined) return undefined;
    cur = cur[parts[i]];
  }
  return cur;
}

class Checker {
  constructor(target, failures, prefix) { this.t = target; this.f = failures; this.p = prefix || ""; }
  pick(p) { return this.p ? this.p + "." + p : p; }
  bad(p, want) { this.f.push("missing:" + this.pick(p) + "=" + safe(at(this.t, p)) + " (期望 " + want + ")"); }
  neq(p, v, want) { this.f.push(this.pick(p) + "=" + safe(v) + " (期望 " + want + ")"); }
  // 读数为 getComputedStyle(...).opacity 时是字符串（如 "0.6"）——数值型字符串算有效读数，
  // 空串/NaN/null/undefined 仍按「读不到」处理（禁止读不到当通过）。
  numOf(p) {
    const v = at(this.t, p);
    if (typeof v === "number" && Number.isFinite(v)) return v;
    if (typeof v === "string" && v.trim() !== "" && Number.isFinite(Number(v))) return Number(v);
    return null;
  }
  num(p, want) { const v = this.numOf(p); if (v === null) this.bad(p, want); else if (v !== want) this.neq(p, v, want); }
  lte(p, want) { const v = this.numOf(p); if (v === null) this.bad(p, "<=" + want); else if (!(v <= want)) this.neq(p, v, "<=" + want); }
  gte(p, want) { const v = this.numOf(p); if (v === null) this.bad(p, ">=" + want); else if (!(v >= want)) this.neq(p, v, ">=" + want); }
  gt(p, want) { const v = this.numOf(p); if (v === null) this.bad(p, ">" + want); else if (!(v > want)) this.neq(p, v, ">" + want); }
  bool(p, want) { const v = at(this.t, p); if (typeof v !== "boolean") this.bad(p, want); else if (v !== want) this.neq(p, v, want); }
  eq(p, want) { const v = at(this.t, p); if (v === undefined || v === null) this.bad(p, want); else if (v !== want) this.neq(p, v, want); }
  oneOf(p, allowed) { const v = at(this.t, p); if (v === undefined || v === null) this.bad(p, allowed.join("|")); else if (allowed.indexOf(v) < 0) this.neq(p, v, allowed.join("|")); }
  emptyArr(p) { const v = at(this.t, p); if (!Array.isArray(v)) this.bad(p, "[] (空数组)"); else if (v.length !== 0) this.neq(p, v, "[] (空数组)"); }
  gteArr(p, min) { const v = at(this.t, p); if (!Array.isArray(v)) this.bad(p, "数组长度>=" + min); else if (v.length < min) this.neq(p, "长度 " + v.length, "数组长度>=" + min); }
  deepEq(p, want) { const v = at(this.t, p); if (v === undefined || v === null) this.bad(p, JSON.stringify(want)); else if (JSON.stringify(v) !== JSON.stringify(want)) this.neq(p, v, JSON.stringify(want)); }
  each(p, fn) {
    const arr = at(this.t, p);
    if (!Array.isArray(arr)) { this.bad(p, "数组"); return; }
    for (let i = 0; i < arr.length; i++) fn(new Checker(arr[i], this.f, this.pick(p) + "[" + i + "]"));
  }
  // 时间序列的「终值」与「峰值」：rings 的停止态终值 / busy 快环峰值都只存在于时间线数组里。
  lastNum(path, field, want) {
    const arr = at(this.t, path);
    if (!Array.isArray(arr) || arr.length === 0) { this.f.push("missing:" + this.pick(path) + " (期望非空数组)"); return; }
    const last = arr[arr.length - 1] || {};
    const v = last[field];
    const num = (typeof v === "number" && Number.isFinite(v)) ? v
      : (typeof v === "string" && v.trim() !== "" && Number.isFinite(Number(v)) ? Number(v) : null);
    if (num === null) this.f.push("missing:" + this.pick(path) + "[终]." + field + "=" + safe(v) + " (期望 " + want + ")");
    else if (num !== want) this.f.push(this.pick(path) + "[终]." + field + "=" + num + " (期望 " + want + ")");
  }
  maxNum(path, field, min) {
    const arr = at(this.t, path);
    if (!Array.isArray(arr) || arr.length === 0) { this.f.push("missing:" + this.pick(path) + " (期望非空数组)"); return; }
    let best = null;
    for (const row of arr) {
      const v = row && row[field];
      const num = (typeof v === "number" && Number.isFinite(v)) ? v
        : (typeof v === "string" && v.trim() !== "" && Number.isFinite(Number(v)) ? Number(v) : null);
      if (num !== null && (best === null || num > best)) best = num;
    }
    if (best === null) this.f.push("missing:" + this.pick(path) + "." + field + " (期望峰值 >" + min + ")");
    else if (!(best > min)) this.f.push(this.pick(path) + "." + field + " 峰值=" + best + " (期望 >" + min + ")");
  }
  // 页签行按 data-tab 名定位（顺序无关），用于钉住每个页签自己的读数。
  tabBaseline(name, wants) {
    const arr = at(this.t, "tabs");
    if (!Array.isArray(arr)) { this.f.push("missing:tabs (期望含 " + name + " 行的数组)"); return; }
    const row = arr.filter(x => x && x.tab === name)[0];
    if (!row) { this.f.push("missing:tabs[" + name + "] (读数里没有这个页签)"); return; }
    const keys = Object.keys(wants);
    for (let i = 0; i < keys.length; i++) {
      const key = keys[i], v = row[key];
      if (typeof v !== "number" || !Number.isFinite(v)) { this.f.push("missing:tabs[" + name + "]." + key + "=" + safe(v) + " (期望 " + wants[key] + ")"); }
      else if (v !== wants[key]) { this.f.push("tabs[" + name + "]." + key + "=" + v + " (期望 " + wants[key] + " 钉住基线)"); }
    }
  }
}

// ---------------------------------------------------------------- 各契约

/**
 * contractFailures({ contract, name, data, expect }) -> string[]（空数组 = 达标）
 * contract ∈ home | metrics | tabs | flow | rings | busy | interact | subnav | navlinks | none
 */
export function contractFailures(c) {
  const f = [];
  const data = c && c.data;
  const ex = (c && c.expect) || {};
  if (data === null || typeof data !== "object" || Array.isArray(data)) {
    f.push("data-not-object:" + safe(data) + " (探针返回值无法作为读数对象)");
    return f;
  }
  const k = new Checker(data, f, "");
  const pin = PINNED[c.name];

  switch (c.contract) {
    case "home": {
      k.num("buttons", ex.buttons);
      k.num("anchors", ex.anchors);
      k.num("divs", ex.divs);
      k.num("images", ex.images);
      k.num("inputs", ex.inputs);
      // 偏差①（2026-10-19 实测）：innerText 含 .sr-only 状态播报（基线 textLen=11），
      // 「textLen<=3」在真实页面上永不可达 ⇒ 改用 textVisibleLen（已排除 sr-only/aria-hidden/零尺寸）。
      // 阈值 6 = 最长按钮文案「重新启动」(4) 留余量；正文段落会到数十~数百字，判据仍有分辨力。
      k.lte("textVisibleLen", ex.textVisibleLenMax);
      k.num("paragraphs", 0);
      k.num("tables", 0);
      k.bool("btn.insideViewport", true);
      k.num("overflowX", ex.overflowX);
      break;
    }
    case "metrics": { // metrics 探针落在子页面（治理控制台/模型池）
      k.num("overflowX", 0);
      // 判据用 clippedH（元素越出视口左右边界）：clipped 是「视口下方元素数」，视作页面长度遥测，
      // 长页面恒 >0（基线 governance-390 clipped=145 而 overflowX=0，内容可滚动到达，无裁切）。
      k.num("clippedH", 0);
      if (pin) for (const key of Object.keys(pin)) k.num(key, pin[key]);
      break;
    }
    case "tabs": {
      k.num("finalActiveTabs", 1);
      k.num("finalActivePanes", 1);
      k.emptyArr("errors");
      k.num("overflowX", 0);
      k.gteArr("tabs", 1);
      // 页签清单必须等于简化后的 5 个（顺序无关）：数量与名字双向钉住，
      // 既防「又加回 10 个把页面塞满」，也防「删过头丢掉主入口」。
      // ex.tabs 缺省（老调用方/夹具未给期望）时只做「至少一个页签」判据，不误报。
      const wantTabs = Array.isArray(ex.tabs) ? ex.tabs : null;
      if (wantTabs && Array.isArray(data.tabs) && data.tabs.length !== wantTabs.length) {
        f.push("tabs.count=" + data.tabs.length + " (期望 " + wantTabs.length + " 个页签：" + wantTabs.join("/") + ")");
      }
      k.each("tabs", (b) => {
        b.num("activeCountWhenMeasured", 1);   // 点到该页签时，全页只允许一个 active（防半激活）
        b.num("visiblePanes", 1);
        b.emptyArr("anomalies");
      });
      if (Array.isArray(data.tabs)) {
        const names = data.tabs.map((x) => String((x && x.tab) || ""));
        for (const want of (wantTabs || [])) {
          if (names.indexOf(want) < 0) f.push("missing-tab:" + want + " (实际 " + JSON.stringify(names) + ")");
        }
        for (const got of names) {
          if (wantTabs && wantTabs.indexOf(got) < 0) f.push("unexpected-tab:" + got + " (简化后不应存在的页签)");
        }
      }
      if (pin && pin.tabBaselines) for (const name of Object.keys(pin.tabBaselines)) k.tabBaseline(name, pin.tabBaselines[name]);
      break;
    }
    case "enter": {
      // 首页入口端到端契约（2026-10-19 目标轮）：点唯一按钮必须把人送进主功能子界面。
      // 判据顺序：① 探针真的点了（clicked）；② 页面带 token（无 token 落地就是 401）；
      // ③ 最终 URL 落在 /admin?token= —— 这是「入口」承诺的全部含义，缺一即 fail。
      k.eq("clicked", true);
      k.eq("hasToken", true);
      const landed = String((data && data.landedUrl) || "");
      if (landed.indexOf("/admin?token=") < 0) f.push("landed-not-admin: 点击后 9s 内未进入 /admin（实际 " + (landed || "（从未离开首页）") + "）");
      break;
    }
    case "flow": {
      k.lte("burstStartCalls", 1);             // 连点 5 次只允许发出 1 次启动请求（防重入）
      k.oneOf("final.state", ["running", "busy"]);
      k.num("spinnerInStoppedState", 0);
      k.gteArr("transitions", 1);
      k.gteArr("statesSeen", 1);
      break;
    }
    case "rings": {
      // rings.js 的停止态终值在 stoppedTimeline 末尾（每 100ms 采样，含过渡中间值）；
      // 「停止态不显示旋转环」判的是终值，不是 500ms 那一刻的过渡值。
      k.lastNum("stoppedTimeline", "spinner", 0);
      k.lastNum("stoppedTimeline", "arc", 0);
      k.num("startCallsFromSingleManualClick", 1); // 手动兜底只允许一次启动请求
      k.gteArr("stoppedTimeline", 1);
      k.gteArr("busyTimeline", 1);
      k.maxNum("busyTimeline", "arc", 0.5);        // busy 期必须有快环（弧线峰值 >0.5）
      break;
    }
    case "busy": {
      k.eq("stableBusyState.ariaBusy", "true");
      k.gt("stableBusyState.spinner", 0.5);
      k.gteArr("busyWindow", 1);
      break;
    }
    case "interact": {
      k.emptyArr("errors");                    // 页面内 window.error / unhandledrejection
      k.gteArr("steps", 1);                    // 交互步骤真的跑了（否则是空跑假绿）
      break;
    }
    case "pane": {
      // 「子页被挡住」的几何判据：读数来自 probes/pane_geometry.js（真实浏览器逐页签点开取矩形）。
      k.emptyArr("errors");
      k.num("overflowX", 0);
      k.gteArr("panes", 1);
      // 导航区不得吃掉太多视口（这是「子页被挡住」的根因面）
      if (ex.navRatioMax !== undefined) k.lte("navRatio", ex.navRatioMax);
      k.each("panes", (b) => {
        // 页签条不得被压扁：压成 0 高会让内容跑到导航底下（这正是老大看到的「被挡住」）
        b.gte("tabsHeight", ex.minTabsHeight);
        // 每个 pane 必须真的渲染出内容，且高度正常
        b.gte("paneHeight", ex.minPaneHeight);
        b.gt("paneTextLen", 0);
        b.num("activeTabs", 1);
        b.num("visiblePanes", 1);
        // 核心：首个内容块不得钻进导航区
        b.lte("overlap", ex.maxOverlap);
      });
      break;
    }
    case "legacy": {
      // 旧页签深链不能是死路：hash 保留原值，但界面必须落到合并后的页签上。
      k.eq("hashOnLoad", LEGACY_EXPECT.hashOnLoad);
      k.eq("tabOnLoad", LEGACY_EXPECT.tabOnLoad);
      k.eq("tabFromHashOnLoad", LEGACY_EXPECT.tabOnLoad);
      k.num("activeTabsOnLoad", 1);
      k.num("activePanesOnLoad", 1);
      k.eq("hashAfterClick", LEGACY_EXPECT.hashAfterClick);
      k.eq("tabAfterClick", LEGACY_EXPECT.tabAfterClick);
      k.eq("tabAfterBack", LEGACY_EXPECT.tabAfterBack);
      k.num("activeTabsAfterBack", 1);
      k.num("overflowX", 0);
      break;
    }
    case "subnav": {
      k.eq("hashOnLoad", SUBNAV_EXPECT.hashOnLoad);
      k.eq("tabFromHashOnLoad", SUBNAV_EXPECT.tabOnLoad);
      k.eq("tabOnLoad", SUBNAV_EXPECT.tabOnLoad);
      k.num("activeTabsOnLoad", SUBNAV_EXPECT.activeTabs);
      k.num("activePanesOnLoad", SUBNAV_EXPECT.activePanes);
      k.eq("hashAfterClick", SUBNAV_EXPECT.hashAfterClick);
      k.eq("tabAfterClick", SUBNAV_EXPECT.tabAfterClick);
      k.eq("hashAfterBack", SUBNAV_EXPECT.hashAfterBack);
      k.eq("tabAfterBack", SUBNAV_EXPECT.tabAfterBack);
      k.num("activeTabsAfterBack", 1);
      k.num("activePanesAfterBack", 1);
      k.num("overflowX", 0);
      break;
    }
    case "navlinks": {
      const page = ex.page;
      k.eq("page", page);
      k.deepEq("requiredAdmin", NAV_REQUIRED.admin);        // 防止「必需清单为空 ⇒ 缺项为空」的假绿
      k.deepEq("requiredGovernance", NAV_REQUIRED.governance);
      k.gteArr("links", 1);
      const required = page === "admin" ? NAV_REQUIRED.admin : NAV_REQUIRED.governance;
      const paths = Array.isArray(data.links) ? data.links.map((l) => String((l && l.path) || "")) : [];
      for (let i = 0; i < required.length; i++) {
        const need = required[i];
        if (!paths.some((p) => p.indexOf(need) === 0)) f.push("required-link-absent:" + need + " (页面链接 " + JSON.stringify(paths) + ")");
      }
      // 只判与当前页面匹配的缺项清单：admin 页的 missingOnGovernance 天然含 "/admin?token="（不自链），
      // 拿它当判据就是「按另一页面的清单判本页」的语义错位。
      if (page === "admin") k.emptyArr("missingOnAdmin"); else k.emptyArr("missingOnGovernance");
      k.emptyArr("missingRelevant");
      // 通用（对任何子界面都成立）：必须真的落在被测实例、且渲染出了顶栏骨架。
      k.bool("landedOnTarget", true);
      k.bool("hasTopbar", true);
      // 契约专属（只对 admin）：管理面板有页签；治理控制台是独立单页，本来就没有 .tab，
      // 把它当通用期望就是「契约作用域错位」（同类缺陷第二次，上一轮是 rings 被套 spinnerInStoppedState）。
      if (page === "admin") k.gte("tabCount", 1);
      break;
    }
    case "none": break;
    default:
      f.push("unknown-contract:" + c.contract + " (没有对应判据，禁止默认通过)");
  }
  return f;
}
