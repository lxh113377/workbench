package web

// appCSS 是四个页面共用的视觉系统（设计读数见文件头注释）。
// 全部为内嵌字符串：随 EXE 发布，零外部字体/CDN 依赖。
const appCSS = `
/* ============================================================================
   个人 AI 工作台 · 视觉系统 v2 —— 「精密仪器 / Precision Instrument」
   ----------------------------------------------------------------------------
   设计读数（Design Read）
     页面类型：本地桌面工具的四页壳（开屏 / 管理台 / 模型池 / 治理台）
     气质：精密、克制、有金属感、仪器面板 —— 不是 SaaSS 营销站
     受众：每天盯 8 小时的单个重度用户
     约束：零外部依赖（不引 CDN 字体/框架）、跨 Win/mac/Linux 系统字体栈、
           四种分辨率连续缩放、prefers-reduced-motion 必须成立
   ----------------------------------------------------------------------------
   为什么不用「AI 默认审美」（蓝色渐变按钮 + 紫色辉光 + 卡片堆叠）：
     · 主色改为信号橙 #ff6a3d（唯一高饱和色），只用于「可操作 / 活跃」；
     · 数字与拉丁字符用 Bahnschrift（Windows 自带的 DIN 级窄体可变字体），
       中文回落微软雅黑 —— 混排层次立刻与系统默认 UI 拉开；
     · 结构靠 1px 发丝线 + 留白，不靠大圆角卡片与阴影堆叠；
     · 动效只做三件事：状态呼吸、按下反馈、页签滑杆；其余一律不动。
   ========================================================================== */

:root{
  /* ——— 表面：冷蓝黑（原暖黑 #07080a → 冷调 #04070d），科技感的底色必须偏蓝 ——— */
  --bg:#04070d;
  --bg2:#070b13;
  --ink:#020409;
  --panel:#0a1018;
  --panel2:#0e151f;
  --panel3:#141d2a;
  /* ——— 发丝线：冷白（原纯白 rgba → 带蓝的冷白），线条在冷底上更「锐」 ——— */
  --line:rgba(120,190,255,.09);
  --line-2:rgba(120,190,255,.17);
  --line-3:rgba(140,210,255,.30);
  /* ——— 文本：冷白（原骨白 f1efe9 偏暖 → 冷白 ecf4ff），与冷底统一 ——— */
  --fg:#ecf4ff;
  --fg-2:#c3d4e8;
  --dim:#8fa6c0;        /* 对 --panel 对比度 ≈ 6.6:1 */
  --dim-2:#7189a4;      /* ≈ 4.6:1，仍过 AA */
  --muted:#8fa6c0;
  /* ——— 信号色：电光青为主（原暖橙 #ff6a3d → 电青 #22d3ee），紫为副信号 ——— */
  --accent:#22d3ee;
  --accent-soft:rgba(var(--accent-rgb),.14);
  --accent-2:#4ade80;    /* 成功/运行（青绿） */
  --accent-3:#a78bfa;    /* 副信号：紫（科技感的第二个记忆色） */
  --warn:#ffc53d;
  --warn-rgb:255,197,61;
  --danger:#ff5c7a;
  --danger-rgb:255,92,122;
  --accent-rgb:34,211,238;
  /* ——— 光效令牌：HUD 的「发光」统一从这里取，避免各处硬编码 ——— */
  --glow-cyan:0 0 0 1px rgba(var(--accent-rgb),.35),0 0 28px -8px rgba(var(--accent-rgb),.55);
  --glow-cyan-soft:0 0 60px -28px rgba(var(--accent-rgb),.45);
  --glow-violet:0 0 28px -10px rgba(167,139,250,.5);
  --scanline:rgba(var(--accent-rgb),.055);
  --grid-line:rgba(120,190,255,.055);
  /* ——— 形状：切角（HUD 面板的角不是圆的，是斜切的） ——— */
  --radius:12px;
  --radius-sm:9px;
  --radius-xs:7px;
  --cut:12px;            /* clip-path 切角尺寸 */
  /* ——— 字体 ——— */
  --font-display:"Bahnschrift SemiCondensed","Bahnschrift","Segoe UI Variable Display",
                 "Segoe UI Semibold","Microsoft YaHei UI","Microsoft YaHei",sans-serif;
  --font-body:"Segoe UI Variable Text","Segoe UI","Microsoft YaHei UI","Microsoft YaHei",
              "PingFang SC","Noto Sans CJK SC",system-ui,sans-serif;
  --font-mono:"Cascadia Mono","Cascadia Code",Consolas,"SFMono-Regular",
              "Microsoft YaHei UI",monospace;
  /* ——— 类型标度（8 档，下限 11px） ——— */
  --fs-micro:11px; --fs-mono:11.5px; --fs-small:12px; --fs-base:13px;
  --fs-lead:15px; --fs-kpi:29px;
  /* ——— 节奏 ——— */
  --sp-1:4px; --sp-2:8px; --sp-3:12px; --sp-4:16px; --sp-5:22px; --sp-6:30px; --sp-7:44px;
  --ease:cubic-bezier(.2,.72,.24,1);
}

*{box-sizing:border-box}
html,body{margin:0;padding:0;height:100%}
html{color-scheme:dark}
body{
  background:var(--bg);
  color:var(--fg);
  font:15px/1.6 var(--font-body);
  -webkit-font-smoothing:antialiased;
  text-rendering:optimizeLegibility;
  font-feature-settings:"kern" 1;
  /* 顶部冷极光：科技感的「环境光」，只在中上部可见，不干扰读数 */
  background-image:
    radial-gradient(1200px 520px at 50% -10%,rgba(var(--accent-rgb),.075),transparent 62%),
    radial-gradient(760px 380px at 82% -6%,rgba(167,139,250,.055),transparent 60%),
    repeating-linear-gradient(90deg,var(--grid-line) 0 1px,transparent 1px 56px),
    repeating-linear-gradient(180deg,var(--grid-line) 0 1px,transparent 1px 56px);
  background-attachment:fixed,fixed,fixed,fixed;
}
::selection{background:rgba(var(--accent-rgb),.32);color:#fff}
button{font-family:inherit;font-size:inherit;cursor:pointer;color:inherit}
a{color:inherit;text-decoration:none}
:focus-visible{outline:2px solid var(--accent);outline-offset:3px}
/* 细滚动条：仪器面板的一部分，不做成 macOS 那种消失式 */
*::-webkit-scrollbar{width:10px;height:10px}
*::-webkit-scrollbar-track{background:transparent}
*::-webkit-scrollbar-thumb{background:rgba(255,255,255,.1);border-radius:20px;border:3px solid transparent;background-clip:content-box}
*::-webkit-scrollbar-thumb:hover{background:rgba(var(--accent-rgb),.42);background-clip:content-box}

/* ============================================================ 开屏页 ==== */
/* 整页只有一个交互元素。所有气氛都只能由 CSS 图层承担（DOM 契约禁止新增可见节点）。
   构图：蓝图坐标纸（网格）→ 雷达同心环 → 中心暖光 → 唯一的按钮。
   层次由「亮度递减 + 尺度递增」建立：越靠中心越亮、越密；越往外越暗、越疏。 */
body.home{
  display:flex;align-items:center;justify-content:center;overflow:hidden;
  /* 冷色纵深：中心最亮处偏青，四周压到近黑（科技感的「看向深处」） */
  background:
    radial-gradient(1100px 760px at 50% 46%,#081625 0%,#050b14 44%,#02040a 100%);
}
/* 蓝图网格：工程坐标纸，只在中景可见，边缘被遮罩吃掉 */
.home-main::before{
  content:'';position:fixed;inset:0;pointer-events:none;z-index:0;
  background-image:
    repeating-linear-gradient(90deg,rgba(120,190,255,.10) 0 1px,transparent 1px 72px),
    repeating-linear-gradient(180deg,rgba(120,190,255,.10) 0 1px,transparent 1px 72px),
    repeating-linear-gradient(90deg,rgba(120,190,255,.04) 0 1px,transparent 1px 18px),
    repeating-linear-gradient(180deg,rgba(120,190,255,.04) 0 1px,transparent 1px 18px);
  -webkit-mask-image:radial-gradient(closest-side at 50% 48%,#000 0%,rgba(0,0,0,.6) 50%,transparent 76%);
          mask-image:radial-gradient(closest-side at 50% 48%,#000 0%,rgba(0,0,0,.6) 50%,transparent 76%);
  opacity:.75;
}
/* 雷达同心环 + 中心暖光：环距 132px，向心衰减，给静止画面一个「量度」 */
body.home::before{
  content:'';position:fixed;inset:0;pointer-events:none;z-index:0;
  background:
    /* 中心冷光 */
    radial-gradient(520px 380px at 50% 50%,rgba(var(--accent-rgb),.20),rgba(var(--accent-rgb),0) 68%),
    /* 水平光带：一条极淡的横向辉光，像屏幕正在扫描（比斜向锥形更稳、不抢主体） */
    linear-gradient(180deg,transparent 46%,rgba(var(--accent-rgb),.07) 50%,transparent 54%),
    /* 同心环 */
    repeating-radial-gradient(circle at 50% 50%,rgba(130,200,255,.13) 0 1px,transparent 1px 132px);
  -webkit-mask-image:radial-gradient(circle at 50% 50%,#000 0%,rgba(0,0,0,.8) 40%,transparent 70%);
          mask-image:radial-gradient(circle at 50% 50%,#000 0%,rgba(0,0,0,.8) 40%,transparent 70%);
  transform-origin:50% 50%;
  /* conic 扇面无法自转（伪元素已用尽、DOM 契约禁止新增节点），
     改为「亮度呼吸 + 扇面角度由关键帧切换」不可行 ⇒ 保留静态扇面，让整体做极缓慢明暗呼吸。
     入场第一段（环境光从景深亮起）与常驻 bloom 必须写在同一个 animation 列表里：
     拆成两条声明会让后者整体覆盖前者（走查实测：入场被静默关掉）。 */
  animation:homeStageEnv 1.5s var(--ease) both,
            bloom 12s ease-in-out 1.5s infinite alternate;
}
/* 胶片颗粒：内联 SVG 噪声，零外部请求；强度压到几乎只在暗部可感 */
body.home::after{
  content:'';position:fixed;inset:0;pointer-events:none;z-index:3;opacity:.035;
  background-image:url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='140' height='140'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='.9' numOctaves='3'/%3E%3C/filter%3E%3Crect width='140' height='140' filter='url(%23n)' opacity='.55'/%3E%3C/svg%3E");
  mix-blend-mode:overlay;
}
.home-main{
  position:relative;z-index:2;width:100%;height:100%;
  display:flex;flex-direction:column;align-items:center;justify-content:center;gap:24px;
}
/* 宽屏取景框：发丝内框把唯一主体托住（只在足够大的画布上加，窄屏会显得局促） */
.home-main::after{
  /* HUD 取景框：四角 L 形定位标 + 上下边刻度尺。
     全部用 background-image 的多重 linear-gradient 画出来（零额外 DOM，符合首页极简契约）。
     小屏隐藏：太小的画布上这些细节会变成噪点。
     品牌铭牌也落在这一层（首页第一屏必须「品牌 unmistakable」；伪元素文字
     不进 TreeWalker 的文本节点计数，极简契约不受影响）。 */
  content:'个人 AI 工作台';
  position:absolute;inset:26px;pointer-events:none;z-index:1;
  font-family:var(--font-display);font-size:var(--fs-micro);font-weight:600;
  letter-spacing:.34em;color:var(--dim);white-space:nowrap;
  padding:16px 0 0 44px;
  --corner:26px;      /* 角标臂长 */
  --tw:1.5px;         /* 线宽 */
  background-image:
    /* 左上角 L */
    linear-gradient(90deg,var(--accent) 0 var(--corner),transparent var(--corner)),
    linear-gradient(180deg,var(--accent) 0 var(--corner),transparent var(--corner)),
    /* 右上角 L */
    linear-gradient(270deg,var(--accent) 0 var(--corner),transparent var(--corner)),
    linear-gradient(180deg,var(--accent) 0 var(--corner),transparent var(--corner)),
    /* 左下角 L */
    linear-gradient(90deg,var(--accent) 0 var(--corner),transparent var(--corner)),
    linear-gradient(0deg,var(--accent) 0 var(--corner),transparent var(--corner)),
    /* 右下角 L */
    linear-gradient(270deg,var(--accent) 0 var(--corner),transparent var(--corner)),
    linear-gradient(0deg,var(--accent) 0 var(--corner),transparent var(--corner)),
    /* 上边刻度尺 */
    repeating-linear-gradient(90deg,rgba(130,200,255,.5) 0 var(--tw),transparent var(--tw) 46px),
    /* 下边刻度尺 */
    repeating-linear-gradient(90deg,rgba(130,200,255,.35) 0 var(--tw),transparent var(--tw) 46px);
  background-repeat:no-repeat,no-repeat,no-repeat,no-repeat,no-repeat,no-repeat,no-repeat,no-repeat,no-repeat,no-repeat;
  background-size:
    var(--corner) var(--tw), var(--tw) var(--corner),      /* 左上 */
    var(--corner) var(--tw), var(--tw) var(--corner),      /* 右上 */
    var(--corner) var(--tw), var(--tw) var(--corner),      /* 左下 */
    var(--corner) var(--tw), var(--tw) var(--corner),      /* 右下 */
    100% 7px, 100% 5px;
  background-position:
    left top, left top,
    right top, right top,
    left bottom, left bottom,
    right bottom, right bottom,
    left top, left bottom;
  opacity:.85;
  filter:drop-shadow(0 0 6px rgba(var(--accent-rgb),.55));
}
/* 刻度尺与角标只在有足够空间时显示，小屏关掉（否则变成噪点）；
   但品牌铭牌必须留下 —— 第一屏「品牌 unmistakable」不分端。 */
@media (max-width:900px){
  .home-main::after{
    inset:0;background-image:none;
    padding:16px 0 0 16px;letter-spacing:.26em;font-size:10.5px;
  }
}
.sr-only{
  position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;
  clip:rect(0 0 0 0);white-space:nowrap;border:0;
}

/* —— 唯一的交互元素：一块「通电的按键」 ——
   旋转渐变边框（@property 驱动）+ 双发光晕（状态色）+ 内斜面。
   @property 不被支持时，conic 停在 0deg，退化成静态渐变边框，不破坏布局。 */
@property --ang{syntax:'<angle>';inherits:false;initial-value:0deg}
.start-btn{
  --ring-1:rgba(var(--accent-rgb),0);
  --ring-2:rgba(var(--accent-rgb),.66);
  --ring-3:rgba(255,255,255,.3);
  --glow:rgba(var(--accent-rgb),.34);
  position:relative;isolation:isolate;
  /* 主体尺度随画布呼吸（2026-10-22 超宽屏走查实测）：原固定 320px 在 3440 宽下
     主体占比塌到 9%，取景框与雷达环都在放大、唯独主体不变 ⇒ 「仪器里的小按钮」。
     26vw / 上限 460 / 下限 320 的实测阶梯（CDP 逐档读 getBoundingClientRect）：
       390→320(82%)  1024→320(31%)  1440→374(26%)  1920→460(24%)  2560→460(18%)
     系数取 26 而非 16：16vw 在 1440 只有 230px，被 320 下限兜住 ⇒ 1440-1920 整段
     是「死区」（占比从 22% 掉到 17% 却一点没放大）。窄屏由下限 + 78vw 兜，零影响。 */
  width:clamp(320px,26vw,460px);min-width:min(260px,72vw);
  min-height:clamp(120px,11vw,164px);padding:0 clamp(30px,2.4vw,42px);
  display:flex;align-items:center;justify-content:center;gap:14px;
  border:1px solid transparent;border-radius:0;
  /* 切角「能量核心」：clip-path 切四角 + 双层背景（内芯渐变 + 巡游边框） */
  clip-path:polygon(18px 0,calc(100% - 18px) 0,100% 18px,100% calc(100% - 18px),calc(100% - 18px) 100%,18px 100%,0 calc(100% - 18px),0 18px);
  background:
    linear-gradient(168deg,#0b2230 0%,#071620 56%,#04101a 100%) padding-box,
    conic-gradient(from var(--ang),var(--ring-1) 0deg,var(--ring-2) 84deg,var(--ring-3) 168deg,var(--ring-1) 262deg,var(--ring-2) 322deg,var(--ring-1) 360deg) border-box;
  color:var(--fg);
  font-family:var(--font-display);font-weight:600;
  font-size:clamp(21px,4.4vw,29px);letter-spacing:.22em;
  text-shadow:0 1px 0 rgba(0,0,0,.6);
  box-shadow:
    0 46px 110px -40px rgba(0,0,0,.98),
    0 0 0 1px rgba(0,0,0,.55),
    0 2px 0 rgba(255,255,255,.05) inset,
    0 -18px 44px -22px var(--glow) inset,
    0 0 120px -46px var(--glow);
  /* 动画顺序即优先级：animation-play-state:paused（hover 时）只暂停**列表第一个**动画，
     所以常驻的 respire 必须放第一位 —— 入场放最后，用户在入场途中 hover 不会把推近卡死。
     入场 .28s 起、.92s 长 ⇒ 1.2s 结束，与 respire/angspin 的 1.2s delay 精确衔接。*/
  animation:respire 7s ease-in-out 1.2s infinite,
            angspin 9s linear 1.2s infinite,
            homeStageHero .92s var(--ease) .28s both;
  transition:transform .3s var(--ease),letter-spacing .32s var(--ease),
             box-shadow .3s var(--ease),color .2s var(--ease);
  touch-action:manipulation;-webkit-tap-highlight-color:transparent;
}
/* 顶部信号灯：一段横向发丝线，像面板上的电源指示 */
.start-btn::before{
  /* 顶部全宽能量条：HUD 的「通电」指示，比原来那截短线更有存在感 */
  content:'';position:absolute;top:0;left:0;right:0;height:2px;
  background:linear-gradient(90deg,transparent 4%,var(--accent) 50%,transparent 96%);
  filter:drop-shadow(0 0 6px var(--accent));
  opacity:.9;transition:opacity .3s var(--ease);
}
/* 上缘高光：让平面玻璃有厚度 */
.start-btn::after{
  /* 内部微网格 + 上缘高光：让它看起来像一块「有电路的面板」而不是一块渐变玻璃 */
  content:'';position:absolute;inset:1px;pointer-events:none;
  background:
    repeating-linear-gradient(90deg,rgba(120,200,255,.055) 0 1px,transparent 1px 14px),
    repeating-linear-gradient(180deg,rgba(120,200,255,.055) 0 1px,transparent 1px 14px),
    linear-gradient(180deg,rgba(255,255,255,.10),rgba(255,255,255,0) 46%);
  mix-blend-mode:screen;
  opacity:.6;
}
.start-btn:hover{
  transform:translateY(-3px);
  letter-spacing:.26em;
  --ring-2:rgba(var(--accent-rgb),.95);
  --glow:rgba(var(--accent-rgb),.44);
  box-shadow:
    0 54px 120px -38px rgba(0,0,0,.98),
    0 0 0 1px rgba(0,0,0,.55),
    0 2px 0 rgba(255,255,255,.07) inset,
    0 -18px 54px -20px var(--glow) inset,
    0 0 80px -24px rgba(var(--accent-rgb),.46);
  animation-play-state:paused;
}
.start-btn:hover::before{opacity:1;width:64%}
.start-btn:active{transform:translateY(-1px) scale(.994);transition-duration:.09s}
.start-btn:focus-visible{outline:2px solid rgba(var(--accent-rgb),.92);outline-offset:8px}
.start-btn:disabled{cursor:progress}
.start-btn[data-state=running]:disabled{cursor:default}

/* 状态：颜色只表达语义（文案与 aria 由脚本写），围栏色/内发光随之切换 */
.start-btn[data-state=busy]{--ring-2:rgba(var(--accent-rgb),.82);--glow:rgba(var(--accent-rgb),.36);animation:angspin 3.2s linear infinite}
.start-btn[data-state=running]{
  --ring-2:rgba(88,224,192,.72);--ring-3:rgba(88,224,192,.28);--glow:rgba(88,224,192,.26);
  animation:angspin 14s linear infinite;
}
.start-btn[data-state=paused]{
  --ring-2:rgba(var(--warn-rgb),.74);--ring-3:rgba(var(--warn-rgb),.32);--glow:rgba(var(--warn-rgb),.24);
  animation:none;
}
.start-btn[data-state=stopped]{
  --ring-2:rgba(255,255,255,.26);--ring-3:rgba(255,255,255,.12);--glow:rgba(255,255,255,.05);
  animation:none;
}
.start-btn[data-state=error]{
  --ring-2:rgba(var(--danger-rgb),.9);--ring-3:rgba(var(--danger-rgb),.32);--glow:rgba(var(--danger-rgb),.32);
  background:
    linear-gradient(168deg,#1a1018 0%,#0e0a12 56%,#07050b 100%) padding-box,
    conic-gradient(from var(--ang),var(--ring-1) 0deg,var(--ring-2) 90deg,var(--ring-3) 180deg,var(--ring-1) 270deg,var(--ring-2) 330deg,var(--ring-1) 360deg) border-box;
  animation:angspin 5s linear infinite;
}

/* 两个加载环：同一个旋转语法，速度区分「正在做事」与「持续在跑」 */
.btn-load{position:absolute;width:22px;height:22px;border-radius:50%;opacity:0;transition:opacity .2s ease}
.btn-spinner{
  left:24px;border:1.5px solid rgba(255,255,255,.16);border-top-color:var(--accent);
  animation:btnspin .8s linear infinite;
}
.btn-arc{
  right:26px;width:24px;height:24px;
  border:1px solid rgba(255,255,255,.1);border-top-color:var(--accent-2);
  animation:btnspin 3.6s linear infinite;
}
/* 只在真实「加载中」显左环、真实「运行中」显右环 —— 状态反馈必须是诚实的 */
.start-btn[data-state=busy] .btn-spinner,
.start-btn[data-state=running] .btn-arc{opacity:1}
.start-btn[data-state=paused] .btn-load{visibility:hidden}
@keyframes btnspin{to{transform:rotate(360deg)}}
@keyframes angspin{to{--ang:360deg}}
@keyframes respire{
  0%,100%{box-shadow:0 46px 110px -40px rgba(0,0,0,.98),0 0 0 1px rgba(0,0,0,.55),0 2px 0 rgba(255,255,255,.05) inset,0 -18px 44px -22px rgba(var(--accent-rgb),.32) inset}
  50%{box-shadow:0 50px 120px -38px rgba(0,0,0,.98),0 0 0 1px rgba(0,0,0,.55),0 2px 0 rgba(255,255,255,.07) inset,0 -18px 52px -20px rgba(var(--accent-rgb),.44) inset}
}
@keyframes bloom{from{opacity:.72}to{opacity:1}}
/* ── 首页入场序列（2026-10-21 获奖级走查）──────────────────────────────
   获奖级第一屏的标准是「镜头推进」而不是「东西都在那儿」：环境光先亮起，
   主体从景深里推近并点亮，最后入口条落位。三段共用一条 1.5s 时间轴，
   全部只驱动 transform/opacity/filter（合成层属性，不触发 layout）。
   受极简契约限制不能新增 DOM，因此由 CSS animation 直接驱动现有元素。
   prefers-reduced-motion 下由全局 reduce 规则退化为静态（契约已覆盖）。 */
@keyframes homeStageEnv{
  from{opacity:0;filter:blur(6px)}
  to{opacity:1;filter:blur(0)}
}
@keyframes homeStageHero{
  0%{opacity:0;transform:translateY(14px) scale(.965);filter:blur(10px)}
  62%{opacity:1;filter:blur(0)}
  100%{opacity:1;transform:none;filter:blur(0)}
}
@keyframes homeStageEntries{
  from{opacity:0;transform:translateY(10px)}
  to{opacity:1;transform:none}
}

/* 隐藏式后门（配置面可达但不占视觉）：右下角一个点，悬停/聚焦才显形 */
/* 子页面入口条（2026-10-19 老大指令：入口直接出现在首页上）。
   仪器铭牌风格：发丝线框 + 等距小字，克制不抢主按钮的视觉中心。 */
.home-entries{
  /* 入场第三段：入口条在主按钮落定后落位（.62s 起，与主体推进错峰） */
  animation:homeStageEntries .6s var(--ease) .62s both;
  /* 入口带：与主按钮同宽，形成一条稳定的纵向中轴（原先是三个孤立胶囊，重心散）。
     视觉重量刻意压到主按钮的 1/8：发丝线分隔 + 微型字距 + 无背景块。 */
  /* 与主按钮同宽：中轴对齐是这条入口带的存在理由（宽屏下一起放大） */
  width:clamp(320px,26vw,460px);min-width:min(260px,72vw);
  display:grid;grid-template-columns:repeat(3,minmax(0,1fr));
  gap:0;z-index:4;
  border:1px solid var(--line);
  border-radius:14px;
  background:rgba(255,255,255,.016);
  overflow:hidden;
  -webkit-backdrop-filter:blur(6px);
          backdrop-filter:blur(6px);
}
.home-entry{
  position:relative;overflow:hidden;
  display:flex;align-items:center;justify-content:center;gap:6px;
  min-height:clamp(46px,3.6vw,58px);padding:0 8px;
  font-family:var(--font-display);font-size:clamp(12.5px,1vw,14px);font-weight:500;
  letter-spacing:.14em;color:var(--dim);text-decoration:none;
  border-left:1px solid var(--line);
  transition:color .22s var(--ease),background .22s var(--ease),letter-spacing .22s var(--ease);
}
.home-entry:first-child{border-left:0}

/* 悬停：信号色从左缘掠过（一条 2px 竖条从 0 长到满高），文字提亮、字距微张 */
.home-entry::before{
  content:'';position:absolute;left:0;top:50%;translate:0 -50%;
  width:2px;height:0;border-radius:2px;background:var(--accent);
  transition:height .26s var(--ease);
}
.home-entry:hover,.home-entry:focus-visible{
  color:var(--fg);background:rgba(var(--accent-rgb),.08);letter-spacing:.17em;outline:none;
}
.home-entry:hover::before,.home-entry:focus-visible::before{height:62%}
.home-entry:focus-visible{box-shadow:inset 0 0 0 1px rgba(var(--accent-rgb),.6)}
/* 窄屏：一行三格在 390px 下每格仅 ~110px，字距一放就挤。
   改为整宽三行（触屏 46px 高基线不变），顺序与原入口一致。 */
@media (max-width:480px){
  .home-entries{width:clamp(320px,26vw,460px);grid-template-columns:1fr}
  .home-entry{min-height:48px;border-left:0;border-top:1px solid var(--line);font-size:var(--fs-base)}
  .home-entry:first-child{border-top:0}
  .home-entry::before{left:0;top:0;translate:0 0;width:0;height:2px;transition:width .26s var(--ease)}
  .home-entry:hover::before,.home-entry:focus-visible::before{width:100%;height:2px}
}
.home-admin{
  position:absolute;bottom:18px;right:20px;z-index:4;
  font-size:var(--fs-micro);line-height:1;color:transparent;
  padding:10px 12px;border-radius:999px;
  transition:color .2s ease,background .2s ease,box-shadow .2s ease;
}
.home-admin:hover,.home-admin:focus-visible{
  color:var(--dim);background:rgba(255,255,255,.05);box-shadow:0 0 0 1px var(--line-2);outline:none;
}

/* ============================================================ 应用壳 ==== */
body.admin{
  display:flex;flex-direction:column;
  background:
    radial-gradient(1200px 520px at 76% -10%,rgba(var(--accent-rgb),.07),transparent 62%),
    radial-gradient(900px 420px at 4% -4%,rgba(88,224,192,.045),transparent 60%),
    var(--bg);
}
/* 顶栏外壳：命令区（topbar）+ 页面识别区（pagebar）共用一条吸顶壳 */
.topbar-shell{
  position:sticky;top:0;z-index:20;
  background:rgba(4,7,13,.88);
  -webkit-backdrop-filter:blur(18px) saturate(1.4);
          backdrop-filter:blur(18px) saturate(1.4);
  border-bottom:1px solid var(--line-2);
}
/* 顶栏底缘的数据流：一段冷光沿水平缓慢巡游，暗示「系统在运行」 */
.topbar-shell::after{
  content:'';position:absolute;left:0;right:0;bottom:-1px;height:1px;
  background:linear-gradient(90deg,transparent,rgba(var(--accent-rgb),.85),transparent);
  background-size:38% 100%;background-repeat:no-repeat;
  animation:dataflow 7s linear infinite;
  opacity:.75;
}
@keyframes dataflow{
  0%{background-position:-40% 0}
  100%{background-position:140% 0}
}
/* 顶栏：把「设备状态」压在一条发丝线上，像机架面板 */
.topbar{
  position:relative;z-index:5;
  display:flex;align-items:center;gap:18px;
  padding:13px 26px;
  background:transparent;
  max-width:1320px;margin:0 auto;
  border-bottom:0;
}
@supports not ((backdrop-filter:blur(1px)) or (-webkit-backdrop-filter:blur(1px))){
  .topbar-shell{background:#0b0d10}
}
/* 页面识别区：一个巨大到几乎充满视线的静态标题 + 细窄说明，
   —— 面板的「重量」主要落在这里，而不是靠更多卡片。 */
.pagebar{
  display:flex;align-items:baseline;gap:16px;flex-wrap:wrap;
  max-width:1320px;margin:0 auto;padding:26px 30px 6px;
}
.pagecrumb{
  display:inline-flex;align-items:center;gap:6px;
  font-family:var(--font-mono);font-size:var(--fs-micro);letter-spacing:.18em;
  text-transform:uppercase;color:var(--dim-2);
}
.pagecrumb::after{content:'/';color:var(--line-3)}
.pagetitle{
  /* h1 语义 + span 外观（2026-10-23 Awwwards 内容维度验收）：
     页面主标题此前是 <span class="pagetitle">，视觉是大标题、语义上**没有任何层级** ——
     全站 h1 数为 0，屏幕阅读器用户无法用标题导航定位主标题（这是无障碍核心导航方式）。
     改用 <h1>，并在此重置 h1 默认 margin（.pagebar 是 flex 容器，默认 margin 会破坏基线对齐）。 */
  margin:0;font-family:var(--font-display);font-size:clamp(26px,3.4vw,40px);
  font-weight:600;line-height:1.05;letter-spacing:.02em;color:var(--fg);
}
.pagesub{
  font-family:var(--font-mono);font-size:var(--fs-micro);letter-spacing:.16em;
  text-transform:uppercase;color:var(--accent);opacity:.85;
}
/* 顶栏之后的内容区上边距：无页签的页面（模型池/治理台）topbar-shell 与 .content 相邻，
   14px 即可；管理面板中间隔了 .tabs，「topbar-shell + content」是相邻兄弟选择器、
   匹配不到，会回落到 26px —— 于是同一产品里「有页签的页比无页签的页多 12px 顶距」。
   2026-10-22 跨页一致性审计实测：admin content.top=183 / 另两页 137。
   补一条「topbar-shell 后面隔着 tabs 也要收紧」的规则，让三页顶距同值。 */
.pagebar + .content, .topbar-shell + .content,
.tabs + .content,               /* 管理面板：content 紧跟页签条 */
.topbar-shell ~ .content{       /* 兜底：只要 content 在 shell 之后同层，都是 14px */
  padding-top:14px;
}
.brand{
  display:flex;align-items:center;gap:10px;
  min-height:36px;padding:0 2px;
  border:0;background:transparent;cursor:pointer;
  font-family:var(--font-display);font-size:14px;font-weight:600;
  letter-spacing:.14em;color:var(--fg);white-space:nowrap;
}
.brand::before{
  content:'';width:9px;height:9px;flex:0 0 auto;border-radius:2px;
  background:var(--accent);
  box-shadow:0 0 0 3px rgba(var(--accent-rgb),.16),0 0 16px rgba(var(--accent-rgb),.55);
}
.topstat{
  flex:1;min-width:0;
  font-family:var(--font-mono);font-size:var(--fs-small);line-height:1.5;
  color:var(--dim);letter-spacing:.02em;
  overflow:hidden;text-overflow:ellipsis;white-space:nowrap;
}
.topstat b{color:var(--fg-2);font-weight:600}
.topactions{display:flex;gap:8px;flex-wrap:wrap;justify-content:flex-end}

/* —— 按钮：三种重量（primary / 默认 / ghost），全部 34px 高 —— */
.btn{
  display:inline-flex;align-items:center;justify-content:center;gap:7px;
  min-height:36px;padding:0 14px;
  border:1px solid var(--line-2);border-radius:var(--radius-xs);
  background:linear-gradient(180deg,rgba(255,255,255,.05),rgba(255,255,255,.015)),var(--panel2);
  color:var(--fg-2);
  font-family:var(--font-body);font-size:var(--fs-base);font-weight:500;letter-spacing:.02em;
  transition:background .16s var(--ease),border-color .16s var(--ease),
             color .16s var(--ease),transform .12s var(--ease),box-shadow .16s var(--ease);
  white-space:nowrap;
}
.btn:hover{border-color:var(--line-3);color:var(--fg);background:linear-gradient(180deg,rgba(255,255,255,.08),rgba(255,255,255,.02)),var(--panel3)}
.btn:active{transform:translateY(1px)}
.btn:disabled{opacity:.45;cursor:not-allowed;transform:none}
.btn.primary{
  background:linear-gradient(180deg,#5ce6f7,#1fb4d8);
  border-color:rgba(120,235,255,.75);color:#02222b;
  font-weight:600;
  box-shadow:0 10px 30px -14px rgba(var(--accent-rgb),.8),inset 0 1px 0 rgba(255,255,255,.45);
}
.btn.primary:hover{filter:brightness(1.07);color:#011a21;border-color:rgba(150,245,255,.95)}
.btn.danger{
  color:#ffc2d0;border-color:rgba(var(--danger-rgb),.34);
  background:linear-gradient(180deg,rgba(var(--danger-rgb),.14),rgba(var(--danger-rgb),.06));
}
.btn.danger:hover{color:#fff;border-color:rgba(var(--danger-rgb),.65);background:linear-gradient(180deg,rgba(var(--danger-rgb),.22),rgba(var(--danger-rgb),.1))}
.btn.ghost{background:transparent;border-color:var(--line-2);color:var(--dim)}
.btn.ghost:hover{color:var(--fg);border-color:var(--line-3);background:rgba(255,255,255,.04)}
.btn.sm{min-height:36px;padding:0 12px;font-size:var(--fs-small);border-radius:7px}
/* 两段式确认的「待确认」态：按钮自身变红并改写文案 —— 破坏性操作的第二击
   必须在视觉上与第一击明确区分（原生 confirm 的系统灰框被换成本体系内的状态）。 */
.btn.armed{
  color:#fff;border-color:rgba(var(--danger-rgb),.72);
  background:linear-gradient(180deg,rgba(var(--danger-rgb),.34),rgba(var(--danger-rgb),.18));
  box-shadow:0 0 0 1px rgba(var(--danger-rgb),.35),0 0 22px -8px rgba(var(--danger-rgb),.6);
  animation:armPulse 1.1s ease-in-out infinite alternate;
}
@keyframes armPulse{from{box-shadow:0 0 0 1px rgba(var(--danger-rgb),.3),0 0 16px -10px rgba(var(--danger-rgb),.5)}to{box-shadow:0 0 0 1px rgba(var(--danger-rgb),.55),0 0 26px -6px rgba(var(--danger-rgb),.7)}}

/* —— 页签：滑杆式下划线，只有一条在动 —— */
.tabs{
  /* 关键（2026-10-20 实测缺陷）：body.admin 是 flex column，而本元素 overflow-x:auto
     ⇒ 按 flexbox 规范，overflow 非 visible 的 item 自动最小尺寸(min-height:auto)变成 0，
       页签条会被压成 h=0，46px 的页签内容直接溢到下面的卡片上（截图里「页签只剩半截」）。
     修法：① 显式 flex:0 0 auto 禁止收缩；② 显式 min-height:46px 覆盖 auto 最小值。 */
  position:relative;z-index:4;
  flex:0 0 auto;min-height:46px;
  display:flex;gap:2px;align-items:stretch;
  padding:0 22px;
  background:transparent;
  border-bottom:0;
  overflow-x:auto;scrollbar-width:none;
}
.tabs::-webkit-scrollbar{display:none}
.tab{
  position:relative;flex:0 0 auto;
  min-height:46px;padding:0 15px;
  border:0;background:transparent;
  color:var(--dim);
  font-family:var(--font-body);font-size:var(--fs-base);font-weight:500;letter-spacing:.03em;
  transition:color .18s var(--ease);
}
.tab::after{
  content:'';position:absolute;left:12px;right:12px;bottom:-1px;height:2px;border-radius:2px;
  background:linear-gradient(90deg,rgba(var(--accent-rgb),.28),var(--accent),rgba(var(--accent-rgb),.28));
  transform:scaleX(0);transform-origin:center;transition:transform .26s var(--ease);
}
.tab:hover{color:var(--fg-2)}
.tab:hover::after{transform:scaleX(.35)}
.tab.active{color:var(--fg)}
.tab.active::after{transform:scaleX(1)}
.tab:focus-visible{outline-offset:-6px}

/* —— 内容区：宽度上限 + 大方留白 —— */
.content{
  flex:1;
  width:100%;max-width:1320px;margin:0 auto;
  padding:26px 30px 64px;
  counter-reset:panelcard;
  animation:shellIn .42s var(--ease) both;
}
.tabpane{display:none}
/* 页切换的位移只做透明度与 4px 上浮：高度突变会推走下方元素（实测 CLS 0.025）。
   同时给激活 pane 一个首屏最小高度，避免「先空后填」造成二次位移。 */
.tabpane.active{display:block;animation:paneIn .3s var(--ease) both}
@keyframes paneIn{from{opacity:0;transform:translateY(4px)}to{opacity:1;transform:none}}

/* ============================================================ 卡片 ==== */
.card{
  position:relative;
  /* HUD 面板：左上/右下切角（clip-path），底纹带极淡的横向扫描线 */
  background:
    linear-gradient(180deg,rgba(var(--accent-rgb),.028),transparent 38%),
    /* 扫描线：6px 间距的极淡横纹（4px 在 1440 宽下会糊成条纹噪点） */
    repeating-linear-gradient(180deg,rgba(var(--accent-rgb),.022) 0 1px,transparent 1px 7px),
    var(--panel);
  border:1px solid transparent;
  border-radius:0;
  padding:20px 22px;margin:0 0 18px;
  /* clip-path 切角 + 用 inset 阴影当描边（border 会被 clip-path 切掉，阴影不会） */
  box-shadow:inset 0 0 0 1px var(--line-2);
  clip-path:polygon(0 0,calc(100% - var(--cut)) 0,100% var(--cut),100% 100%,var(--cut) 100%,0 calc(100% - var(--cut)));
  transition:border-color .22s var(--ease),background .22s var(--ease),box-shadow .22s var(--ease);
}
/* 悬停：边框提亮 + 冷光溢出（HUD 的「激活」反馈） */
.card:hover{border-color:var(--line-3);box-shadow:var(--glow-cyan-soft)}
/* 左上角一枚发光直角标：仪表面板的定位角 */
/* 左上角定位标 + 右下角呼应短线：一个 HUD 模块的「注册角」 */
.card::before{
  content:'';position:absolute;top:0;left:0;right:0;height:2px;z-index:2;
  background:linear-gradient(90deg,transparent 0 42%,var(--accent) 50%,transparent 58% 100%);
  background-size:60% 100%;background-repeat:no-repeat;
  filter:drop-shadow(0 0 8px var(--accent));opacity:.8;
}
/* 右下角两段式角标（横 + 竖），比单线段更像仪表定位角 */
.card::after{
  content:'';position:absolute;bottom:0;right:0;z-index:2;
  width:18px;height:2px;background:rgba(var(--accent-rgb),.45);
  box-shadow:12px -10px 0 -0px rgba(var(--accent-rgb),.3);
}
/* 卡片顶缘扫描：一条 1px 冷光缓缓掠过，错峰 2.4s，周期 9s。
   它承担「系统活着」的信号，同时因为极慢 + 极淡，不会造成视觉疲劳。 */
.card::before{animation:cardSweep 11s linear infinite}
.card:nth-of-type(2)::before{animation-delay:-1.5s}
.card:nth-of-type(3)::before{animation-delay:-3s}
.card:nth-of-type(4)::before{animation-delay:-4.5s}
.card:nth-of-type(5)::before{animation-delay:-6s}
@keyframes cardSweep{
  0%{background-position:-60% 0;opacity:.25}
  18%{opacity:.55}
  30%{background-position:160% 0;opacity:.4}
  100%{background-position:160% 0;opacity:.25}
}
/* gov-card 基座：与 .card 同一 HUD 语言但更轻（无切角、无巡游光），
   治理台的结论卡与明细卡共用。此前它只有 ::after 反光、没有盒体，
   卡片呈「裸文字漂浮」——走查实测缺陷。 */
.gov-card{
  /* 与 .card 同一套 HUD 卡片语言（2026-10-22 跨页一致性审计）：
     设计体系头注释写明「HUD 面板的角不是圆的，是斜切的」（--cut 为主语言，9 处 clip-path），
     而本卡此前用圆角（--radius）+ margin 14px，与 .card 的切角 + margin 18px 冲突 ——
     同一产品里两种卡片语言。此处对齐 .card：切角、inset 描边、同 margin 节奏。 */
  position:relative;
  background:
    linear-gradient(180deg,rgba(var(--accent-rgb),.028),transparent 38%),
    repeating-linear-gradient(180deg,rgba(var(--accent-rgb),.022) 0 1px,transparent 1px 7px),
    var(--panel);
  border:1px solid transparent;border-radius:0;
  padding:20px 22px;margin:0 0 18px;
  box-shadow:inset 0 0 0 1px var(--line-2);
  clip-path:polygon(0 0,calc(100% - var(--cut)) 0,100% var(--cut),100% 100%,var(--cut) 100%,0 calc(100% - var(--cut)));
  transition:border-color .22s var(--ease),background .22s var(--ease),box-shadow .22s var(--ease);
}
/* 悬停只提亮内描边：切角卡没有可描边的 border（会被 clip-path 切掉），
   提 borderColor 无视觉变化，所以用 inset 阴影换色表达「激活」。 */
.gov-card:hover{box-shadow:inset 0 0 0 1px var(--line-3)}
/* gov-card 沿用旧的反光（它没有切角结构） */
.gov-card::after{
  content:'';position:absolute;left:20px;right:20px;top:0;height:1px;pointer-events:none;
  background:linear-gradient(90deg,transparent,rgba(255,255,255,.1) 22%,rgba(255,255,255,.1) 78%,transparent);
}
/* 卡片标题 = 分区小标签：编号 + 展开字距，替代「大号黑标题」这种默认排版 */
.card h3,.gov-card h3{
  display:flex;align-items:baseline;gap:10px;
  margin:0 0 16px;padding-bottom:12px;
  border-bottom:1px solid var(--line);
  font-family:var(--font-display);font-size:12.5px;font-weight:600;
  letter-spacing:.12em;color:var(--fg-2);text-transform:uppercase;
}
.card h3::before,.gov-card h3::before{
  counter-increment:panelcard;
  content:counter(panelcard,decimal-leading-zero);
  font-family:var(--font-mono);font-size:var(--fs-micro);letter-spacing:.06em;
  color:var(--accent);opacity:.9;
}
.card h4,.gov-card h4{
  margin:16px 0 8px;font-family:var(--font-display);
  font-size:var(--fs-small);font-weight:600;letter-spacing:.16em;
  text-transform:uppercase;color:var(--dim);
}

/* —— KPI：数字是主角，标签是刻度 —— */
.grid{display:grid;gap:14px}
/* c4 = 读数面：宽屏固定 5 列（10 个 KPI 正好两行，不留孤行）；窄屏自动降列 */
.grid.c4{grid-template-columns:repeat(auto-fit,minmax(210px,1fr))}
/* c2 = 设置面：桌面严格两列，窄屏一列（表单不该被 auto-fit 拉成三列） */
.grid.c2{grid-template-columns:repeat(2,minmax(0,1fr))}
.grid.c2 input,.grid.c2 select,.grid.c2 textarea{max-width:560px}
.kpi{
  position:relative;overflow:hidden;
  /* 读数格：切角 + 左上定位标 + 细网格底（仪表读数格的标准形态） */
  background:
    repeating-linear-gradient(90deg,rgba(var(--accent-rgb),.02) 0 1px,transparent 1px 12px),
    linear-gradient(180deg,rgba(var(--accent-rgb),.05),transparent 55%),
    var(--panel2);
  border:1px solid var(--line);
  border-radius:3px;
  /* 不再用 clip-path：切角会把 border 一起切掉，左上出现镂空（走查实测）。
     改为「直角小圆角 + 角标」，同样是仪表读数格的语言，但边缘干净。 */
  padding:16px 16px 14px;
  transition:border-color .22s var(--ease),transform .22s var(--ease),box-shadow .22s var(--ease);
}
.kpi:hover{
  border-color:rgba(var(--accent-rgb),.4);transform:translateY(-1px);
  box-shadow:0 0 26px -12px rgba(var(--accent-rgb),.55);
}
/* 左上发光线 + 底部进度感短线：两个角标让读数格「有框架」 */
.kpi::before{
  content:'';position:absolute;top:0;left:0;width:22px;height:2px;
  background:var(--accent);box-shadow:0 0 10px var(--accent);
  transition:width .3s var(--ease);
}
.kpi:hover::before{width:100%}
.kpi::after{
  content:'';position:absolute;right:8px;bottom:6px;width:5px;height:5px;
  background:rgba(var(--accent-rgb),.5);transform:rotate(45deg);
}
/* 读数入场：仅 transform + opacity（不触发 layout，CLS 保持 0）。
   错峰延迟由 :nth-child 给出 —— 一屏 10 个读数依次点亮，是这个面板唯一的「开机感」。 */
.kpi{animation:kpiIn .46s var(--ease) both}
/* SSE 增量刷新重建 DOM 时不重放入场（读数闪烁 = 走查实测缺陷）：只有首帧带动画 */
.grid.no-anim .kpi{animation:none}
.kpi:nth-child(1){animation-delay:.02s}.kpi:nth-child(2){animation-delay:.05s}
.kpi:nth-child(3){animation-delay:.08s}.kpi:nth-child(4){animation-delay:.11s}
.kpi:nth-child(5){animation-delay:.14s}.kpi:nth-child(6){animation-delay:.17s}
.kpi:nth-child(7){animation-delay:.20s}.kpi:nth-child(8){animation-delay:.23s}
.kpi:nth-child(9){animation-delay:.26s}.kpi:nth-child(10){animation-delay:.29s}
@keyframes kpiIn{
  from{opacity:0;transform:translateY(5px)}
  to{opacity:1;transform:none}
}
.kpi .v{
  font-family:var(--font-display);font-size:var(--fs-kpi);font-weight:600;line-height:1.12;
  letter-spacing:.01em;color:var(--fg);font-variant-numeric:tabular-nums;
}
.kpi .l{
  margin-top:7px;
  font-family:var(--font-mono);font-size:var(--fs-micro);letter-spacing:.13em;
  text-transform:uppercase;color:var(--dim);
}

/* 复选框：视觉本体 18px（表格里不抢戏），但命中区用 label + 外框补到 36px，
   避免触屏与审计判据里出现「18px 可点目标」。 */
input[type=checkbox]{
  width:18px;min-width:18px;height:18px;padding:0;
  accent-color:var(--accent);
  box-shadow:0 0 0 9px transparent;   /* 视觉不显，仅扩可点范围 */
}
label:has(> input[type=checkbox]){min-height:36px;align-items:center;display:inline-flex;gap:8px}

/* ============================================================ 折叠块 ==== */
/* 2026-10-20 简化：次级信息默认收起。折叠标题行本身就是「这一块是什么」，
   展开才铺明细 —— 一屏只留结论与关键读数，是这次「页面太乱」整改的核心手法。 */
.fold{
  border-top:1px solid var(--line);
  margin-top:14px;
}
.fold:first-of-type{margin-top:0;border-top:0}
.fold > summary{
  display:flex;align-items:center;gap:9px;
  padding:12px 2px;
  cursor:pointer;list-style:none;
  font-family:var(--font-mono);font-size:var(--fs-mono);letter-spacing:.12em;
  text-transform:uppercase;color:var(--dim);
  transition:color .16s var(--ease);
}
.fold > summary::-webkit-details-marker{display:none}
.fold > summary::before{
  content:'';width:6px;height:6px;flex:0 0 auto;
  border-right:1.5px solid currentColor;border-bottom:1.5px solid currentColor;
  transform:rotate(-45deg);transition:transform .18s var(--ease);margin-left:2px;
}
.fold[open] > summary::before{transform:rotate(45deg)}
.fold > summary:hover{color:var(--fg-2)}
.fold > summary:hover::before{border-color:var(--accent)}
.fold > summary:focus-visible{outline:2px solid var(--accent);outline-offset:3px;border-radius:4px}
.fold-body{padding:2px 0 16px;animation:foldIn .24s var(--ease) both}
@keyframes foldIn{from{opacity:0;transform:translateY(-3px)}to{opacity:1;transform:none}}
.fold-body > table:first-child{margin-top:2px}

/* 状态行：一行人话结论 + 几个关键读数，替代一排 10 个 KPI 卡 */
.status-line{
  display:flex;align-items:center;flex-wrap:wrap;gap:8px;margin-bottom:14px;
  font-size:var(--fs-base);line-height:1.7;color:var(--dim);
}
.status-line b{color:var(--fg);font-weight:600;font-variant-numeric:tabular-nums}
.status-line .sep{color:var(--dim-2)}
.status-line .dot{width:7px;height:7px;border-radius:50%;flex:0 0 auto}
.status-line .dot.on{background:var(--accent-2);box-shadow:0 0 10px var(--accent-2)}
.status-line .dot.off{background:var(--dim-2)}
.status-line code{font-family:var(--font-mono);font-size:var(--fs-small);color:var(--fg-2);background:rgba(255,255,255,.05);padding:1px 7px;border-radius:5px}

/* 读取骨架：慢后端时占位不是空白，而是「正在读」的形状（判据卷：Loading 兜底不得永驻）。
   只做背景位移（合成层友好），与全站动效纪律一致；reduce 态由全局规则压成静态。 */
.skeleton{
  height:11px;border-radius:6px;margin:10px 0;max-width:520px;
  background:linear-gradient(90deg,rgba(120,190,255,.11),rgba(120,190,255,.045) 45%,rgba(120,190,255,.11));
  background-size:220% 100%;
  animation:skeletonSweep 1.6s linear infinite;
}
.skeleton.short{width:58%}
@keyframes skeletonSweep{from{background-position:120% 0}to{background-position:-120% 0}}

/* 空态：一句话说清「这里现在没东西 / 去哪里弄」，不再是整块空白 */
.empty-hint{
  padding:26px 20px;text-align:center;
  border:1px dashed var(--line-2);border-radius:var(--radius-sm);
  color:var(--dim);font-size:var(--fs-base);line-height:1.7;
  background:rgba(255,255,255,.012);
}

/* 顶栏「更多」菜单：跨页入口收进一个按钮，顶栏不再一排 6 个按钮 */
.topmore{
  position:absolute;right:26px;top:calc(100% - 4px);z-index:30;
  display:flex;flex-direction:column;gap:2px;min-width:180px;
  padding:7px;border-radius:var(--radius-sm);
  background:var(--panel2);border:1px solid var(--line-2);
  box-shadow:0 22px 50px -24px #000;
}
.topmore[hidden]{display:none}
.topmore{animation:menuIn .18s var(--ease) both}
@keyframes menuIn{from{opacity:0;transform:translateY(-6px) scale(.985)}to{opacity:1;transform:none}}
.topmore a{
  display:flex;align-items:center;min-height:38px;padding:0 12px;border-radius:var(--radius-xs);
  font-size:var(--fs-base);color:var(--fg-2);transition:background .14s var(--ease),color .14s var(--ease);
}
.topmore a:hover{background:rgba(var(--accent-rgb),.12);color:var(--fg)}

/* ============================================================ 表格 ==== */
table{width:100%;border-collapse:collapse;font-size:var(--fs-base)}
caption{caption-side:top;text-align:left;color:var(--dim);font-size:var(--fs-small);padding-bottom:8px}
th,td{text-align:left;padding:11px 12px;border-bottom:1px solid var(--line);vertical-align:top}
th{
  position:sticky;top:0;z-index:1;
  background:linear-gradient(180deg,rgba(var(--accent-rgb),.055),transparent),var(--panel);
  font-family:var(--font-mono);font-weight:500;font-size:var(--fs-micro);
  letter-spacing:.12em;text-transform:uppercase;color:var(--dim);
  border-bottom:1px solid var(--line-2);
  /* 表头底部一条冷暖渐变细线：暗示「这是一张数据表」而不是普通列表 */
  box-shadow:0 1px 0 rgba(var(--accent-rgb),.18);
}
tbody tr{transition:background .14s var(--ease)}
tbody tr:hover td{background:rgba(255,255,255,.028)}
tbody tr:hover td:first-child{box-shadow:inset 2px 0 0 var(--accent),inset 6px 0 14px -10px var(--accent)}
td{color:var(--fg-2)}
/* 单元格必须能断长串（2026-10-22 极端内容实测）：表格 width:100% 只约束表格本身，
   单元格的**最小内容宽度**仍会把表撑开 —— 300 个无空格字符的单元格能让 900px 容器里
   的表格变成 2334px，并让整页横向溢出。下面的规则此前只写在窄屏断点里，桌面宽度下缺席。 */
th,td{overflow-wrap:anywhere;word-break:break-word}
tbody tr:last-child td{border-bottom:0}

/* 长列表：审计日志/工作目录这类「行数不可控」的表格必须限高滚动，
   否则一页 3000px 会把其它信息全部挤出视线（走查实测）。 */
.table-scroll{
  max-height:min(64vh,680px);overflow:auto;
  border:1px solid var(--line);border-radius:var(--radius-sm);
  background:rgba(0,0,0,.14);
}
.table-scroll table{margin:0}
.table-scroll th{background:var(--panel2)}
.table-scroll::-webkit-scrollbar{width:8px;height:8px}

/* 提示面板：空态也要有设计（不是一整屏空白 + 一行小字） */
.hint-panel{
  display:flex;flex-direction:column;gap:12px;
  padding:30px 28px;border-radius:var(--radius);
  border:1px dashed var(--line-2);
  background:
    radial-gradient(520px 200px at 82% -30%,rgba(var(--accent-rgb),.09),transparent 70%),
    rgba(255,255,255,.012);
}
.hint-panel h4{
  margin:0;font-family:var(--font-display);font-size:clamp(19px,2vw,24px);
  font-weight:600;letter-spacing:.02em;text-transform:none;color:var(--fg);
}
.hint-panel p{margin:0;max-width:68ch;color:var(--dim);font-size:var(--fs-base);line-height:1.75}
.hint-panel ul{margin:0;padding-left:18px;color:var(--dim);font-size:var(--fs-base);line-height:1.9}
.hint-panel code{background:rgba(255,255,255,.06);padding:1px 6px;border-radius:5px;font-family:var(--font-mono);font-size:var(--fs-small)}
.hint-panel .hint-steps{display:flex;gap:10px;flex-wrap:wrap;margin-top:2px}
.hint-panel .hint-step{
  display:inline-flex;align-items:center;gap:8px;padding:8px 13px;border-radius:999px;
  border:1px solid var(--line-2);background:rgba(255,255,255,.03);
  font-family:var(--font-mono);font-size:var(--fs-micro);letter-spacing:.06em;color:var(--fg-2);
}
.hint-panel .hint-step b{color:var(--accent);font-weight:600}

/* ============================================================ 表单 ==== */
input,select,textarea{
  width:100%;
  background:var(--bg2);
  border:1px solid var(--line-2);border-radius:var(--radius-xs);
  padding:10px 12px;
  color:var(--fg);font-family:var(--font-body);font-size:var(--fs-base);line-height:1.5;
  transition:border-color .16s var(--ease),box-shadow .16s var(--ease),background .16s var(--ease);
}
input::placeholder,textarea::placeholder{color:var(--dim-2)}
input:hover,select:hover,textarea:hover{border-color:var(--line-3)}
input:focus,select:focus,textarea:focus{
  outline:none;background:#0e1115;
  border-color:rgba(var(--accent-rgb),.7);
  box-shadow:0 0 0 3px rgba(var(--accent-rgb),.16),0 0 24px -10px rgba(var(--accent-rgb),.35);
}
select{appearance:none;cursor:pointer;padding-right:30px;
  background-image:linear-gradient(45deg,transparent 50%,var(--dim) 50%),linear-gradient(135deg,var(--dim) 50%,transparent 50%);
  background-position:calc(100% - 16px) 50%,calc(100% - 11px) 50%;
  background-size:5px 5px,5px 5px;background-repeat:no-repeat;
}
/* 复选框/单选：控件本体 18px，但通过 label 扩大命中区到 44px（触屏可达）。
   量化审计原读数 15×13px，单点命中面积不足。 */
input[type=checkbox],input[type=radio]{
  width:18px;height:18px;accent-color:var(--accent);vertical-align:middle;
  flex:0 0 auto;cursor:pointer;
}
label:has(input[type=checkbox]),label:has(input[type=radio]){
  display:inline-flex;align-items:center;gap:8px;min-height:32px;cursor:pointer;
}
label{
  display:block;margin:14px 0 6px;
  font-family:var(--font-mono);font-size:var(--fs-micro);letter-spacing:.12em;
  text-transform:uppercase;color:var(--dim);
}
.row{display:flex;gap:10px;align-items:center;flex-wrap:wrap}
/* 工具条：与 .row 不同，**不**把子项拉成 flex:1 —— 操作按钮应保持自身宽度，
   否则三个导出按钮会各占半屏、视觉重量超过页面主体（实测走查缺陷）。 */
.toolbar{display:flex;gap:8px;align-items:center;flex-wrap:wrap}
.toolbar > *{flex:0 0 auto}
.row > *{flex:1;min-width:0}
.row > .fix{flex:0 0 auto}
.num{font-variant-numeric:tabular-nums;text-align:right}
td.ops{display:flex;gap:4px;align-items:center;flex-wrap:wrap}
tbody tr:hover td:last-child{box-shadow:none}
.muted{color:var(--dim);font-size:var(--fs-small);line-height:1.6}
.note{color:var(--dim-2);font-size:var(--fs-small);line-height:1.65}

/* ============================================================ 其它原语 ===== */
.tag{
  display:inline-flex;align-items:center;gap:5px;
  padding:2px 9px;border-radius:999px;
  border:1px solid var(--line-2);background:rgba(255,255,255,.03);
  font-family:var(--font-mono);font-size:var(--fs-micro);letter-spacing:.06em;color:var(--dim);
}
.state-done{color:var(--accent-2)}
.state-running{color:var(--accent)}
.state-failed,.state-timeout{color:var(--danger)}
.state-rejected,.state-cancelled{color:var(--dim)}
.state-queued{color:var(--warn)}
.empty{
  padding:26px 18px;text-align:center;color:var(--dim);font-size:var(--fs-base);
  border:1px dashed var(--line-2);border-radius:var(--radius-sm);background:rgba(255,255,255,.015);
}
pre,.logbox{
  background:#080a0d;border:1px solid var(--line);border-radius:var(--radius-sm);
  padding:14px 16px;overflow:auto;max-height:460px;
  font-family:var(--font-mono);font-size:var(--fs-small);line-height:1.7;color:var(--fg-2);
  /* 同 .doc：无换行长串（日志行 / JSON / 栈帧）会把容器撑开（实测 5657px 未修时）。
     关键：only overflow-wrap/word-break 对 pre **无效** —— <pre> 默认 white-space:pre
     根本不换行，必须先 pre-wrap 才能让断行规则起作用（本轮实测第一版只加 break 无效果）。
     保留 pre-wrap（仍尊重源码换行与缩进），仅在超长时才在任意字符断。 */
  white-space:pre-wrap;overflow-wrap:anywhere;word-break:break-word;
}
.doc{
  background:#080a0d;border:1px solid var(--line);border-radius:var(--radius);
  padding:22px 26px;max-height:62vh;overflow:auto;line-height:1.78;color:var(--fg-2);
  /* 长串必须能断（2026-10-22 极端内容实测）：产出里常出现 URL / 哈希 / 代码标识符
     这类**无空格长串**，不加断行时 600 字符会把 898px 容器撑到 5831px，
     并让整页横向溢出近 1000px（实测 docOverflowX=979）。中文靠 CJK 原生断行，
     只有 ASCII 长串需要显式规则。overflow-wrap:anywhere 比 word-break:break-all
     温和：优先在自然边界断，实在不行才在任意字符断。 */
  overflow-wrap:anywhere;word-break:break-word;
}
.doc h1,.doc h2,.doc h3{margin:20px 0 10px;font-family:var(--font-display);letter-spacing:.02em}
.doc h1{font-size:22px}.doc h2{font-size:18px}.doc h3{font-size:var(--fs-lead)}
.doc p{margin:9px 0}
.doc ul,.doc ol{margin:9px 0;padding-left:22px}
.doc code{background:rgba(255,255,255,.06);padding:1px 6px;border-radius:5px;font-family:var(--font-mono);font-size:var(--fs-small)}

.toast{
  position:fixed;left:50%;bottom:26px;z-index:60;
  transform:translateX(-50%) translateY(14px) scale(.97);
  padding:11px 20px;border-radius:999px;
  background:rgba(18,22,27,.96);border:1px solid var(--line-2);
  color:var(--fg);font-size:var(--fs-base);letter-spacing:.01em;
  box-shadow:0 24px 60px -20px rgba(0,0,0,.9);
  opacity:0;pointer-events:none;
  transition:opacity .2s var(--ease),transform .24s var(--ease);
}
.toast.show{opacity:1;transform:translateX(-50%) translateY(0)}

/* 键盘可达性：所有交互元素已有全局 :focus-visible；这里补表格行的键盘态 */
a:focus-visible,button:focus-visible,select:focus-visible{outline:2px solid var(--accent);outline-offset:2px}

/* ============================================================ 治理台 ==== */
.gov-actions{display:flex;gap:10px;align-items:center;margin:0 0 18px;flex-wrap:wrap}
.gov-banner{
  display:flex;align-items:center;gap:10px;
  border-radius:var(--radius);padding:12px 16px;margin:0 0 18px;
  border:1px solid var(--line-2);
  font-family:var(--font-body);font-weight:600;font-size:var(--fs-base);letter-spacing:.02em;
}
.gov-banner::before{content:'';width:7px;height:7px;border-radius:50%;background:currentColor;box-shadow:0 0 12px currentColor}
.gov-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(158px,1fr));gap:10px}
.gov-kv{
  display:flex;flex-direction:column;gap:4px;
  background:var(--panel2);border:1px solid var(--line);border-radius:var(--radius-xs);
  padding:12px 14px;
}
.gov-kv span{font-family:var(--font-mono);font-size:var(--fs-micro);letter-spacing:.1em;text-transform:uppercase;color:var(--dim)}
.gov-kv b{font-family:var(--font-display);font-size:19px;font-weight:600;font-variant-numeric:tabular-nums}
.gov-stages{display:flex;gap:8px;margin-top:12px;flex-wrap:wrap}
.gov-row{
  display:flex;gap:12px;align-items:baseline;flex-wrap:wrap;
  padding:9px 12px;margin:6px 0;
  background:var(--panel2);border:1px solid var(--line);border-radius:var(--radius-xs);
  border-left:2px solid var(--line-2);
}
.gov-dim{color:var(--dim);font-size:var(--fs-small)}
.lv-ok{border-left-color:var(--accent-2)}
.lv-warn{border-left-color:var(--warn)}
.lv-red{border-left-color:var(--danger)}
.lv-unk{border-left-color:var(--dim-2)}
.gov-banner.lv-ok{background:rgba(88,224,192,.1);border-color:rgba(88,224,192,.3);color:var(--accent-2)}
.gov-banner.lv-warn{background:rgba(var(--warn-rgb),.11);border-color:rgba(var(--warn-rgb),.32);color:var(--warn)}
.gov-banner.lv-red{background:rgba(var(--danger-rgb),.1);border-color:rgba(var(--danger-rgb),.32);color:var(--danger)}
.gov-banner.lv-unk{background:rgba(255,255,255,.04);color:var(--dim)}

/* ── 总览 · 停摆自证横幅（2026-10-21 现场）─────────────────────
   「运行中 + 今日产出 0」是一种自相矛盾的读数：不说清原因，用户只能靠猜。
   这条横幅只在服务端判定「确实没有产出且能给出原因」时出现，正常产出时不渲染。 */
.stall-banner{
  display:flex;gap:10px;align-items:flex-start;
  margin:12px 0 4px;padding:10px 14px;
  background:rgba(var(--warn-rgb),.1);
  border:1px solid rgba(var(--warn-rgb),.3);
  border-left:2px solid var(--warn);
  border-radius:var(--radius-xs);
  color:var(--warn);font-size:var(--fs-base);line-height:1.6;
}
.stall-banner .stall-icon{
  flex:0 0 auto;width:16px;height:16px;margin-top:2px;
  display:inline-flex;align-items:center;justify-content:center;
  border:1px solid var(--warn);border-radius:50%;
  font-family:var(--font-display);font-size:var(--fs-micro);font-weight:700;
}
.stall-banner b{font-weight:600}
.stall-banner .muted{color:var(--dim);margin-top:3px;font-size:var(--fs-small)}

/* ── 治理台 · 首屏状态带（结论先行，证据在后） ─────────────── */
.gov-hero{
  display:grid;grid-template-columns:minmax(240px,1fr) minmax(0,1.55fr);
  gap:0;margin:0 0 18px;
  border:1px solid var(--line);border-radius:18px;overflow:hidden;
  background:
    radial-gradient(620px 220px at 8% -30%,rgba(var(--accent-rgb),.11),transparent 70%),
    linear-gradient(180deg,rgba(255,255,255,.025),transparent 60%),var(--panel);
}
.gov-hero.lv-ok{background:radial-gradient(620px 220px at 8% -30%,rgba(88,224,192,.12),transparent 70%),linear-gradient(180deg,rgba(255,255,255,.025),transparent 60%),var(--panel)}
.gov-hero.lv-warn{background:radial-gradient(620px 220px at 8% -30%,rgba(var(--warn-rgb),.13),transparent 70%),linear-gradient(180deg,rgba(255,255,255,.025),transparent 60%),var(--panel)}
.gov-hero.lv-red{background:radial-gradient(620px 220px at 8% -30%,rgba(var(--danger-rgb),.13),transparent 70%),linear-gradient(180deg,rgba(255,255,255,.025),transparent 60%),var(--panel)}
.gov-hero-main{
  display:flex;flex-direction:column;justify-content:center;gap:6px;
  padding:26px 28px;border-right:1px solid var(--line);
}
.gov-hero-label{font-family:var(--font-mono);font-size:var(--fs-micro);letter-spacing:.2em;text-transform:uppercase;color:var(--dim)}
.gov-hero-level{
  font-family:var(--font-display);font-size:clamp(40px,5.2vw,58px);font-weight:600;
  line-height:1;letter-spacing:.01em;color:var(--fg);
}
.lv-ok .gov-hero-level{color:var(--accent-2)}
.lv-warn .gov-hero-level{color:var(--warn)}
.lv-red .gov-hero-level{color:var(--danger)}
.gov-hero-foot{margin-top:8px;font-size:var(--fs-small);line-height:1.7;color:var(--dim)}
.gov-hero-cells{
  display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:1px;
  background:var(--line);
}
.gov-cell{
  display:flex;flex-direction:column;justify-content:center;gap:4px;
  padding:20px 18px;background:var(--panel);
}
.gov-cell b{
  font-family:var(--font-display);font-size:clamp(22px,2.4vw,30px);font-weight:600;
  line-height:1.05;color:var(--fg);font-variant-numeric:tabular-nums;
}
.gov-cell b i{font-size:.55em;font-style:normal;color:var(--dim);margin-left:3px}
.gov-cell span{font-family:var(--font-mono);font-size:var(--fs-micro);letter-spacing:.12em;text-transform:uppercase;color:var(--dim)}
.gov-cell.tone-warn b{color:var(--warn)}
.gov-cell.tone-danger b{color:var(--danger)}
/* 考核校验矩阵：8 项一屏扫完 */
/* 治理台明细卡折叠：标题行沿用 gov-card 的编号样式，展开才铺明细 */
.gov-card.foldcard{display:block;padding:0}
.gov-card.foldcard > summary{
  display:flex;align-items:baseline;gap:10px;
  margin:0;padding:20px 22px;
  cursor:pointer;list-style:none;
  font-family:var(--font-display);font-size:12.5px;font-weight:600;
  letter-spacing:.2em;text-transform:uppercase;color:var(--fg-2);
  transition:color .16s var(--ease);
}
.gov-card.foldcard > summary::-webkit-details-marker{display:none}
.gov-card.foldcard > summary::before{
  counter-increment:panelcard;
  content:counter(panelcard,decimal-leading-zero);
  font-family:var(--font-mono);font-size:var(--fs-micro);letter-spacing:.06em;
  color:var(--accent);opacity:.9;
}
.gov-card.foldcard > summary::after{
  content:'';margin-left:auto;align-self:center;
  width:6px;height:6px;border-right:1.5px solid var(--dim);border-bottom:1.5px solid var(--dim);
  transform:rotate(-45deg);transition:transform .18s var(--ease);
}
.gov-card.foldcard[open] > summary::after{transform:rotate(45deg)}
.gov-card.foldcard > summary:hover{color:var(--fg)}
.gov-card.foldcard > summary:focus-visible{outline:2px solid var(--accent);outline-offset:-4px}
.gov-card.foldcard > .fold-body{padding:0 22px 20px}
.gov-card.foldcard > .fold-body > .gov-grid:first-child{margin-top:0}

.gov-check-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:8px}
.gov-check{
  display:grid;grid-template-columns:auto 1fr;align-items:center;gap:2px 8px;
  padding:11px 13px;border-radius:var(--radius-xs);
  background:var(--panel2);border:1px solid var(--line);
  border-left:2px solid var(--dim-2);
}
.gov-check-code{
  grid-column:1;font-family:var(--font-mono);font-size:var(--fs-micro);letter-spacing:.1em;color:var(--dim);
}
.gov-check b{grid-column:1/-1;font-size:12.5px;font-weight:500;color:var(--fg-2);line-height:1.35}
.gov-check-state{
  grid-column:2;grid-row:1;justify-self:end;
  font-family:var(--font-mono);font-size:var(--fs-micro);letter-spacing:.08em;
  padding:1px 7px;border-radius:999px;border:1px solid var(--line-2);color:var(--dim);
}
.gov-check.ok{border-left-color:var(--accent-2)}
.gov-check.ok .gov-check-state{color:var(--accent-2);border-color:rgba(88,224,192,.35);background:rgba(88,224,192,.1)}
.gov-check.bad{border-left-color:var(--danger)}
.gov-check.bad .gov-check-state{color:var(--danger);border-color:rgba(var(--danger-rgb),.35);background:rgba(var(--danger-rgb),.1)}
.gov-check{transition:transform .18s var(--ease),border-color .18s var(--ease),background .18s var(--ease)}
.gov-check:hover{transform:translateY(-1px);background:var(--panel3)}
.gov-check.unk{border-left-color:var(--warn)}
.gov-check.unk .gov-check-state{color:var(--warn);border-color:rgba(var(--warn-rgb),.36);background:rgba(var(--warn-rgb),.11)}
@media (max-width:900px){
  .gov-hero{grid-template-columns:1fr}
  .gov-hero-main{border-right:0;border-bottom:1px solid var(--line);padding:20px}
  .gov-hero-cells{grid-template-columns:repeat(2,minmax(0,1fr))}
}
@media (max-width:480px){
  .gov-hero{border-radius:14px}
  .gov-hero-main{padding:16px 15px}
  .gov-hero-level{font-size:clamp(34px,9vw,44px)}
  .gov-hero-foot{font-size:var(--fs-mono);line-height:1.65}
  .gov-hero-cells{grid-template-columns:repeat(2,minmax(0,1fr))}
  .gov-cell{padding:12px 13px;gap:2px}
  .gov-cell b{font-size:22px}
  .gov-cell span{font-size:var(--fs-micro);letter-spacing:.1em;line-height:1.35}
  .gov-check-grid{grid-template-columns:1fr}
  .gov-card.foldcard > summary{padding:16px 15px}
  .gov-card.foldcard > .fold-body{padding:0 15px 16px}
}

/* ============================================================ 模型池 ==== */
/* 通用件（.card/.kpi/.btn/.pill 的基座）在全局；池专属视觉留在页内 <style>，
   这样契约测试仍能钉住「池内行内按钮规则不得上移到全局」。 */
.pill{
  display:inline-flex;align-items:center;gap:5px;
  padding:2px 9px;border-radius:999px;
  font-family:var(--font-mono);font-size:var(--fs-micro);letter-spacing:.05em;
  border:1px solid transparent;
}
.pill::before{content:'';width:5px;height:5px;border-radius:50%;background:currentColor;flex:0 0 auto}
.pill.healthy{background:rgba(88,224,192,.12);color:var(--accent-2);border-color:rgba(88,224,192,.32)}
.pill.degraded{background:rgba(var(--warn-rgb),.13);color:var(--warn);border-color:rgba(var(--warn-rgb),.34)}
.pill.down{background:rgba(var(--danger-rgb),.12);color:var(--danger);border-color:rgba(var(--danger-rgb),.32)}
.pill.unknown{background:rgba(255,255,255,.05);color:var(--dim);border-color:var(--line-2)}
.pill.disabled{background:rgba(255,255,255,.03);color:var(--dim-2);border-color:var(--line);text-decoration:line-through}
.kv{display:grid;grid-template-columns:repeat(auto-fit,minmax(150px,1fr));gap:10px}
.kv .item{background:var(--panel2);border:1px solid var(--line);border-radius:var(--radius-xs);padding:12px 14px}
.kv .item .v{font-family:var(--font-display);font-size:20px;font-weight:600;font-variant-numeric:tabular-nums}
.kv .item .l{font-family:var(--font-mono);font-size:var(--fs-micro);letter-spacing:.1em;text-transform:uppercase;color:var(--dim);margin-top:3px}

/* ============================================================ 动效 ==== */
@keyframes shellIn{from{opacity:0}to{opacity:1}}

/* ============================================================ 响应式 ==== */
@media (max-width:1080px){
  .content{padding:18px 20px 56px}
  .pagebar{max-width:1080px;padding:22px 20px 4px}
  .topbar{padding:12px 18px;gap:12px;max-width:1080px}
  .tabs{padding:0 14px;max-width:1080px;margin:0 auto}
}
@media (max-width:720px){
  .topbar-shell{position:sticky}
  /* —— 窄窗口导航区瘦身（2026-10-20 实测：用户窗口 1040x613 物理 / DPR1.75 = 594x350 CSS，
        原导航吃掉 239px = 68% 屏高，子页只剩 99px 可见 ⇒ 体感「子页面被挡住」）。
        三处压缩：品牌行不再独占一行、状态行与按钮同排、页面标题取消超大字号。 —— */
  .topbar{flex-wrap:nowrap;gap:8px;padding:8px 12px}
  .brand{width:auto;flex:0 0 auto}
  /* 品牌文字在窄屏让位给状态与操作：.brand 是 <a>纯文本</a>，用 font-size:0 隐藏文字、
     ::before 色块不受影响（仍显示 9px 方块，作为品牌锚点）。 */
  .brand{font-size:0;gap:0;letter-spacing:0;min-width:9px}
  .brand::before{width:9px;height:9px}
  .topstat{flex:1 1 auto;order:0;white-space:nowrap;font-size:var(--fs-micro);line-height:1.3}
  /* nowrap 会让 4 个按钮（同步/探测/刷新/更多）在 390px 下把 topactions 撑到 274px、
     页面整体被撑宽 18px（2026-10-20 实测 modelpool-390 overflowX=18）。
     改用 min-width:0 + 允许换行，按钮仍不挤成竖排（按钮自身 nowrap）。 */
  .topactions{flex-wrap:wrap;gap:5px;justify-content:flex-end;flex:1 1 auto;min-width:0}
  .topactions .btn{flex:0 0 auto}
  /* 页面识别区：窄屏下不再用巨型标题（40px 只为撑"重量"，窄屏纯占地方） */
  .pagebar{padding:8px 12px 0;gap:8px}
  .pagetitle{font-size:var(--fs-lead);letter-spacing:.02em}
  .pagecrumb{font-size:10px}
  .pagesub{display:none}
  .content{padding:12px 13px 48px}
  .card{padding:16px 15px;border-radius:12px}
  .card h3,.gov-card h3{letter-spacing:.14em}
  .grid.c2{grid-template-columns:1fr}
  .grid.c4{grid-template-columns:repeat(2,minmax(0,1fr))}
  .grid.c2 input,.grid.c2 select,.grid.c2 textarea{max-width:none}
  .gov-grid{grid-template-columns:repeat(auto-fill,minmax(132px,1fr))}
  .row{flex-direction:column;align-items:stretch}
  .row > .fix{width:100%}
  .tabs{overflow-x:auto;-webkit-overflow-scrolling:touch;scrollbar-width:none}
  .tabs::-webkit-scrollbar{display:none}
  .tab{min-height:44px;padding:0 13px}
  /* 表格固定布局 + 任意位置断行：审计日志这类「内容长度不可控」的表，
     在窄屏下也不得把整页撑出横向滚动。 */
  table{font-size:12.5px;table-layout:fixed}
  th,td{padding:9px 7px;word-break:break-word;overflow-wrap:anywhere}
  th{position:static}
  pre,.logbox{white-space:pre-wrap;word-break:break-word}
  .kpi .v{font-size:25px}
  .toast{left:14px;right:14px;transform:translateY(16px) scale(.97);text-align:center}
  .toast.show{transform:translateY(0) scale(1)}
  .topmore{right:14px;left:14px;min-width:0}
  .fold > summary{padding:14px 2px}
  .status-line{gap:6px;font-size:12.5px}
  /* 窄屏顶部操作按钮：36px 足够鼠标点，也保住触屏 32px 以上可点性 */
  .topbar .btn{min-height:34px;padding:0 10px;font-size:var(--fs-small)}
  /* 页签：44 → 36，仍是可点尺寸（触屏档由 pointer:coarse 兜回 44） */
  .tab{min-height:36px;padding:0 12px;font-size:12.5px}
  /* 页签条本体的 min-height 也要一起压（基座写死 46px 是为了防 flex 压扁；
     窄屏下 46 会白白多占 8px，改成 38 仍 >36 的页签高度，不破坏防压扁判据） */
  .tabs{min-height:38px}
}
@media (max-width:480px){
  /* 注意：这里**不**缩小 body 字号（2026-10-22 WCAG 1.4.4 实测）。
     窄视口分两种成因：① 手机（真小屏，14px 合理）；② **缩放放大**（用户把 200%/400%
     放大后视口同样变窄）。两者在 CSS 里无法区分，而后者一旦把 15px 降到 14px，
     就是「用户为了看清而放大、字号反而变小」—— 与 1.4.4 的意图正好相反。
     故保留 15px，靠布局（单列/卡片化/换行）消化窄视口，不靠牺牲可读性。 */
  /* 超窄屏顶栏：操作按钮缩小内距，保证 4 个按钮一行放得下 */
  .topbar .btn{min-height:32px;padding:0 8px;font-size:var(--fs-mono)}
  /* ≤480px 管理面板表格转卡片模式（2026-10-21 窄屏走查实测）：
     390px 下 table-layout:fixed 把 6-8 列均分成每列 ~48px，「价值先验」折成 4 行、
     「状态」列跑位 —— 那是列宽塌陷，不是内容问题。复用池表已验证的
     data-label 卡片范式：thead 隐藏、每 tr 变一张卡、td 前置字段名。
     只对 .content 内表格生效（池表 .pool-table 在 720px 已切，且是 5-6 列无中文长头）。 */
  .content table thead{display:none}
  .content table tbody{display:block}
  .content table tr{display:block;border:1px solid var(--line-2);border-radius:12px;padding:12px 14px;margin:0 0 12px;background:rgba(255,255,255,.02)}
  .content table tr:last-child{margin-bottom:0}
  .content table td{display:flex;gap:10px;align-items:center;padding:5px 0;border-bottom:0}
  .content table td::before{content:attr(data-label);flex:0 0 auto;min-width:4.2em;color:var(--dim);font-family:var(--font-mono);font-size:10.5px;letter-spacing:.1em}
  .content table td:not([data-label])::before{content:'';min-width:0}
  .content table td.ops,.content table td.num{justify-content:flex-end}
  .content table td.ops::before,.content table td.num::before{margin-right:auto}
  .content table td.ops .btn{min-height:36px}
  .table-scroll{max-height:none;border:0;background:transparent;overflow:visible}
  .topstat{font-size:10.5px}
  .content{padding:10px 10px 42px}
  .pagebar{padding:6px 10px 0}
  .pagetitle{font-size:14px}
  .card{padding:14px 13px;border-radius:11px}
  .start-btn{border-radius:18px;letter-spacing:.14em;padding:0 22px}
  .home-admin{bottom:max(10px,env(safe-area-inset-bottom));right:12px;padding:8px 10px}
  .btn{padding:0 12px}
  .kpi{padding:13px 12px}
  .kpi .v{font-size:21px}
  .kpi .l{font-size:var(--fs-micro);letter-spacing:.1em}
  .grid.c4{gap:10px}
  .brand{font-size:var(--fs-base);letter-spacing:.1em}
}
/* 低高度横屏（手机横放 / 小窗口）：按钮仍在首屏内且不溢出 */
@media (max-height:520px){
  .start-btn{min-height:90px;font-size:clamp(18px,4.2vw,23px);border-radius:20px}
  .home-admin{bottom:8px}
}
@media (min-width:1600px){
  /* 取景框刻度随画布放大（2026-10-23 CDP 实测）：1440/1920/2560 三档读到**完全相同**的
     色值（alpha .5/.35）、线宽（1.5px）、间距（46px）—— 而 2560 的画布面积是 1440 的 3.2 倍，
     刻度被稀释成看不见。宽屏必须加粗、提亮、拉大间距，否则「仪器感」在大屏上消失。
     实测目标：2560 下刻度线在 1 米外观测距离仍可辨。 */
  .home-main::after{
    --tw:2px;
    background-image:
      linear-gradient(90deg,var(--accent) 0 var(--corner),transparent var(--corner)),
      linear-gradient(180deg,var(--accent) 0 var(--corner),transparent var(--corner)),
      linear-gradient(270deg,var(--accent) 0 var(--corner),transparent var(--corner)),
      linear-gradient(180deg,var(--accent) 0 var(--corner),transparent var(--corner)),
      linear-gradient(90deg,var(--accent) 0 var(--corner),transparent var(--corner)),
      linear-gradient(0deg,var(--accent) 0 var(--corner),transparent var(--corner)),
      linear-gradient(270deg,var(--accent) 0 var(--corner),transparent var(--corner)),
      linear-gradient(0deg,var(--accent) 0 var(--corner),transparent var(--corner)),
      repeating-linear-gradient(90deg,rgba(150,215,255,.72) 0 var(--tw),transparent var(--tw) 64px),
      repeating-linear-gradient(90deg,rgba(150,215,255,.5) 0 var(--tw),transparent var(--tw) 64px);
    background-size:
      var(--corner) var(--tw), var(--tw) var(--corner),
      var(--corner) var(--tw), var(--tw) var(--corner),
      var(--corner) var(--tw), var(--tw) var(--corner),
      var(--corner) var(--tw), var(--tw) var(--corner),
      100% 9px, 100% 7px;
    filter:drop-shadow(0 0 8px rgba(var(--accent-rgb),.7));
    opacity:.92;
  }
  .content{padding:20px 44px 80px;max-width:1460px}
  .pagebar{max-width:1460px;padding:32px 44px 4px}
  .topbar{max-width:1460px}
  .kpi .v{font-size:32px}
}
/* 触屏可点性：真正的手指设备才抬到 44px。
   2026-10-20 修：原来写成 (pointer:coarse), (max-width:720px) ——「窄窗口」不等于「触屏」，
   桌面用户把窗口拖窄时页签被强制 44px，导航区又变胖（用户窗口 594x350 时导航占 68%）。
   现在窄窗口走上面 720 档的 36px，只有粗指针设备才 44px。 */
@media (pointer:coarse){
  .btn{min-height:44px;padding-left:16px;padding-right:16px}
  .btn.sm{min-height:38px}
  .tab{min-height:44px}
  input,select,textarea{min-height:44px}
  a.btn{display:inline-flex;align-items:center}
  .topbar .btn{min-height:44px}
}
/* 无障碍：尊重系统「减少动态效果」，一切动画退化为静态（状态仍靠颜色/文案表达） */
@media (prefers-reduced-motion:reduce){
  *,*::before,*::after{
    animation-duration:.001ms !important;animation-iteration-count:1 !important;
    transition-duration:.001ms !important;scroll-behavior:auto !important;
  }
  .start-btn[data-state=busy] .btn-load{opacity:1;border-top-color:rgba(255,255,255,.4)}
}
/* ============================================================ 打印 ====
   2026-10-22 CDP 打印介质实测：全站此前**零** @media print 规则，
   打印预览里 body 仍是 rgb(4,7,13) 深底 + 极光渐变 + 顶栏 + 按钮残骸 ——
   纸上是满页黑块，完全不可用。而界面上有「打印自检报告」按钮，用户真的会按 Ctrl+P。
   打印版走「白纸黑字」：去掉所有装饰层（渐变/网格/辉光/扫描线），
   隐去导航与操作件，只留内容；卡片退化成分隔线而不是色块（省墨且清晰）。 */
@media print{
  /* 白纸：先关掉所有背景层与装饰伪元素 */
  html,body{
    background:#fff !important;color:#111 !important;
    -webkit-print-color-adjust:economy;print-color-adjust:economy;
  }
  body::before,body::after,body.home::before,body.home::after,
  .home-main::before,.home-main::after,
  .card::before,.card::after,.gov-card::after,
  .topbar-shell::after,.kpi::after,.kpi::before,.skeleton{display:none !important}
  /* 隐去导航与操作面（打印不点按钮） */
  .topbar-shell,.tabs,.topactions,.topmore,.toolbar,
  .btn,button,select,input[type=checkbox],.toast,
  .home-entries,.home-main::after{display:none !important}
  /* 内容区：白底黑字，去掉限宽与内边距带来的空白 */
  .content{max-width:none !important;padding:0 !important;animation:none !important}
  .card,.gov-card{
    background:#fff !important;border:0 !important;border-bottom:1px solid #bbb !important;
    box-shadow:none !important;clip-path:none !important;border-radius:0 !important;
    padding:10px 0 !important;margin:0 0 10px !important;break-inside:avoid;
  }
  .card h3,.gov-card h3,.card h4,.gov-card h4{color:#111 !important;border-bottom:1px solid #999 !important}
  .card h3::before,.gov-card h3::before{color:#555 !important}
  /* 文本与数据：保证可读对比 */
  *{color:#111 !important;text-shadow:none !important}
  .muted,.gov-dim,.dim,.status-line{color:#444 !important}
  th{background:#f2f2f2 !important;color:#111 !important;box-shadow:none !important}
  td,th{border-bottom:1px solid #ccc !important}
  .kpi{background:#fff !important;border:1px solid #ccc !important;border-radius:0 !important}
  .kpi .v{color:#111 !important}
  /* 状态色保留语义但压深（黑白打印机也能辨亮暗） */
  .state-done,.lv-ok{color:#0a6b2d !important}
  .state-failed,.lv-red{color:#a30014 !important}
  .lv-warn{color:#8a5b00 !important}
  /* 折叠块：打印必须展开（打印稿不能靠点击） */
  details > .fold-body,.gov-card.foldcard > .fold-body{display:block !important}
  details:not([open]) > .fold-body{display:block !important}
  summary{list-style:none}
  /* 长表格不截断，跨页处允许断开 */
  .table-scroll{max-height:none !important;overflow:visible !important;border:0 !important;background:transparent !important}
  table{break-inside:auto}
  tr{break-inside:avoid}
  /* 链接打印出 URL（离线纸质件仍可追溯） */
  a[href^="http"]::after{content:" (" attr(href) ")";font-size:10px;color:#666}
}`
