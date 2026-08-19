# SMS688 → NewAPI 账号自动化设计

**日期：** 2026-08-18  
**状态：** 已完成章节评审，等待书面规格确认  
**范围：** 独立的管理界面与编排服务；通过公开 API 调用 SMS688，并更新、测试 NewAPI Codex 渠道

## 1. 目标

提供一个独立的管理界面。管理员一次粘贴一个或多个 GPT 账号资料后，系统自动：

1. 调用 SMS688 Account API 创建自动化任务；
2. 查询每个账号的认证、接码和凭据生成状态；
3. 在任务完成后立即下载 CPA JSON 或 ZIP；
4. 校验并解析每个成功账号的 Codex 认证 JSON；
5. 将账号稳定映射到 NewAPI 渠道池中的一个 Codex 渠道；
6. 更新渠道密钥并执行一次渠道测试；
7. 测试失败时恢复旧密钥并验证恢复结果；
8. 在界面中展示批次和账号级脱敏状态。

系统不重新实现登录、接码或认证凭据生成；这些能力由 SMS688 提供。

## 2. 非目标

- 不修改 SMS688 的自动化流程。
- 不在浏览器内直接调用 SMS688 或 NewAPI 管理 API。
- 不把该界面耦合到 NewAPI 现有前端。
- 不创建新的 Codex 登录、短信接码或 OAuth 实现。
- 不把多个 Codex 凭据塞入同一个 NewAPI 渠道。
- 首版不使用 WebSocket；界面通过短轮询读取进度。
- 不保存长期可恢复的原始账号密码、2FA 密钥或完整 CPA 文件。

## 3. 已验证的外部契约

### 3.1 SMS688

认证：

```http
Authorization: Bearer <ACCOUNT_API_KEY>
```

创建任务：

```http
POST https://cdk.sms688.cc/api/v1/tasks
Idempotency-Key: <stable-idempotency-key>
X-Submission-Token: <same-stable-idempotency-key>
Content-Type: application/json
```

```json
{
  "account_mode": "microsoft",
  "account_text": "ACCOUNT_LINES",
  "bind_free": false
}
```

查询与下载：

```http
GET https://cdk.sms688.cc/api/v1/tasks/{batch_id}
GET https://cdk.sms688.cc/api/v1/tasks/{batch_id}/download/cpa
```

支持的账号模式：`microsoft`、`totp`、`link`、`manual`。

已观察到的任务状态：

- 活动状态：`queued`、`running`、`waiting_phone`、`waiting_code`；
- 终态：`complete`、`error`、`expired`、`cancelled`。

已确认的批次字段包括：

```text
batch_id, all_finished, total, terminal, complete, error,
cancelled, skipped_free, expired, duration_seconds, jobs
```

已确认的账号任务字段包括：

```text
id, email, email_masked, status, stage, account_mode,
manual_code_required, execution_generation, manual_code_attempt,
phone_rejection_classification, phone_rejection_reason
```

文件行为：

- 一个成功账号返回 JSON；
- 多个成功账号返回 ZIP；
- `complete == 0` 时没有认证文件；
- 文件尚未开放时可能返回 HTTP 409；
- 导出文件仅保留约 30 分钟，完成后必须立即下载。

公开文档没有稳定声明全部响应与错误 Schema。客户端必须只强依赖必需字段、忽略未知字段，并同时判断 HTTP 状态和业务结果。

### 3.2 NewAPI

NewAPI 管理 API 支持管理员访问令牌，并要求管理员用户 ID：

```http
Authorization: <administrator-access-token>
New-Api-User: <administrator-user-id>
```

更新渠道：

```http
PUT {NEWAPI_BASE_URL}/api/channel/
```

最小 Codex 渠道更新体：

```json
{
  "id": 123,
  "type": 57,
  "key": "{\"access_token\":\"...\",\"account_id\":\"...\"}"
}
```

Codex 密钥必须是 JSON 对象，至少包含非空的 `access_token` 和 `account_id`。兼容字段为：

```text
id_token, access_token, refresh_token, account_id,
last_refresh, email, type, expired
```

测试渠道：

