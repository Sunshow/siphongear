# AGENTS.md

## 项目定位

SiphonGear 是一个**配置驱动的通用采集与指标平台**。最初目标是定时查询多个网站的充值余额，泛化成一个可扩展的 Web 爬虫/采集工具：每个采集任务（Collector）由一条 `Input → Fetch → Transform → Parse → Extract` 流水线组成，每个环节都是可插拔的 Step；Web 界面配置任务、调度、指标，所有结果落库形成时间序列。

参考项目（架构灵感来源，非依赖）：`/Users/sunshow/GIT/ChoutiRev/ct-collector`。

## 核心架构

- HTTP API（Gin）：CRUD + 触发 + 试运行 + 仪表盘
- 流水线引擎（`internal/pipeline`）：注册表 + 顺序执行 + StepListener 观察
- 内置 Step（`internal/steps/*`）：input / fetch / transform / parse / extract / script.js
- 调度器（`internal/scheduler`）：robfig/cron 支持 interval / cron / event 三种模式
- 数据层（`internal/store`）：GORM + SQLite（默认，glebarez 纯 Go 驱动），可换 MySQL / Postgres
- 凭证 AES-GCM 加密（`internal/crypto`）；JWT 单用户认证（`internal/auth`）
- 模板（`internal/templates`）：内置任务模板，前端"From Template"一键应用
- 前端（Vue 3 + TS + Element Plus + Pinia + Vue Router + ECharts），通过 `go:embed` 嵌入二进制

## 编码约定

- 日志：zerolog（`pkg/logger`），结构化输出
- JSON：使用 `bytedance/sonic`，不使用 `encoding/json`
- HTTP 客户端：`go-resty`
- 浏览器：`chromedp`（fetch.browser step）
- 脚本沙箱：`dop251/goja`（script.js.* steps），默认禁网络
- 扩展点：
  - 新 Step：`pipeline.Register(StepMeta{...}, factory)` 在包 `init()` 注册
  - 新 Template：`templates.Register(...)` 在 `internal/templates/builtin.go` 注册
- 加密：所有 Credential payload 经 AES-GCM 加密落库；返回 API 时永远不解密
- 安全：脚本 Step 默认禁用 `http.get/post`，需显式 `allow_http: true`；脚本超时通过 `runtime.Interrupt`

## 快速命令

```bash
# 后端
go build ./...                    # 编译
go test ./...                     # 跑测试（pipeline 引擎 e2e）
go vet ./...                      # 静态检查
go run ./cmd/server               # 直跑

# 前端
cd web && npm install && npm run dev    # 开发模式（vite proxy 到 :7080）
cd web && npm run build                  # 产物到 web/dist，被 go:embed 打入

# 一键
make build      # web + server
make run        # ./bin/siphongear --config config.yaml
make test
```

启动需要的 env / config：
- `auth.master_key`（或 `SIPHON_AUTH__MASTER_KEY`）：32+ 字节，AES 加密密钥
- `auth.jwt_secret`（或 `SIPHON_AUTH__JWT_SECRET`）：JWT 签名
- 默认 SQLite 路径：`data/siphongear.db`
- 首启动若无 user，按 `auth.init_username` / `auth.init_password` 创建（默认 `admin/admin`）

## 目录速查

| 路径 | 作用 |
|---|---|
| `cmd/server/main.go` | 入口；装配 db、cipher、jwt、bus、runner、scheduler、router |
| `internal/pipeline/types.go` | Step / Payload / Definition / Context 接口 |
| `internal/pipeline/engine.go` | 顺序执行、step 监听、indicator 抽取 |
| `internal/pipeline/registry.go` | Step 工厂注册表 + 元信息 |
| `internal/steps/<stage>/*.go` | 内置 Step 实现，每个文件 `init()` 注册自己 |
| `internal/runner/runner.go` | 触发任务、写 Run / StepLog / DataPoint、发事件 |
| `internal/scheduler/scheduler.go` | 把 enabled collector 注册到 cron / event 总线 |
| `internal/store/models/models.go` | GORM 表结构 |
| `internal/api/router.go` + `handlers.go` | REST 路由与 handlers |
| `internal/templates/{templates.go,builtin.go}` | 模板类型 + 内置模板（如 `sub2api-balance`） |
| `web/src/views/*.vue` | 前端页面 |
| `web/src/components/*.vue` | StepEditor / TemplatePicker / IndicatorsEditor / DryRunPanel |

## 常见任务怎么做

### 加一个新内置 Step

1. 在 `internal/steps/<stage>/` 下新建 go 文件
2. 实现 `pipeline.Step` 接口（`Kind()` + `Run(ctx, in)`）
3. 在 `init()` 里 `pipeline.Register(StepMeta{Kind, Stage, Description, Schema}, factory)`
4. `Schema` 里的字段会被前端 `StepEditor.vue` 动态渲染成表单
5. 内置 step package 已通过 `internal/steps/steps.go` 集中 blank import；新加的 stage 子包记得在 `steps.go` 加进去

### 加一个新 Template

1. 在 `internal/templates/builtin.go` 加 `Register(Template{...})`
2. 必填：`Name / Description / NeedsCredential / CredentialHint / Pipeline / Indicators`
3. 流水线里需要替换的占位符用 `{{BASE_URL}}` 这样的形式（前端 substitute），凭证字段用 `{{.vars.cred.fieldName}}`（运行时模板）

### 重置数据

```bash
rm -rf data/      # 整个 SQLite + 历史
```

## 测试

- `internal/pipeline/engine_test.go`：端到端跑 static + jsonpath 两条用例
- 实际接口验证可以通过任意 sub2api 风格的网关复现 `sub2api-balance` 模板

## 已知限制

- 前端没有专门的指标历史曲线图（数据已落 `data_points` 表，只展示在 CollectorRuns 的 Data Points tab）
- 单二进制单实例，多实例需引入分布式锁
- 浏览器 step 在 macOS 默认会拉本机 Chrome，远程模式通过 `BROWSER_WS_URL` 接 chrome:devtools

## 边界与禁忌

- 不要把 master_key / jwt_secret 提交到 git
- script step 内勿暴露 `os` / 文件系统，别破坏沙箱
- 凭证 payload 在 API 层永远不返回明文；GET /credentials/:id 不解密
- HTTP step 模板里如果用了 `Authorization: Bearer {{.vars.token}}` 这种敏感 header，运行日志的 snippet 已截断到 600 字符且仅记录 body / 部分输出，不会回写完整 token
