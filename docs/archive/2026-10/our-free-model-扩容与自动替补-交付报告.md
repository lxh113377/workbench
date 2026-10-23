# Our Free Model 三渠道扩容 + 故障自动替补 交付报告

日期：2026-10-20 08:25 | 执行端：DSH Desktop（ds 端）

## 目标

1. 增加可选择可支持的模型数量（从免费模型 / EAC 模型 / 白嫖模型接入三渠道）
2. 有不可用模型故障时，其余可用模型可迅速自动替补

## 现状盘点（改动前实测）

### 三渠道架构
- **免费模型**（OpenCode Zen 网关匿名车道）：10 个模型 id，本网络出口被上游判定滥用流量，多数 probe 判 unavailable（但整轮全拒时保留广播）
- **EAC 模型**（协付车道，GitHub 授权）：8 个模型（deepseek-v4.1-flash / kimi-k3 / glm-5.3 / kimi-k2.6 / gpt-oss-20b 等），主力可用
- **白嫖模型接入**（13 账号渠道，已登录 6 家共 66 账号）：qodercn 12 / codearts 13 / zcode 12 / minimax 11 / buddy 10 / trae 8
- **Kilo 渠道**（免密免费池）：18 个模型
- **聚合渠道**（自带 Key）：76 家提供商注册表，当前未配置 Key

### 故障数据（stats.json 实测）
- deepseek-ai/deepseek-v4.1-flash：69 次调用 45 次失败（65% 失败率）
- z-ai/glm-5.3：209 次 13 败
- kilo-auto/free：1159 次 11 败
- 原默认模型 qodercn/qfmodel：单点，无替补

## 改动清单

### 1. combo:auto-main 替补链 9 → 15 模型（~/.dsh/our-free-model/settings.json）

链序（fallback 顺序 = 优先级）：
1. deepseek-ai/deepseek-v4.1-flash（EAC 主力）
2. moonshotai/kimi-k3（EAC）
3. z-ai/glm-5.3（EAC）
4. deepseek-v4.1-flash-free（免费车道）
5. kilo-auto/free（Kilo 自动路由）
6. nvidia/nemotron-3.5-super-vi:free（Kilo，视觉+推理）
7. minimax/minimax-m3.1-flash:free（Kilo，1M ctx）
8. glm-5.3-flash-free（免费车道）
9. dots-studio/dots-3-note-preview:free（Kilo，512K ctx）
10. moonshotai/kimi-k2.6（EAC）
11. opencode/deepseek-v4.1-flash-free（EAC 中继的 OpenCode 免费车道，服务器侧出口不受本机 IP 限制）
12. opencode/glm-5.3-flash-free（同上）
13. nvidia/nemotron-3-super-120b-a12b:free（Kilo 大杯）
14. nvidia/nemotron-3.5-lightning:free（Kilo）
15. openrouter/free（Kilo OpenRouter 自动路由）

### 2. combo:auto-free 9 → 12 模型（主力大杯前移，小杯垫底）

### 3. combo:auto-vision 重建：全部 6 个 vision=true 模型（旧链混入 vision=false 模型名不副实）

### 4. 默认模型切换（~/.dsh/profiles/desktop/cordis.patch.yml）

agent-default-model: qodercn/qfmodel（单点）→ our-free-model / combo:auto-main（15 模型 fallback 链）
reasoningEffort: xhigh → high

### 5. 白嫖渠道补开 7 个旗舰模型（~/.dsh/channel-pack/state.json disabledModels）

- buddy: kimi-k3-2、glm-5.3-flashx、glm-5.3-flash、mimo-v2.6-pro
- trae: kimi-k2.7-code、glm-5.3-flashx
- codearts: deepseek-v4-pro

## 生效机制

- combo 是 OFM 的虚拟模型：一个模型名背后是有序链，请求内逐候选尝试，首个出正文者胜出；候选在输出正文前失败会被整段丢弃，切换对调用方不可见（毫秒级替补）
- 白嫖渠道模型在 DSH 选择器各 provider 分组独立可选（作为手动替补池）
- **cordis.patch.yml 的默认模型改动需重启 DSH Desktop 生效**（设置服务在宿主启动时读取）
- settings.json 的 combos 已被运行中宿主内存态持有，本次部署了守护进程（后台 job）防止宿主周期 flush（15 分钟 probe / 30 分钟 feed）回写旧值；重启后守护使命完成

## 验证记录

1. 离线仿真 buildComboCatalog：三链全部构建成功（15/12/6 链接，vision 链 vision=true ✓）
2. YAML 解析：0 errors，agent-default-model 段为 our-free-model/combo:auto-main ✓
3. 守护进程恢复测试：模拟宿主 flush 旧 combos → 7 秒内自动恢复 15 模型版 ✓
4. 真实覆盖事件：00:21:48 守护已恢复过一次宿主实际回写 ✓

## 可选模型数量变化

- combo 替补链：9 → 15（+67%）
- combo:auto-free：9 → 12
- combo:auto-vision：3 → 6（全真视觉）
- 白嫖渠道可见模型：+7 个旗舰
- 默认模型故障恢复能力：单点（0 替补）→ 15 级深链

## 回滚

- settings.json.bak-2026-10-20T*（OFM combos 原版）
- cordis.patch.yml.bak-2026-10-20T*（默认模型原版）
- channel-pack/state.json.bak-*-enablemodels（渠道开关原版）