```http
GET {NEWAPI_BASE_URL}/api/channel/test/{channel_id}
```

可选查询参数为 `model`、`endpoint_type` 和 `stream`。Codex 渠道默认使用 Responses 端点。成功判定必须读取响应 JSON 的 `success == true`，不能仅依赖 HTTP 200。

当前安全策略已关闭渠道密钥读取接口。独立服务因此不能通过管理 API 取得旧密钥，回滚所需旧密钥必须在渠道池初次纳管时由管理员安全导入，或由本系统此前保存的加密快照提供。没有可信旧密钥时不得承诺自动回滚。

## 4. 架构

```text
Browser Management UI
          │
          ▼
Automation Orchestrator
  ├── Task API
  ├── Batch Worker
  ├── SMS688 Client
  ├── CPA Parser
  ├── Channel Allocator
  ├── NewAPI Client
  └── Persistence + Secret Encryption
          │                 │
          ▼                 ▼
      SMS688 API         NewAPI API
```

### 4.1 浏览器管理界面

职责：

- 多行账号输入；
- 账号模式、`bind_free` 和渠道池选择；
- 输入有效行、无效行、重复行统计；
- 批次及账号级状态展示；
- 渠道池和连接状态管理；
- 触发安全的后处理重试。

浏览器不得接收或持久化 SMS688 API Key、NewAPI 管理令牌、账号原始行、CPA 内容或渠道旧密钥。

### 4.2 自动化编排服务

职责：

- 校验账号输入并生成稳定幂等键；
- 调用 SMS688，处理超时、轮询、下载和恢复；
- 解析 CPA JSON 或 ZIP；
- 将账号映射到渠道池；
- 更新、测试并在可行时回滚 NewAPI 渠道；
- 持久化非敏感状态和加密的短期回滚快照；
- 服务重启后恢复未完成任务。

### 4.3 持久化层

保存：

- 本地批次 ID、SMS688 `batch_id` 和稳定幂等键；
- 账号脱敏标识、状态、阶段和错误分类；
- `account_id` 与渠道 ID 的稳定映射；
- 渠道租约、更新时间和恢复游标；
- 加密的短期旧密钥快照及其清理时间。

不保存：

- 原始账号行；
- 账号密码或 2FA 密钥；
- 明文 API Key、access token、refresh token；
- 完整 CPA JSON 或 ZIP；
- 第三方响应的敏感原文。

持久化实现必须兼容 SQLite、MySQL 和 PostgreSQL。

## 5. 组件边界

### 5.1 Task API

提供：

```text
POST /api/batches
GET  /api/batches
GET  /api/batches/{id}
POST /api/batches/{id}/resume
POST /api/batches/{id}/accounts/{account_id}/retry-processing

GET  /api/channel-pools
POST /api/channel-pools
PUT  /api/channel-pools/{id}

GET  /api/settings/status
PUT  /api/settings
POST /api/settings/test-sms688
POST /api/settings/test-newapi
```

API 不返回账号原始行或认证内容。批次创建成功后，原始输入不得再次出现在任何响应中。

### 5.2 SMS688 Client

封装任务创建、查询和 CPA 下载。创建请求的两个幂等头始终使用同一持久化值。请求结果未知时先恢复原任务，不盲目创建新任务。

### 5.3 CPA Parser

- 单 JSON：限制大小，解析一个凭据；
- ZIP：限制压缩包大小、文件数量、单文件大小和解压后总大小；
- 拒绝绝对路径、`..`、符号链接及非预期文件；
- 每个凭据必须具有非空字符串 `access_token` 和 `account_id`；
- 使用 `email`、`account_id` 和安全的文件元数据匹配任务结果；
- 无法唯一匹配时标记 `credential_invalid`，禁止猜测分配。

解析后的完整凭据只在后处理期间存在于内存。

### 5.4 Channel Allocator

- 管理一个或多个预配置渠道池；
- 每个成功账号独占一个 Codex 渠道；
- 同一 `account_id` 再次成功时更新原绑定渠道；
- 新 `account_id` 按池内确定顺序选择可用渠道；
- 使用数据库事务和带过期时间的租约避免并发占用；
- 渠道不足时进入 `waiting_channel`，不覆盖已有绑定；
- 渠道类型必须为 Codex（类型 57）。

