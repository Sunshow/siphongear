# SiphonGear

> 一个**配置驱动的指标采集 + 监控看板**：定时抓取网站 / API 数据，提取成时间序列指标，仪表盘统一查看，按阈值告警。
>
> 单二进制（Go + 内嵌 Vue 3 UI），原生为「多账户余额监控」而生，已泛化成通用的轻量采集平台。

![SiphonGear Dashboard](docs/screenshot-dashboard.png)

[English summary](#english-summary)

---

## 对外 HTTP API

除了内嵌 UI，SiphonGear 还提供一组只读的 HTTP API，方便把指标接到 Grafana / 自建大盘 / 脚本 / 飞书机器人之类的外部系统。鉴权用面板里 **API Keys** 页生成的 key（`sg_xxx_yyy`），通过 `X-API-Key` 或 `Authorization: Bearer` 头携带。

详细端点、参数、返回结构请见 [API.md](./API.md)。

---

## 它能解决什么问题

- **多账户余额监控**：在 N 个 LLM 网关 / sub2api / NewAPI / DeepSeek / 自建中转上都有点钱，每天一个个登录看太累。一次配好模板，仪表盘 30 分钟自动刷新，余额低于阈值自动变红。
- **任意 HTTP / HTML / JSON 指标采集**：登录、token、Cookie、浏览器渲染都能拼成一条流水线，结果落库变时序数据。
- **轻量定时任务调度**：固定间隔 / cron / 上游事件触发都行，不想为这个搭一整套 Airflow。

如果你之前在用一堆 Bash + crontab + 自写脚本干这些事，SiphonGear 就是这件事的 UI 版本。

## 核心特性

- **可视化流水线**：每个采集任务由 `Input → Fetch → Transform → Parse → Extract` 五段组成，18 个内置 Step（HTTP / 浏览器 / JSONPath / CSS 选择器 / 正则 / JS 沙箱…），全部图形化配置
- **模板一键创建**：选模板 → 填账号 → Apply，Site / Credential / Collector / Pipeline / Indicators 一次到位
- **三种调度方式**：固定间隔、cron 表达式、事件链式触发（A 跑完触发 B）
- **Site Tags + Dashboard 过滤**：上百个 collector 也能按 tag 多选分组
- **阈值告警规则（Rules）**：指标 `<` / `>` / `=` 阈值时仪表盘卡片高亮，可按 tag 限定生效范围
- **多通道告警通知**：规则可挂多个通知实例，仅在 OK→alert / alert→OK 翻转时推送，避免刷屏；首期内置 ServerChan，类型可扩展；title / body 支持 Go `text/template` 自定义，留空走默认
- **Dashboard 自动刷新**：默认 30s 一次，可关，间隔可调（10/30/60/120/300s），一键「全部触发一次」
- **指标历史曲线**：每个指标都有时序数据，可查询 / 画图
- **History**：每行合并一次 Run 与对应的 Data Points，按开始时间从新到旧排列；支持本地日期范围、指标筛选和分页，点击可查看步骤详情
- **Dry Run**：不写库试跑流水线，立即查看每个 Step 的输出片段
- **Save as Template**：把调好的 Collector 一键存成自己的模板复用
- **凭证 AES-GCM 加密**：永远不在 API 回写明文，运行时仅注入到 header / body
- **会话复用（Session Reuse）**：密码登录站点首次登录后，把 cookies / UA / 关键变量加密持久化到 `sessions` 表；后续调度默认复用，只有会话失效才重新登录，避免每次运行都被站点当成新设备（见下文「会话复用」）
- **单二进制 + 纯 Go SQLite**：无 cgo，开箱即用；MySQL / Postgres 也可

## 中文简介（架构）

- 后端 Go：Gin + GORM + zerolog + bytedance/sonic + chromedp + dop251/goja + robfig/cron
- 前端：Vue 3 + TS + Vite + Element Plus + Pinia + Vue Router + ECharts，通过 `go:embed` 打入二进制
- 默认 SQLite（`glebarez/sqlite`，纯 Go），可换 MySQL / Postgres
- 凭证 AES-GCM 加密；JWT 单用户登录

| 层 | 选型 |
|---|---|
| 语言 | Go 1.25+ |
| HTTP | Gin |
| ORM | GORM (`glebarez/sqlite`) |
| 日志 | zerolog |
| JSON | bytedance/sonic |
| 浏览器 | chromedp |
| 调度 | robfig/cron v3 |
| 脚本 | dop251/goja |
| 前端 | Vue 3 + TypeScript + Vite + Element Plus + Pinia + Vue Router + ECharts |

---

## 5 分钟跑起来

### 前置

- Go 1.25+ 和 Node 22+（如果要本地编译）
- 或者直接用 Docker
- （可选）Chrome / Chromium，仅当你用 `fetch.browser` Step 时

### 二选一：本地编译

```bash
git clone <this-repo> siphongear
cd siphongear

cp config.yaml.example config.yaml
# 编辑 config.yaml：把 master_key 和 jwt_secret 改成长随机串

make build                 # 同时打包前端 dist + Go 二进制
./bin/siphongear --config config.yaml
```

### 二选一：Docker

```bash
docker run -d --name siphongear -p 7080:7080 \
  -e SIPHON_AUTH__MASTER_KEY=$(openssl rand -hex 32) \
  -e SIPHON_AUTH__JWT_SECRET=$(openssl rand -hex 32) \
  -v siphongear-data:/app/data \
  sunshow/siphongear:latest
```

启动后浏览器打开 <http://localhost:7080>，默认账号 `admin / admin`（首启动按 `auth.init_username` / `auth.init_password` 创建，登录后请到 Settings 改密码）。

### 创建你的第一个采集任务（推荐用模板）

以监控 DeepSeek 账户余额为例：

1. 顶部菜单 **Templates → From Template**
2. 选 `deepseek-balance`
3. 填 `API Key`（`sk-xxxx`，DeepSeek 控制台 → API keys）；其余字段保持默认即可（已带 `https://api.deepseek.com`）
4. 点 **Apply**，系统会自动建好 Site / Credential / Collector / 流水线 / 指标
5. 进 **Collectors** 列表点 **Run** 触发一次，几秒后回到 **Dashboard** 就能看到余额卡片
6. 进 **Rules** 加一条规则：`indicator: balance / op: < / value: 10 / target: all` —— 余额低于 10 元的卡片就会高亮告警

接下来就可以照葫芦画瓢，把 sub2api / NewAPI / 自家 API 都接上。

### 开发模式（前后端分离）

```bash
# 终端 1：后端
SIPHON_CONFIG=config.yaml go run ./cmd/server

# 终端 2：前端 HMR
cd web && npm install && npm run dev
# http://localhost:5173 (vite proxy /api -> :7080)
```

---

## 使用指南（按页面）

应用左侧导航 8 个页面，自上而下：

### Dashboard（仪表盘）

- 所有指标按 Site 分组成卡片，显示 **当前值 / 上次值 / 变化（带正负号）/ 上次更新时间**
- 顶部 tag 多选过滤；自动刷新可开关、间隔可调
- **Refresh All** 会并发触发当前过滤范围内所有 collector 的真实运行
- 命中告警规则时卡片显示 warning 颜色

### Collectors（采集任务）

每个 Collector = 一条 Pipeline + 多个 Indicator + 一种 Schedule。

- 编辑器分四块：基本信息 / Pipeline 步骤 / Indicators 绑定 / Schedule；保存后多一个 **Session** tab，可以查看 / 清除该任务的持久化登录态（见「会话复用」）
- **Dry Run**：不写库试跑，按步显示 Snippet 和 Vars，方便调流水线
- **Save as Template**：调好后一键存为「用户模板」（带 `{{BASE_URL}}` 占位符），下次复用

### Templates（模板中心）

- 列出所有模板：内置（builtin）+ 用户自建（user）
- 用户模板可改名、可删
- 「From Template」就是基于模板新建 Collector 的入口（也在 Collector 列表页直接可用）

### Sites（站点）

被采集对象的元信息：名字、Base URL、Tags、Notes。

- **Tags** 用来在 Dashboard 上过滤；多个 tag 用逗号分隔
- 一个 Site 下挂多个 Credential 和 Collector

### Credentials（凭证）

按 Site 分组，AES-GCM 加密落库，永远不在 API 返回明文。

- 类型：`password` / `token` / `cookie` / `custom`
- payload 字段由模板的 `credential_hint` 决定，也可手动建任意字段

### Rules（阈值告警）

- 条件：`compare`（`<` / `≤` / `>` / `≥` / `=` / `≠`）+ 阈值
- 目标：`all`（所有指标） 或 `tags`（按 Site tag 限定）
- 动作：`indicator_color`（仪表盘卡片高亮）
- 同一指标命中多条规则时按 Priority 选最高优先级
- **Notify**：可勾选一个或多个通知实例，仅在 OK→alert 与 alert→OK 翻转时推送（按规则 + 指标维度记状态，避免刷屏）
- **自定义模板**：title / body 两个 Go `text/template` 字段，留空用内置默认；上下文含 `.Severity / .Rule / .Collector / .Site / .Indicator / .Value / .ValueRaw / .ValueNum / .RunID / .Time` 与 `upper / lower / printf / default / now` 函数；支持 alert / recovery 两种 severity 的 Preview 试渲染

### Notifications（通知）

通知系统两级：**通知类型**（代码内置实现，可扩展）+ **通知实例**（具体配置实例）。

- **Channels** tab：基于通知类型创建实例，名称 + 启用开关 + 备注 + 类型自带的参数表单（含 `secret` 字段走 password input）；payload 全部 AES-GCM 加密落库；提供 **Test** 按钮一键试发，结果写入日志
- **Logs** tab：每次实际投递（包括 Test）都会写一条 `notification_logs`，包含 channel / 严重级别 / 标题 / 状态 / 错误
- 内置类型：`serverchan`（方糖，`https://sctapi.ftqq.com`）；`channel` 字段可选，留空用方糖控制台默认通道，多通道用 `|` 分隔，如 `9|66`

加新通知类型：在 `internal/notify/<type>/` 下实现 `notify.Notifier`，`init()` 调 `notify.Register(Meta{...}, factory)`，并在 `internal/notify/builtin/builtin.go` 加 blank import；前端表单按 schema 自动渲染，无需 UI 改动。

### Settings（设置）

- 修改当前用户密码

### 流水线 Pipeline（深入一层）

一个流水线就是有序的 Step 列表，Step 之间通过 `Payload` 传递：

- `Body`：原始字节流（HTTP body / 抓到的 HTML 等）
- `Object`：parse 阶段产物（JSON 对象 / goquery DOM）
- `Vars`：键值表，extract 阶段往里塞值，runtime 模板可读 `{{.vars.foo}}`

每个 Step 除了 `kind` / `name` / `config` / `enabled`，还可以带两个可选字段，用来做条件执行和容错：

| 字段 | 类型 | 说明 |
|---|---|---|
| `when` | string（JS 表达式） | 对当前 payload 求值，falsy 时跳过该 Step（Step Log 记为 skipped）。作用域与 `script.js.*` 相同：`payload = {vars, meta, body, object}`，同时暴露裸 `vars` / `meta`，如 `"when": "!vars.session_ok"`。表达式报错或超时（3s）会让整次运行失败 |
| `on_error` | `fail`（默认）/ `continue` | `fail` 让整次运行失败（原行为）；`continue` 把错误记进 Step Log 后，带着失败前的 payload 继续跑后面的 Step，并写入 `vars.last_error`（错误文本）/ `vars.last_error_step`（步骤序号），本次运行不算失败 |

编辑器里每个 Step 的 **Run if** / **On error** 高级选项就是这两个字段；设置了 `when` 的步骤会在标题旁显示 `when: ...` 标签。

例如只在会话不可用时才登录：

```json
{
  "kind": "fetch.http",
  "name": "login",
  "when": "!vars.session_ok",
  "on_error": "continue",
  "config": { "method": "POST", "url": "{{BASE_URL}}/api/user/login" }
}
```

### 内置 Step

| 阶段 | Kind | 说明 |
|---|---|---|
| input | `input.static` | 注入字面量到 Vars |
| input | `input.credential` | 按 ID 取出加密凭证，解密注入 Vars |
| input | `script.js.input` | goja JS 沙箱（默认禁网络） |
| input | `session.load` | 恢复持久化会话（cookies / UA / vars），计算 `vars.session_ok` |
| fetch | `fetch.http` | Resty，URL / Headers / Body 支持 `{{.vars.x}}` 模板；默认复用并回写会话（`use_session` / `session_key` / `user_agent` / `mark_login`） |
| fetch | `fetch.browser` | chromedp 渲染 / 求值 |
| transform | `transform.gunzip` | gzip 解压 |
| transform | `transform.charset` | 按 Content-Type 转码 |
| transform | `transform.template` | text/template 渲染当前 Body |
| transform | `transform.expr` | 数值表达式（读 `vars.*`），单值或批量写回 Vars |
| transform | `script.js.transform` | JS 沙箱改 Body / Vars |
| parse | `parse.json` | sonic 解析为 Object |
| parse | `parse.html` | goquery 文档 |
| extract | `extract.jsonpath` | ohler55/ojg，按映射写 Vars |
| extract | `extract.css` | CSS 选择器 + 可选属性 |
| extract | `extract.regex` | 命名分组直接成 Vars |
| extract | `script.js.extract` | JS 沙箱写 Vars |
| extract | `session.save` | 把本次抓到的 cookies + 选定的 vars 持久化，供后续运行复用（`ttl` / `login`） |

UI 表单字段从 `GET /api/v1/registry/steps` 元数据自动渲染（`StepEditor.vue`）。

---

## 会话复用（Session Reuse）

### 为什么需要

很多站点（sub2api / NewAPI / GPT2IMAGE 等）只提供「邮箱 + 密码登录」换 token / Cookie，而每次登录都会被站点记成一个新设备。定时任务每 30 分钟跑一次，一天就是几十次登录；站点对设备数量 / 在线连接数做限制时，轻则频繁踢下线，重则直接锁号。

### 解法

把每个 collector 的登录态加密持久化下来，后续运行直接复用，只有确认会话不可用时才重新登录：

- **存什么**：cookies（含 domain / path / 过期时间等属性）、选定的 vars、User-Agent——浏览器里维持登录态的那一套
- **怎么存**：AES-GCM 加密后写入 `sessions` 表，master key 与凭证相同，进程重启后仍能读回；cookie / var 的**值永远不会出现在 API 响应里**
- **怎么复用**：`fetch.http` 默认 `use_session: true`——请求前加载已保存的 cookies / UA，响应里的 `Set-Cookie` 自动合并回会话；step config 里显式写的 `Cookie` header 优先
- **怎么判断失效**：`session.load` 恢复会话并算出 `vars.session_ok`（会话过期、没有 cookie / var、缺少 `require_vars` 都算不可用）；带上已保存的会话（cookie / UA）仍收到 401 / 403 时 `fetch.http` 会写 `vars.session_invalid`。多数模板用的是「探测请求 + `on_error: continue` + 脚本检查响应」：很多站点未登录时也返回 200 的登录页，只看状态码不够，所以脚本会再校验响应体特征
- **过期**：`session.save` 的 `ttl`（如 `168h`）写 `expires_at`，过期后 `session.load` 直接给 `session_ok = false`
- **Key**：默认 `collector:<collector_id>`；想让多个 collector 共享同一份登录态时，用步骤上的 `key`（`fetch.http` 里叫 `session_key`）覆盖

### 推荐流水线

密码登录站点的标准写法（内置模板 `sub2api-balance` / `newapi-balance` / `udealproxy-balance` / `gpt2image-balance` 都是这个模式）：

1. `input.credential` 取账号密码
2. `session.load` 恢复已有会话 → `vars.session_ok`
3. `fetch.http` 带会话试打一次受保护接口：`when: vars.session_ok`、`on_error: continue`，把状态码存进 `probe_status`
4. `script.js.transform` 检查 probe 的状态码 + 响应体特征，重新计算 `vars.session_ok`（probe 失败时状态变量不存在 → false）
5. 登录（`when: !vars.session_ok`）→ `parse.json` / `extract.jsonpath` 拿 `user_id` 等变量 → `session.save`（`login: true`、`vars: ["user_id"]`）
6. 重新 `fetch.http` 受保护接口（`when: !vars.session_ok`）→ `parse.json` → 抽指标；会话可用时直接用第 3 步的响应，跳过登录

```json
{
  "steps": [
    { "kind": "input.credential", "name": "load credential",
      "config": { "credential_id": 0, "var_name": "cred" } },
    { "kind": "session.load", "name": "load session",
      "config": { "require_vars": ["user_id"] } },
    { "kind": "fetch.http", "name": "probe protected", "when": "vars.session_ok", "on_error": "continue",
      "config": { "method": "GET", "url": "{{BASE_URL}}/api/user/self", "save_status_as": "probe_status" } },
    { "kind": "script.js.transform", "name": "recheck session",
      "config": { "source": "var s = Number(payload.vars.probe_status || 0); var body = String(payload.body || ''); return { vars: { session_ok: s === 200 && body.indexOf('\"data\"') >= 0 } };" } },
    { "kind": "fetch.http", "name": "login", "when": "!vars.session_ok",
      "config": {
        "method": "POST", "url": "{{BASE_URL}}/api/user/login",
        "headers": { "Content-Type": "application/json" },
        "body": "{\"username\":\"{{.vars.cred.username}}\",\"password\":\"{{.vars.cred.password}}\"}"
      } },
    { "kind": "parse.json", "name": "parse login", "when": "!vars.session_ok" },
    { "kind": "extract.jsonpath", "name": "extract user_id", "when": "!vars.session_ok",
      "config": { "mappings": [ { "name": "user_id", "path": "$.data.user_id", "type": "string" } ] } },
    { "kind": "session.save", "name": "save session", "when": "!vars.session_ok",
      "config": { "login": true, "ttl": "168h", "vars": ["user_id"] } },
    { "kind": "fetch.http", "name": "fetch protected", "when": "!vars.session_ok",
      "config": { "method": "GET", "url": "{{BASE_URL}}/api/user/self" } },
    { "kind": "parse.json", "name": "parse protected" },
    { "kind": "extract.jsonpath", "name": "extract balance",
      "config": { "mappings": [ { "name": "balance", "path": "$.data.balance", "type": "number" } ] } }
  ]
}
```

> 示例里的 probe 脚本只是演示：真实站点要按自己的成功返回判断（内置模板会检查 `"success":true`、`"data"`、Dashboard HTML 里的 `Credits Balance` 之类的特征）。probe 失败时 `on_error: continue` 会保留失败前的 payload，`probe_status` 不存在 → `session_ok=false` → 走登录分支。

### Step 配置

`session.load`（input 阶段）：

| key | 类型 | 说明 |
|---|---|---|
| `key` | string（模板） | 会话 key，默认 collector key |
| `require_vars` | string[] | 只有这些变量都存在且非空才算会话可用，如 `["user_id"]` |

写回 `vars`：`session_ok`、`session_key`、`session_updated_at`（RFC3339 或空串）、`session_login_count`，以及所有持久化的变量。

`session.save`（extract 阶段）：

| key | 类型 | 说明 |
|---|---|---|
| `key` | string（模板） | 同上 |
| `vars` | string[] | 要持久化的 payload 变量名，如 `["user_id"]` |
| `ttl` | string | 过期时间，如 `168h`；到期后 `session.load` 判定不可用 |
| `login` | bool | 标记为一次成功（重）登录：`login_count` +1、刷新 `last_login_at`、清掉旧的 TTL（同时配了 `ttl` 时按新值重设） |

写回 `vars`：`session_saved`。

`fetch.http` 与会话相关的配置：

| key | 类型 | 默认 | 说明 |
|---|---|---|---|
| `use_session` | boolean | `true` | 请求前加载、响应后回写会话 |
| `session_key` | string（模板） | `""`（= `collector:<id>`） | 覆盖会话作用域 |
| `user_agent` | string（模板） | `""` | 显式 UA；留空时优先复用会话里保存的 UA，并把本次生效的 UA 再存回去（保持设备指纹稳定） |
| `mark_login` | boolean | `false` | 把这次响应标为（重）登录：`login_count` +1、刷新 `last_login_at` |

`fetch.http` 写出的会话相关运行时变量：`session_reused`（是否用上了已保存的 cookies / UA）、`session_invalid`（带上了已保存的会话仍返回 401 / 403）、`session_key`、`session_cookie_count`。

> Dry Run 不会写持久会话：读取走真实会话（可复用），登录流程能在本次试跑里走通（cookie 只存在内存 overlay），但不会回写 / 落库，别指望靠 dry run 刷新登录态。

### 查看与清除

- **UI**：Collector 编辑页 → **Session** tab（已保存的 collector 才有），列出会话的 key / 状态 / cookie 数 / vars / 登录次数 / 最近使用与过期时间，支持单条 Clear 和 Clear all
- **REST API**：

```bash
TOKEN="<POST /api/v1/auth/login 的返回值>"

# 列出某个 collector 的会话
curl -s -H "Authorization: Bearer $TOKEN" \
     'http://localhost:7080/api/v1/sessions?collector_id=3' | jq

# 清除单条 / 清空该 collector 的全部会话
curl -s -X DELETE -H "Authorization: Bearer $TOKEN" 'http://localhost:7080/api/v1/sessions/1'
curl -s -X DELETE -H "Authorization: Bearer $TOKEN" 'http://localhost:7080/api/v1/collectors/3/sessions'
```

改密码 / 换账号 / 怀疑账号被锁时，先清会话再手动 Run 一次；删除 collector 时它的会话会一起删掉。

### 安全与注意

- 会话 payload 用与凭证相同的 AES-GCM master key 加密落库；cookie 值 / 变量值等价于登录态，**不要在脚本里打印，也不要期望 API 返回**。换 `master_key` 会让旧凭证和旧会话都解不出来（和旧凭证一样）；解不开的会话按「没有会话」处理，下次运行会重新登录并覆盖旧记录，但凭证本身仍需重新保存
- 会话是「每个 collector 一份、单实例」的设计：同一个 collector 不会被并发运行；但多个 collector 若配了同一个 `session_key`，并发运行时会话以最后一次落库为准，没有跨运行协调，多实例部署更不保证一致
- **从旧模板建的 collector 不会自动升级**：重新用更新后的模板建一个（或手动往流水线里补 `session.load` / `session.save` 和 `when` 条件），才有「会话可用时跳过登录」的效果；不升级的 collector 也会因为 `fetch.http` 默认 `use_session: true` 而复用已保存的 cookie / UA（登录请求的 `Set-Cookie` 会被自动保存），但每次运行仍会完整走一遍登录

---

## 内置模板一览

| 名称 | 适用 | 凭证 | 调度默认 |
|---|---|---|---|
| `deepseek-balance` | DeepSeek 官方 API `/user/balance`（CNY） | API Key | interval 30m |
| `sub2api-balance` | sub2api 风格网关：邮箱+密码登录（会话复用） | Email + Password | interval 30m |
| `newapi-balance` | [QuantumNous/new-api](https://github.com/QuantumNous/new-api) cookie session 登录（会话复用） | Username + Password | interval 30m |
| `newapi-balance-accesstoken` | NewAPI 访问令牌直取，免登录（新版本用 `nap_` 前缀 scoped token，需 `profile:read`；也可兼容填在 API Key 字段） | Access Token + User ID（旧版需要，可选） | interval 30m |
| `rixapi-balance-accesstoken` | RixAPI / NewAPI 商业分支（同样兼容 `nap_` scoped token） | Access Token + User ID（旧版需要，可选） | interval 30m |
| `udealproxy-balance` | UDealProxy 海外住宅代理：邮箱+密码（md5）登录，取住宅 GB 余额（会话复用） | Email + Password | interval 30m |
| `gpt2image-balance` | GPT2IMAGE 积分查询：邮箱+密码登录，从 Dashboard HTML 提取积分（会话复用） | Email + Password | interval 30m |

其中四个密码登录模板（`sub2api-balance` / `newapi-balance` / `udealproxy-balance` / `gpt2image-balance`）已内置会话复用：首次登录后持久化 cookies / UA / 关键变量（gpt2image 只用到 cookie），后续调度运行先探测会话、可用时跳过登录（见上文「会话复用」一节）。

模板源码：`internal/templates/builtin.go`。运行时也能 `GET /api/v1/templates` 拿全量定义。

---

## 部署

### Docker Compose（推荐）

仓库根的 `docker-compose.yaml` 直接用 `sunshow/siphongear:latest`，数据走命名卷：

```bash
cp .env.example .env
# 编辑 .env：填 SIPHON_AUTH__MASTER_KEY 和 SIPHON_AUTH__JWT_SECRET（长随机串）

docker compose pull
docker compose up -d
docker compose logs -f siphongear
```

两个密钥用 `${VAR:?...}` 强校验，没设直接 `up` 会失败。

升级：`docker compose pull && docker compose up -d`。

清空数据重来：`docker compose down -v`。

### 命名卷 vs Bind Mount

默认 compose 用**命名卷** `siphongear-data`，省事。容器内进程是非 root 用户 `siphon` (uid 1000)，命名卷 Docker 会自动按容器里 `/app/data` 的所有权初始化，开箱即用。

如果改成 bind mount（`./data:/app/data`），**必须先把宿主目录 chown 到 1000**：

```bash
mkdir -p ./data
sudo chown -R 1000:1000 ./data
```

否则启动会看到这样一条**误导性**报错：

```
{"level":"error","error":"unable to open database file: out of memory (14)","message":"open db"}
```

`out of memory` 是 SQLITE_CANTOPEN（错误码 14）的文案，**真实原因是宿主目录权限不让 uid 1000 写 WAL/SHM**。chown 一下，或继续用命名卷。

### 自己 build Docker 镜像

```bash
docker build -t siphongear .
docker run -p 7080:7080 \
  -e SIPHON_AUTH__MASTER_KEY=$(openssl rand -hex 32) \
  -e SIPHON_AUTH__JWT_SECRET=$(openssl rand -hex 32) \
  -v siphongear-data:/app/data \
  siphongear
```

---

## 配置参考

所有 yaml key 都能通过环境变量覆盖：`SIPHON_<SECTION>__<KEY>`（大写，路径段之间双下划线）。比如 `auth.master_key` → `SIPHON_AUTH__MASTER_KEY`。

```yaml
server:
  host: 0.0.0.0
  port: 7080

database:
  driver: sqlite          # sqlite | mysql | postgres
  dsn: data/siphongear.db

auth:
  jwt_secret: <required>   # JWT 签名 key，长随机串
  master_key: <required>   # AES-GCM 加密凭证 payload，长随机串（>= 32 字节）
  init_username: admin     # 首次启动若无用户则按此创建
  init_password: admin
  token_ttl_hrs: 168

log:
  level: info
  pretty: true

browser:
  ws_url: ""              # 远程 chromedp endpoint，比如 ws://chrome:9222；空则本机找 Chrome

runner:
  max_concurrency: 4      # 同时跑多少 collector
  default_timeout: 60     # 单次 run 全局超时（秒），collector 自身 timeout 优先

history:
  retention_days: 7       # 历史保留天数，必须为正整数；环境变量 SIPHON_HISTORY__RETENTION_DAYS
```

历史清理在启动后执行一次，之后每小时执行一次，默认保留最近 7 天（每一天按 24 小时计）。过期且已结束的 Run、对应 Step Logs / Data Points，以及过期通知日志会分批永久删除；正在运行或在保留期内才结束的任务会保留。Collector、Indicator 等配置和告警状态不受影响。修改 `history.retention_days` 或环境变量后重启生效；Docker Compose 可直接在 `.env` 设置 `SIPHON_HISTORY__RETENTION_DAYS`。

> ⚠️ `master_key` 一旦换了，旧凭证就解不出来了，**别在生产上随便改**。

---

## 概念模型

```
Site -- 1..N --> Credentials
Site -- 1..N --> Collectors
Collector -- 有 1 条 --> Pipeline (JSON, 一组 Steps)
Collector -- 0..N --> Sessions (AES-GCM 加密的 cookies / 选定 vars / UA，默认 1 条)
Collector -- 1..N --> Indicators
Indicator -- 1..N --> DataPoints (时序)
Run -- 1..N --> StepLogs
ThresholdRule -- 通过 indicator_key + 可选 tag --> 影响 Indicator 渲染
```

Pipeline 是扁平的有序 Step 列表；每个 Step 有 `kind`（已注册的步骤类型）和 `config` 对象。Step 之间通过 `Payload.Vars` 传值，body 与 parsed object 也会跨步保留。每个 Step 还可带 `when` / `on_error` 条件字段（见「流水线 Pipeline」）。密码登录的 collector 还会用 `sessions` 表保存登录态：`fetch.http` 默认读写会话，`session.load` / `session.save` 控制何时恢复与落盘（见「会话复用」）。

---

## REST API

> 下列 `/api/v1/*` 是面板内部使用的 JWT 鉴权 API。要做 **外部集成** 建议优先看 [API.md](./API.md) 中描述的 API Key 公开端点。

Base：`/api/v1`。除了 `/auth/login` 和 `/healthz`，其它都要 `Authorization: Bearer <token>`。

```
POST   /auth/login                         { username, password } -> { token, user }
GET    /auth/me
POST   /auth/password                      { old_password, new_password }

GET    /registry/steps                     UI 表单元数据
GET    /templates                          内置 + 用户模板
GET    /templates/:name

CRUD   /sites
CRUD   /credentials                        payload 加密落库；编辑时 GET /credentials/:id 返回解密 payload 以便回填表单
CRUD   /collectors

GET    /sessions?collector_id=             持久化会话列表（只返回元信息，不含 cookie / var 值）
DELETE /sessions/:id                       删除单条会话
DELETE /collectors/:id/sessions            清空该 collector 的全部会话

POST   /collectors/:id/run                 同步触发一次，返回结果
POST   /collectors/:id/dryrun              试跑（不写库），返回每步日志
GET    /collectors/:id/runs?limit=50&offset=0&from=&to=&indicator_id=
GET    /collectors/:id/datapoints?indicator_id=&from=&to=&limit=&offset=
GET    /collectors/:id/indicators
POST   /collectors/:id/indicators
PUT    /indicators/:id
DELETE /indicators/:id

GET    /runs/:id                           run + 全部 step logs
GET    /dashboard                          所有指标 + 最新值，按 site 分组（带 tag、规则匹配）

CRUD   /rules                              阈值告警规则（含 notify_channel_ids、notify_title_tpl、notify_body_tpl）
POST   /rules/preview-notify               用 mock 上下文渲染 title/body 模板

GET    /notify/types                       已注册的通知类型 + schema
CRUD   /notify/channels                    通知实例，payload AES-GCM 加密
POST   /notify/channels/:id/test           试发
GET    /notify/logs?channel_id=&rule_id=&collector_id=&limit=
```

Runs 接口仍返回数组，每项包含 `data_points`；响应头 `X-Total-Count` 为筛选后总数。Runs 的 `limit` 范围为 1–200，Data Points 为 1–5000；`offset` 从 0 开始。面板这两个历史接口均按时间降序（同时间按 ID 降序），`from` / `to` 为带时区的 RFC3339 时间，采用左闭右开 `[from, to)`；按日期查询时 `to` 应传结束日期的次日零点。公开 API 的历史接口保持原有行为。

会话管理端点（`GET /sessions` / `DELETE /sessions/:id` / `DELETE /collectors/:id/sessions`）同样是 JWT 鉴权，字段与示例见 [API.md 第 3 节](./API.md#3-会话管理面板内部jwt-鉴权)。

---

## 进阶

### 加一个新内置 Step

1. 在 `internal/steps/<stage>/` 下新建 go 文件
2. 实现 `pipeline.Step` 接口（`Kind() string` + `Run(ctx, in *Payload) (*Payload, error)`）
3. `init()` 里 `pipeline.Register(StepMeta{Kind, Stage, Description, Schema}, factory)`
4. `Schema` 字段会被前端 `StepEditor.vue` 自动渲染成表单
5. 新增的 stage 子包记得在 `internal/steps/steps.go` 的 blank import 加进去

### 加一个新内置模板

编辑 `internal/templates/builtin.go`：

```go
Register(Template{
    Name:            "my-template",
    Description:     "...",
    NeedsCredential: true,
    CredentialHint:  &TemplateCredentialHint{Type: "token", Fields: ...},
    Variables:       []TemplateVariable{...},
    Pipeline:        pipeline.Definition{Steps: []pipeline.StepConfig{...}},
    Indicators:      []TemplateIndicator{...},
})
```

约定：

- 前端变量替换用 `{{BASE_URL}}` 形式（保存前替换成实际值）
- 运行时模板用 `{{.vars.foo}}`（Go text/template，每次 run 求值）
- 凭证字段访问 `{{.vars.<var_name>.<field>}}`，`<var_name>` 默认 `cred`

### 加一个新通知类型

1. 在 `internal/notify/<type>/` 下新建 go 文件，实现 `notify.Notifier`：`Type() string` + `Send(ctx, msg notify.Message) error`
2. 提供 `Factory` 函数 `func(payload map[string]any) (notify.Notifier, error)`，从解密后的 payload 构造实例
3. `init()` 里 `notify.Register(notify.Meta{Type, Description, Schema}, factory)`；schema 用 `string / number / boolean` 字段（标 `secret: true` 的会走 password input）
4. 在 `internal/notify/builtin/builtin.go` 加 `_ "..."` blank import 让其被注册

前端 Notifications 页面按 schema 自动渲染表单，无需任何 UI 改动。

### 不开 UI，仅通过 API 集成

直接调上面 REST API 即可。典型流程：`POST /auth/login` 拿 token → `POST /sites` → `POST /credentials` → `POST /collectors`（带 pipeline JSON）→ `POST /collectors/:id/run`。

---

## 项目结构

```
siphongear/
├── cmd/server/                 入口
├── internal/
│   ├── api/                    Gin 路由 & handlers
│   ├── auth/                   bcrypt + JWT
│   ├── config/                 koanf yaml + env
│   ├── crypto/                 AES-GCM
│   ├── events/                 内存 pub/sub
│   ├── notify/                 通知类型注册表 + Dispatcher（订阅 run.completed）
│   │   ├── builtin/            内置类型 blank import 集中处
│   │   └── serverchan/         ServerChan 实现
│   ├── pipeline/               Step / Engine / Registry
│   ├── runner/                 触发 collector，落库 run/step/datapoints
│   ├── rules/                  阈值规则引擎
│   ├── scheduler/              cron + 事件订阅
│   ├── session/                会话状态（cookies / vars / UA）加密持久化
│   ├── steps/                  内置 step
│   ├── store/                  GORM 模型 + 迁移
│   └── templates/              内置任务模板
├── pkg/logger/                 zerolog 包装
├── web/                        Vue 3 SPA，go:embed 嵌入
├── config.yaml.example
├── .env.example
├── Dockerfile
├── docker-compose.yaml
├── Jenkinsfile
└── Makefile
```

---

## 开发

```bash
make tidy        # go mod tidy
make test        # go test ./...
make web         # 只打前端
make server      # 只打后端
make build       # 前后端一起
make clean       # 清掉 bin/ web/dist/ data/
```

`internal/pipeline/engine_test.go` 跑端到端流水线（static + jsonpath）。要对接真实站点验证，可以用任一 sub2api 风格网关复现 `sub2api-balance` 模板。

---

## 已知限制

- 没有专门的指标历史曲线大图页（数据已落 `data_points`，目前在 Collector History 中随 Run 展示）
- 单二进制单实例，多实例需引入分布式锁
- 会话复用同样假设单实例：同一个 collector 不会被并发运行，但多个 collector 配了同一个 `session_key` 并并发运行时，会话以最后一次落库为准，没有跨运行协调
- `fetch.browser` 在 macOS 默认拉本机 Chrome；远程模式通过 `BROWSER_WS_URL` 接 chrome:devtools

---

## English summary

SiphonGear is a configuration-driven, single-binary collector + dashboard. Define an `Input → Fetch → Transform → Parse → Extract` pipeline per task, schedule it (interval / cron / event), and watch values flow into a time-series dashboard with tag filtering and threshold-based alert rules. Rules can fan out to pluggable notification channels (ServerChan built in) on OK→alert / alert→OK transitions, with optional Go-template title/body. Credentials and channel payloads are AES-GCM encrypted at rest. Password-login collectors also persist an encrypted per-collector session (cookies + selected vars + user agent) and reuse it on later runs; the `session.load` / `session.save` steps plus per-step `when` / `on_error` conditions re-login only when the saved session stops working. Defaults to pure-Go SQLite (no cgo); MySQL / Postgres also supported. Originally built for tracking recharge balances across many LLM gateways, generalized to any periodic web/API metric collection. UI in Vue 3 + Element Plus, embedded via `go:embed`.

---

## License

Personal project — no license declared yet.
