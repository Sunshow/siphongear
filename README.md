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

- **可视化流水线**：每个采集任务由 `Input → Fetch → Transform → Parse → Extract` 五段组成，15 个内置 Step（HTTP / 浏览器 / JSONPath / CSS 选择器 / 正则 / JS 沙箱…），全部图形化配置
- **模板一键创建**：选模板 → 填账号 → Apply，Site / Credential / Collector / Pipeline / Indicators 一次到位
- **三种调度方式**：固定间隔、cron 表达式、事件链式触发（A 跑完触发 B）
- **Site Tags + Dashboard 过滤**：上百个 collector 也能按 tag 多选分组
- **阈值告警规则（Rules）**：指标 `<` / `>` / `=` 阈值时仪表盘卡片高亮，可按 tag 限定生效范围
- **多通道告警通知**：规则可挂多个通知实例，仅在 OK→alert / alert→OK 翻转时推送，避免刷屏；首期内置 ServerChan，类型可扩展；title / body 支持 Go `text/template` 自定义，留空走默认
- **Dashboard 自动刷新**：默认 30s 一次，可关，间隔可调（10/30/60/120/300s），一键「全部触发一次」
- **指标历史曲线**：每个指标都有时序数据，可查询 / 画图
- **Dry Run**：不写库试跑流水线，立即查看每个 Step 的输出片段
- **Save as Template**：把调好的 Collector 一键存成自己的模板复用
- **凭证 AES-GCM 加密**：永远不在 API 回写明文，运行时仅注入到 header / body
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

- 编辑器分四块：基本信息 / Pipeline 步骤 / Indicators 绑定 / Schedule
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

### 内置 Step

| 阶段 | Kind | 说明 |
|---|---|---|
| input | `input.static` | 注入字面量到 Vars |
| input | `input.credential` | 按 ID 取出加密凭证，解密注入 Vars |
| input | `script.js.input` | goja JS 沙箱（默认禁网络） |
| fetch | `fetch.http` | Resty，URL / Headers / Body 支持 `{{.vars.x}}` 模板 |
| fetch | `fetch.browser` | chromedp 渲染 / 求值 |
| transform | `transform.gunzip` | gzip 解压 |
| transform | `transform.charset` | 按 Content-Type 转码 |
| transform | `transform.template` | text/template 渲染当前 Body |
| transform | `script.js.transform` | JS 沙箱改 Body / Vars |
| parse | `parse.json` | sonic 解析为 Object |
| parse | `parse.html` | goquery 文档 |
| extract | `extract.jsonpath` | ohler55/ojg，按映射写 Vars |
| extract | `extract.css` | CSS 选择器 + 可选属性 |
| extract | `extract.regex` | 命名分组直接成 Vars |
| extract | `script.js.extract` | JS 沙箱写 Vars |

UI 表单字段从 `GET /api/v1/registry/steps` 元数据自动渲染（`StepEditor.vue`）。

---

## 内置模板一览

| 名称 | 适用 | 凭证 | 调度默认 |
|---|---|---|---|
| `deepseek-balance` | DeepSeek 官方 API `/user/balance`（CNY） | API Key | interval 30m |
| `sub2api-balance` | sub2api 风格网关：邮箱+密码登录 | Email + Password | interval 30m |
| `newapi-balance` | [QuantumNous/new-api](https://github.com/QuantumNous/new-api) cookie session 登录 | Username + Password | interval 30m |
| `newapi-balance-accesstoken` | NewAPI 访问令牌直取，免登录 | Access Token + User ID | interval 30m |
| `rixapi-balance-accesstoken` | RixAPI / NewAPI 商业分支 | Access Token + User ID | interval 30m |
| `gpt2image-balance` | GPT2IMAGE 积分查询：邮箱+密码登录，从 Dashboard HTML 提取积分 | Email + Password | interval 30m |

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
```

> ⚠️ `master_key` 一旦换了，旧凭证就解不出来了，**别在生产上随便改**。

---

## 概念模型

```
Site -- 1..N --> Credentials
Site -- 1..N --> Collectors
Collector -- 有 1 条 --> Pipeline (JSON, 一组 Steps)
Collector -- 1..N --> Indicators
Indicator -- 1..N --> DataPoints (时序)
Run -- 1..N --> StepLogs
ThresholdRule -- 通过 indicator_key + 可选 tag --> 影响 Indicator 渲染
```

Pipeline 是扁平的有序 Step 列表；每个 Step 有 `kind`（已注册的步骤类型）和 `config` 对象。Step 之间通过 `Payload.Vars` 传值，body 与 parsed object 也会跨步保留。

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
CRUD   /credentials                        payload 加密落库，GET 不解密
CRUD   /collectors

POST   /collectors/:id/run                 同步触发一次，返回结果
POST   /collectors/:id/dryrun              试跑（不写库），返回每步日志
GET    /collectors/:id/runs?limit=50
GET    /collectors/:id/datapoints?indicator_id=&from=&to=&limit=
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

- 没有专门的指标历史曲线大图页（数据已落 `data_points`，目前在 CollectorRuns 的 Data Points tab 展示）
- 单二进制单实例，多实例需引入分布式锁
- `fetch.browser` 在 macOS 默认拉本机 Chrome；远程模式通过 `BROWSER_WS_URL` 接 chrome:devtools

---

## English summary

SiphonGear is a configuration-driven, single-binary collector + dashboard. Define an `Input → Fetch → Transform → Parse → Extract` pipeline per task, schedule it (interval / cron / event), and watch values flow into a time-series dashboard with tag filtering and threshold-based alert rules. Rules can fan out to pluggable notification channels (ServerChan built in) on OK→alert / alert→OK transitions, with optional Go-template title/body. Credentials and channel payloads are AES-GCM encrypted at rest. Defaults to pure-Go SQLite (no cgo); MySQL / Postgres also supported. Originally built for tracking recharge balances across many LLM gateways, generalized to any periodic web/API metric collection. UI in Vue 3 + Element Plus, embedded via `go:embed`.

---

## License

Personal project — no license declared yet.