### 5.5 NewAPI Client

- 更新渠道密钥；
- 触发渠道测试；
- 解析 `success`、`message`、`time` 和可选 `error_code`；
- 屏蔽第三方响应中的敏感内容；
- 只有存在可信旧密钥快照时才能执行补偿回滚。

### 5.6 Batch Worker

- 保证同一批次只有一个有效 worker；
- 轮询 SMS688 并投递后处理；
- 对每个账号独立执行，允许部分成功；
- 使用持久状态实现进程重启恢复；
- 所有步骤可重复执行，不创建重复任务或重复渠道映射。

## 6. 数据流

```text
1. 管理员提交多行账号
2. 服务端校验、归一化和去重
3. 建立本地批次并生成稳定幂等键
4. 使用同一幂等键调用 SMS688 创建任务
5. 保存 SMS688 batch_id
6. 轮询至每个账号进入终态
7. 批次具备下载条件后立即下载 CPA
8. 解析并匹配每个成功凭据
9. 查找 account_id 的既有渠道或预留新渠道
10. 验证渠道具有可信回滚基线
11. 更新 NewAPI 渠道密钥
12. 调用 NewAPI 渠道测试
13. 成功：提交渠道映射并清理回滚快照
14. 失败：恢复旧密钥并再次测试
15. 更新账号与批次最终状态
```

多账号部分成功时，系统立即处理每个成功账号；失败账号不会阻止其他账号更新渠道。

## 7. 状态机

### 7.1 批次状态

```text
created
  → submitting
  → submitted
  → polling
  → downloading
  → processing
  → completed
```

批次终态：

```text
completed
partial_completed
failed
cancelled
expired
```

只有当所有账号进入终态，且所有可执行的渠道后处理已经结束后，批次才能进入终态。

### 7.2 账号状态

主路径：

```text
pending
  → sms688_queued
  → sms688_running
  → sms688_waiting
  → credential_ready
  → channel_reserved
  → channel_updated
  → testing
  → succeeded
```

异常或等待状态：

```text
invalid_input
sms688_failed
sms688_expired
sms688_cancelled
download_failed
credential_invalid
waiting_channel
channel_update_failed
channel_test_failed
rollback_failed
```

`waiting_channel` 是可恢复的等待状态，不是终态。添加可用渠道或释放租约后可重新进入 `channel_reserved`。

## 8. 幂等、重试与恢复

### 8.1 SMS688 提交

- 幂等键在首次远程请求前持久化；
- 所有提交尝试均复用该键；
- 超时或连接中断表示结果未知，不表示任务创建失败；
- 结果未知时使用原键恢复，不能生成新键重提；
- 只有明确的确定性拒绝才进入提交失败。

### 8.2 轮询

- 活动状态使用配置的基础间隔；
- 网络错误、HTTP 429 和临时 5xx 使用带抖动的指数退避；
- 服务器提供 `Retry-After` 时优先遵循；
- 401、403 和有效载荷校验失败不自动重试；
- 达到批次最大执行时间后停止主动轮询，同时保留已完成结果。

### 8.3 下载

- 仅在远端批次允许下载时请求 CPA；
- HTTP 409 视为尚未开放，在文件保留窗口内有限重试；
- 下载响应必须先通过 MIME、大小与归档安全校验；
- 文件已过期时不自动重新购买或重新提交账号任务。

### 8.4 NewAPI 更新和测试

- 网络错误有限重试；
- 权限、参数和 Codex 密钥校验错误不自动重试；
- 更新成功后仅执行一次正常渠道测试；
- 测试失败时进入补偿流程，而不是保留未经验证的新密钥。

### 8.5 服务重启恢复

启动时扫描：

- `submitting`：使用原幂等键恢复提交结果；
- `submitted`、`polling`：继续查询；
- `downloading`、`processing`：重新执行幂等后处理；
- `channel_reserved`、`testing`：校验租约、映射和补偿记录后恢复；
- `waiting_channel`：在渠道池变化后重新尝试分配。

