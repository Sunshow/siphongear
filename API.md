# SiphonGear 对外 HTTP API

本文只描述用 **API Key** 鉴权的对外只读接口（`/api/public/*`），适合把 SiphonGear 采集到的指标接进 Grafana / 自建大盘 / 飞书机器人 / 自动化脚本等外部系统。

面板内部用的 `/api/v1/*` 走 JWT 鉴权，不在本文范围，简表见 [README.md](./README.md#rest-api)。

---

## 鉴权

进入面板的 **API Keys** 页面创建一个 key，会一次性返回明文，形如：

```
sg_<prefix>_<secret>
```

> 明文仅在 **创建** 和 **轮换**（`POST /api/v1/api-keys/:id/rotate`）时返回。遗失只能 rotate，不能找回。

### 请求时如何带

任选其一，优先级从高到低：

1. Header `X-API-Key`：

   ```
   X-API-Key: sg_AB3KXXXX_YYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYY
   ```

2. Header `Authorization: Bearer`：

   ```
   Authorization: Bearer sg_AB3KXXXX_YYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYY
   ```

3. URL query 参数 `api_key`：

   ```
   GET /api/public/indicators?api_key=sg_AB3KXXXX_YYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYY
   ```

> URL query 方式仅推荐用在 **不能自定义 header** 的场景（iframe 嵌入、某些 Grafana 数据源、简单 webhook 等）。Key 会出现在反代 / CDN / 浏览器历史 / access log 里，能用 header 就别用 query。

### 错误码

| HTTP | body | 含义 |
|------|------|------|
| 401 | `{"error":"missing api key"}` | 没带任何 token |
| 401 | `{"error":"invalid api key"}` | 格式不对 / prefix 不存在 / secret 不匹配 |
| 401 | `{"error":"api key disabled"}` | key 存在但已被禁用 |

每次成功调用会异步刷新该 key 的 `last_used_at`（节流到 1 分钟一次）。

---

## Base URL

```
http(s)://<host>:<port>/api/public
```

默认端口 `7080`。

---

## 1. `GET /api/public/indicators`

获取 **所有当前可见指标的最新值**，等同于面板 Dashboard 上的卡片列表。

`hidden=true` 的 indicator 不会返回。

### Query 参数（全部可选）

| 参数 | 类型 | 说明 |
|------|------|------|
| `collector_id` | uint | 仅返回该 Collector 下的指标 |
| `site_id` | uint | 仅返回属于该 Site 的指标 |
| `indicator_key` | string | 按 indicator key 精确过滤 |
| `tag` | string | 按 site tag 精确过滤（单个，不支持多选） |

### 响应

`200 OK`，body 是一个 **数组**（不是 `{data: [...]}`）。每个元素：

| 字段 | 类型 | 说明 |
|------|------|------|
| `collector_id` | uint | 所属 Collector ID |
| `collector_name` | string | Collector 名称 |
| `site_id` | uint | 所属 Site ID |
| `site_name` | string | Site 名称 |
| `site_base_url` | string | Site 的 Base URL |
| `indicator_id` | uint | Indicator ID |
| `key` | string | Indicator key（程序友好的标识，如 `balance`） |
| `name` | string | Indicator 显示名 |
| `type` | string | `number` / `string` / `bool` / `json` |
| `unit` | string | 单位，如 `USD` / `CNY` |
| `display` | string | `gauge` / `line` / `table` |
| `value_num` | number\|null | 当前数值（`type=number` 时非空） |
| `value_str` | string\|null | 当前字符串值 |
| `value_json` | string\|null | 当前 JSON 值（已序列化为字符串） |
| `ts` | string\|null | 当前值的采集时间（RFC3339） |
| `prev_value_num` | number\|null | 上一次的数值，用于趋势/变化展示 |
| `prev_value_str` | string\|null | 上一次的字符串值 |
| `prev_value_json` | string\|null | 上一次的 JSON 值 |
| `prev_ts` | string\|null | 上一次值的时间 |
| `site_tags` | string[] | 该 Site 的 tag 列表 |
| `matched_rule` | object\|null | 命中的阈值规则（仅在命中时存在） |
| `last_status` | string | Collector 上次运行状态（`success` / `error` / ...） |

`matched_rule` 的结构：

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | uint | 规则 ID |
| `name` | string | 规则名 |
| `severity` | string | 严重级别 |
| `actions` | string[] | 触发的 action 类型，如 `["indicator_color"]` |

注意：`value_num` / `value_str` / `value_json` 三个字段按 indicator `type` 三选一非空，其余为 `null`。

### 示例

请求：

```bash
# 用 header（推荐）
curl -H "X-API-Key: sg_AB3KXXXX_YYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYY" \
     'http://localhost:7080/api/public/indicators?indicator_key=balance&tag=prod'

# 或用 query（不能改 header 的场景）
curl 'http://localhost:7080/api/public/indicators?api_key=sg_AB3KXXXX_YYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYY&indicator_key=balance&tag=prod'
```

响应：

```json
[
  {
    "collector_id": 12,
    "collector_name": "deepseek-balance",
    "site_id": 3,
    "site_name": "DeepSeek Prod",
    "site_base_url": "https://api.deepseek.com",
    "indicator_id": 27,
    "key": "balance",
    "name": "Account Balance",
    "type": "number",
    "unit": "USD",
    "display": "gauge",
    "value_num": 12.34,
    "value_str": null,
    "value_json": null,
    "ts": "2026-05-28T03:21:11Z",
    "prev_value_num": 13.10,
    "prev_value_str": null,
    "prev_value_json": null,
    "prev_ts": "2026-05-28T02:21:08Z",
    "site_tags": ["prod", "llm"],
    "matched_rule": {
      "id": 4,
      "name": "low-balance",
      "severity": "warning",
      "actions": ["indicator_color"]
    },
    "last_status": "success"
  }
]
```

---

## 2. `GET /api/public/indicators/:collector_id/:indicator_key/history`

获取 **单个指标的时间序列**（DataPoints），可按时间窗口和数量切片。

### Path 参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `collector_id` | uint | 是 | Collector ID |
| `indicator_key` | string | 是 | Indicator key |

### Query 参数

| 参数 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `from` | RFC3339 | 无 | `ts >= from`（如 `2026-05-01T00:00:00Z`） |
| `to`   | RFC3339 | 无 | `ts <= to` |
| `limit`| int | 500 | 返回点数上限；最大 5000，超过会被截断 |

返回顺序按 `ts` **升序**。

### 错误

| HTTP | body | 含义 |
|------|------|------|
| 400 | `{"error":"invalid collector_id"}` | path 里 collector_id 非数字或为 0 |
| 400 | `{"error":"indicator_key is required"}` | indicator_key 为空 |
| 404 | `{"error":"indicator not found"}` | 指定 collector 下找不到该 key |

### 响应

`200 OK`：

```json
{
  "indicator": {
    "id": 27,
    "collector_id": 12,
    "key": "balance",
    "name": "Account Balance",
    "type": "number",
    "unit": "USD",
    "display": "gauge",
    "hidden": false,
    "created_at": "2026-04-01T10:00:00Z",
    "updated_at": "2026-05-28T03:21:11Z"
  },
  "points": [
    {
      "id": 9921,
      "collector_id": 12,
      "indicator_id": 27,
      "run_id": 33012,
      "value_num": 12.34,
      "value_str": null,
      "value_json": null,
      "ts": "2026-05-28T03:21:11Z"
    }
  ]
}
```

`points[].value_num / value_str / value_json` 同样按 indicator `type` 三选一非空。

### 示例

```bash
# 用 header
curl -H "X-API-Key: sg_AB3KXXXX_YYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYY" \
     'http://localhost:7080/api/public/indicators/12/balance/history?from=2026-05-20T00:00:00Z&limit=200'

# 用 query
curl 'http://localhost:7080/api/public/indicators/12/balance/history?api_key=sg_AB3KXXXX_YYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYY&from=2026-05-20T00:00:00Z&limit=200'
```

---

## 端到端最小用例

```bash
# 1) 在面板 API Keys 页面点 "Create"，把返回的 plaintext 存好（只显示这一次）
KEY="sg_AB3KXXXX_YYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYY"

# 2) header 方式：拉所有 prod 标签下的 balance 指标
curl -s -H "X-API-Key: $KEY" \
     'http://localhost:7080/api/public/indicators?indicator_key=balance&tag=prod' | jq

# 3) query 方式（iframe / Grafana 等不能改 header 的场景）
curl -s "http://localhost:7080/api/public/indicators?api_key=$KEY&indicator_key=balance&tag=prod" | jq

# 4) 拉某个 collector 的 balance 历史
curl -s -H "X-API-Key: $KEY" \
     'http://localhost:7080/api/public/indicators/12/balance/history?limit=500' | jq
```

---

## 限制

- 公开面 **只有** 上面两个只读端点；新建采集任务、触发运行、改规则、改通知等仍要走 `/api/v1/*`（JWT 鉴权）。
- header 与 query 同时存在时按 **header 优先**（`X-API-Key` > `Authorization: Bearer` > `?api_key=`）。
- query 方式建议只在已经 TLS 终结的反代后面使用；URL 里的 key 会被 CDN / access log / 浏览器历史持久化，泄露后请尽快 `POST /api/v1/api-keys/:id/rotate`。
- 没有针对 API Key 的请求频次限流，建议在反向代理层加 rate limit。
- API Key 不区分 scope / 权限粒度，能调就能读到所有非 hidden 指标，请按"读取整个仪表盘"为粒度授予。
- 明文 plaintext 仅在创建 / 轮换时返回，遗失只能 `POST /api/v1/api-keys/:id/rotate` 重新生成。