同一批次、`account_id` 和渠道均须有唯一标识或数据库约束，禁止用数组下标寻址持久状态。

## 9. 渠道更新与补偿

标准流程：

```text
确认可信旧密钥
  → 加密保存短期快照
  → PUT 新密钥
  → GET 渠道测试
      ├─ success=true：确认映射并删除快照
      └─ success=false：PUT 旧密钥
                         → 再次测试
                            ├─ 成功：释放租约，账号失败
                            └─ 失败：锁定渠道并标记 rollback_failed
```

由于 NewAPI 不提供读取渠道密钥的管理接口，渠道池纳管规则为：

1. 新建空占位渠道：回滚基线为“没有旧的有效账号”，测试失败时将渠道锁定为空闲待修复状态；
2. 已有渠道：管理员纳管时必须提供当前旧密钥，系统加密保存为可信基线；
3. 无可信基线的已有渠道不得由自动化流程覆盖。

该规则避免依赖已经硬下线的密钥读取接口，也避免虚假承诺自动回滚。

## 10. 错误分类

每个外部错误保存：来源、HTTP 状态、稳定错误类别、首次/最后发生时间和脱敏摘要。

禁止保存完整响应正文。错误类别至少包括：

```text
sms688_auth
sms688_validation
sms688_rate_limited
sms688_transient
sms688_terminal
sms688_file_not_ready
sms688_file_expired
newapi_auth
newapi_validation
newapi_update
newapi_test
credential_parse
channel_capacity
rollback
```

界面只显示可操作的错误摘要，例如“SMS688 凭据无效”“渠道池容量不足”“NewAPI 渠道测试失败”。

## 11. 敏感数据与安全边界

- SMS688 API Key、NewAPI 管理令牌和加密密钥仅存在于服务端配置或 Secret 存储；
- 浏览器配置表单只允许写入或替换秘密，不回显明文；
- 原始账号输入只在提交和远端创建任务所需的最短生命周期内存在；
- 提交完成后释放相关缓冲区引用，不进入任务历史；
- CPA 文件下载到内存或权限受限的临时文件，并在解析后立即删除；
- 旧渠道密钥采用带版本的认证加密格式保存，过期后清理；
- 日志中禁止出现 Authorization、Cookie、API Key、密码、2FA、access token、refresh token、ID token 和完整 JSON 凭据；
- 服务端日志只记录本地批次 ID、脱敏账号 ID、渠道 ID、状态和错误类别；
- 管理界面必须受管理员登录保护；生产部署必须使用 HTTPS。

## 12. 界面设计

### 12.1 账号提交区

- 多行文本框，每行一个账号；
- 账号模式，默认 `microsoft`；
- `bind_free`，默认关闭；
- 渠道池选择；
- 有效、无效和重复行计数；
- 提交成功后清空浏览器中的原始输入。

### 12.2 批次概览

显示：

- 本地批次 ID 和 SMS688 批次 ID；
- 总数、处理中、成功、失败和等待渠道数量；
- 当前阶段、执行时长和最后更新时间；
- 历史批次和恢复状态。

### 12.3 账号结果表

显示：

- 脱敏邮箱；
- SMS688 状态和阶段；
- NewAPI 渠道 ID；
- 渠道更新、测试和回滚结果；
- 脱敏错误摘要；
- “重试后处理”操作。

“重试后处理”只能重试解析后的下游操作，不得重新创建付费 SMS688 任务。

### 12.4 配置区

- SMS688 连接测试；
- NewAPI 连接与管理员权限测试；
- 渠道池配置、Codex 类型校验和容量统计；
- 默认测试模型、超时、轮询和保留参数；
- 秘密写入或轮换入口，不提供明文查看。

## 13. 输入和文件校验

- 拒绝空批次；
- 批次数量不得超过服务端配置上限；
- 按账号模式检查每行字段数量；
- 邮箱归一化后在批次内去重；
- 不把包含密码的原始行放入校验错误响应；
- 限制 HTTP 请求体、CPA 下载、JSON 和 ZIP 大小；
- ZIP 限制文件数量、单文件大小和解压总量；
- 拒绝 ZIP Slip、符号链接、嵌套归档和意外文件类型；
- CPA 字段必须具有预期类型，且 `access_token`、`account_id` 非空；
- 多个结果无法唯一映射账号时停止处理，不按顺序猜测身份。

具体数值上限在实现计划中作为可配置默认值确定，并由测试锁定；外部服务已声明的 30 分钟文件保留窗口不可更改。

## 14. 部署与配置

推荐独立容器部署，通过环境变量或 Docker Secret 注入：

```text
SMS688_ACCOUNT_API_KEY
NEWAPI_BASE_URL
NEWAPI_ACCESS_TOKEN
NEWAPI_USER_ID
AUTOMATION_DATABASE_DSN
AUTOMATION_ENCRYPTION_KEY
```

数据库 DSN 和秘密不得返回浏览器。反向代理只公开管理界面和编排服务 API；SMS688 与 NewAPI 调用均由服务端发起。

本规格不要求修改 NewAPI 的 `go.mod`、CI/CD、ELK、`docker-compose.yml` 或网络配置。

## 15. 测试策略

### 15.1 单元测试

- 账号解析、归一化、脱敏和去重；
- 状态转换和非法转换拒绝；
- 幂等键稳定性；
- JSON 与 ZIP 解析和资源限制；
- 凭据字段校验和账号匹配；
- 渠道分配、租约和重复 `account_id`；
- 错误分类、退避和日志脱敏；
- 认证加密快照的写入、读取、轮换和清理。

### 15.2 集成测试

使用本地模拟 HTTP 服务覆盖：

- SMS688 成功提交、结果未知恢复、轮询、409、429、5xx 和文件过期；
- 单 JSON、多账号 ZIP 和恶意 ZIP；
- NewAPI 更新成功、测试失败、回滚成功和回滚失败；
- 无可信旧密钥时拒绝覆盖；
- worker 重启后的任务恢复；
- 同一批次和渠道的并发竞争；
- SQLite、MySQL 和 PostgreSQL 的迁移与事务行为。

### 15.3 端到端测试

- 多账号部分成功；
- 渠道不足后补充渠道并恢复；
- 提交超时但远端已经创建批次；
- 一个 `account_id` 再次提交时复用原渠道；
- 更新后测试失败并自动恢复旧密钥；
- 界面状态与后端最终状态一致；
- 浏览器、日志和任务记录中均无敏感凭据。

### 15.4 验收标准

- 同一幂等键不会创建两个 SMS688 批次；
- 同一渠道不会并发分配给两个账号；
- 一个成功账号只映射一个 Codex 渠道；
- 成功账号自动更新渠道并通过一次 NewAPI 测试；
- 测试失败不会保留未经验证的新密钥；
- 没有可信回滚基线时不会覆盖已有渠道；
- 服务重启不丢任务、不重复提交、不重复占用渠道；
- 敏感字段不出现在日志、前端响应和普通任务记录中；
- 变更代码测试覆盖率不低于 80%；
- Go 测试必须运行 `go test -race`。

## 16. 实施分期

### 第一阶段：可测试的后端核心

实现持久模型、输入解析、SMS688 客户端、状态机、CPA 解析、渠道分配、NewAPI 客户端和 worker，并通过模拟服务完成集成测试。

### 第二阶段：管理界面

实现批次提交、历史列表、账号结果、渠道池和配置状态页面，以短轮询展示后台状态。

### 第三阶段：生产验收

使用脱敏测试账号和专用占位渠道执行真实 SMS688 → CPA → NewAPI 更新 → 渠道测试链路，验证日志、恢复、回滚和文件清理。

## 17. 已知外部不确定性

SMS688 公开文档未完整承诺创建、查询、错误和 CPA 归档的全部 Schema。实施时应：

- 先用脱敏账号采集一组真实响应样本；
- 将样本固化为契约测试夹具，但不提交真实凭据；
- 对未知字段保持前向兼容；
- 遇到无法证明身份映射的归档结构时安全停止；
- 不把公开网页 bundle 中的内部字段当作长期稳定契约。
