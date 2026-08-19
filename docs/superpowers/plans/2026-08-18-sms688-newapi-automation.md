# SMS688 → NewAPI 自动化首版实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 构建可本地运行的独立管理服务，自动提交 SMS688、多账号跟踪、解析 CPA，并按显式渠道映射更新和测试 NewAPI。

**Architecture:** 在仓库新增独立 `cmd/account-automation` 可执行程序和 `internal/accountautomation` 包，不接入 NewAPI 现有路由或前端。首版采用内存任务状态、服务端持有外部凭据、浏览器短轮询；每行账号显式附带渠道 ID，避免尚未实现数据库渠道池时发生错误分配。

**Tech Stack:** Go 1.25、标准库 HTTP、现有 `common` JSON 包、嵌入式 HTML/JS、Go table-driven tests。

**Spec:** `docs/superpowers/specs/2026-08-18-sms688-newapi-automation-design.md`

## Global Constraints

- 不修改 `go.mod`、CI/CD、ELK、`docker-compose.yml` 或网络配置。
- 所有 JSON 编解码调用使用 `common.Marshal`、`common.Unmarshal` 或 `common.DecodeJson`。
- 不记录账号原文、密码、API Key、access token、refresh token 或 CPA 正文。
- 每个持久或运行状态对象使用唯一字符串 ID，不使用数组下标寻址。
- 首版只覆盖 `microsoft` 模式和显式 `channel_id|account_line` 输入。
- 服务器端必须验证 NewAPI Codex 渠道更新所需 `access_token` 与 `account_id`。
- 测试必须执行 `go test -race`，变更包覆盖率不低于 80%。

---

## 文件结构

- `cmd/account-automation/main.go`：读取环境变量、构造依赖、启动服务。
- `internal/accountautomation/types.go`：批次、账号、SMS688 与 NewAPI DTO 和状态常量。
- `internal/accountautomation/input.go`：解析 `channel_id|account_line`，去重并脱敏。
- `internal/accountautomation/sms688.go`：SMS688 创建、查询和 CPA 下载客户端。
- `internal/accountautomation/cpa.go`：单 JSON/ZIP 安全解析和凭据匹配。
- `internal/accountautomation/newapi.go`：NewAPI 更新和渠道测试客户端。
- `internal/accountautomation/store.go`：并发安全的内存批次仓库和不可变快照。
- `internal/accountautomation/orchestrator.go`：后台状态机、轮询、账号后处理和结构化日志。
- `internal/accountautomation/server.go`：管理 API、静态页面和管理员 Bearer 认证。
- `internal/accountautomation/web/index.html`：多账号提交与批次状态界面。
- `internal/accountautomation/*_test.go`：输入、客户端、解析器、状态机与 HTTP 集成测试。
- `tasks/account-automation/lessons.md`：实现中验证出的项目特定经验。

### Task 1: 输入模型、状态与日志安全

**Files:**
- Create: `internal/accountautomation/types.go`
- Create: `internal/accountautomation/input.go`
- Test: `internal/accountautomation/input_test.go`

**Interfaces:**
- Produces: `ParseAccountLines(text string) ([]AccountSubmission, error)`。
- Produces: `MaskEmail(email string) string`。
- Produces: `Batch`, `BatchAccount`, `Credential` 与状态常量。

- [ ] 编写表驱动失败测试，覆盖合法输入、空行、无渠道 ID、非法 ID、重复渠道和重复邮箱。
- [ ] 运行 `go test ./internal/accountautomation -run 'TestParseAccountLines|TestMaskEmail'`，确认因符号缺失而失败。
- [ ] 实现最小类型和解析器；错误只能含行号与错误类别，不得回显原始行。
- [ ] 再次运行目标测试并确认通过。

### Task 2: SMS688 客户端

**Files:**
- Create: `internal/accountautomation/sms688.go`
- Test: `internal/accountautomation/sms688_test.go`

**Interfaces:**
- Produces: `SMS688Client.CreateTask(ctx, request, idempotencyKey) (RemoteBatch, error)`。
- Produces: `SMS688Client.GetTask(ctx, batchID) (RemoteBatch, error)`。
- Produces: `SMS688Client.DownloadCPA(ctx, batchID) (DownloadedCPA, error)`。

- [ ] 使用 `httptest.Server` 编写请求方法、路径、Bearer、两个幂等头、响应大小和错误分类测试。
- [ ] 运行目标测试确认失败。
- [ ] 实现带上下文超时、响应上限和脱敏错误的最小客户端。
- [ ] 运行目标测试确认通过。

### Task 3: CPA JSON/ZIP 解析

**Files:**
- Create: `internal/accountautomation/cpa.go`
- Test: `internal/accountautomation/cpa_test.go`

**Interfaces:**
- Produces: `ParseCPA(download DownloadedCPA) ([]Credential, error)`。
- Consumes: `Credential`。

- [ ] 编写单 JSON、多文件 ZIP、缺必需字段、ZIP Slip、文件数和解压大小限制测试。
- [ ] 运行目标测试确认失败。
- [ ] 使用现有 `common` JSON 包实现安全解析，凭据保留原始 JSON 仅供即时 NewAPI 更新。
- [ ] 运行目标测试确认通过。

### Task 4: NewAPI 客户端

**Files:**
- Create: `internal/accountautomation/newapi.go`
- Test: `internal/accountautomation/newapi_test.go`

**Interfaces:**
- Produces: `NewAPIClient.UpdateChannel(ctx, channelID, credential) error`。
- Produces: `NewAPIClient.TestChannel(ctx, channelID) (ChannelTestResult, error)`。

- [ ] 编写更新方法、请求体、认证头、测试成功和业务失败测试。
- [ ] 运行目标测试确认失败。
- [ ] 实现客户端；HTTP 200 但 `success=false` 必须返回失败。
- [ ] 运行目标测试确认通过。

### Task 5: 并发安全仓库与编排器

**Files:**
- Create: `internal/accountautomation/store.go`
- Create: `internal/accountautomation/orchestrator.go`
- Test: `internal/accountautomation/orchestrator_test.go`

**Interfaces:**
- Produces: `NewOrchestrator(store, sms688, newAPI, logger, config) *Orchestrator`。
- Produces: `Orchestrator.Submit(ctx, CreateBatchRequest) (Batch, error)`。
- Produces: `Store.GetBatch(id string) (Batch, bool)` 与 `Store.ListBatches() []Batch`。

- [ ] 编写成功、部分失败、下载失败、凭据匹配、更新失败和测试失败的端到端单元测试。
- [ ] 验证所有日志事件只含批次 ID、脱敏邮箱、渠道 ID、状态和错误类别。
- [ ] 运行目标测试确认失败。
- [ ] 实现不可变快照仓库和后台状态机；首版不做旧密钥回滚，测试失败明确标记并记录渠道 ID。
- [ ] 运行目标测试和 `go test -race ./internal/accountautomation`。

### Task 6: HTTP API 与独立页面

**Files:**
- Create: `internal/accountautomation/server.go`
- Create: `internal/accountautomation/web/index.html`
- Create: `internal/accountautomation/server_test.go`
- Create: `cmd/account-automation/main.go`

**Interfaces:**
- Produces: `NewServer(orchestrator, adminToken, logger) http.Handler`。
- Endpoints: `POST /api/batches`、`GET /api/batches`、`GET /api/batches/{id}`、`GET /healthz`、`GET /`。

- [ ] 编写管理员 Bearer 认证、请求大小、输入失败、成功提交和状态读取测试。
- [ ] 运行目标测试确认失败。
- [ ] 实现 HTTP API 与嵌入式页面；页面使用内存状态，不写入 `localStorage`。
- [ ] 实现 `main.go` 环境变量校验和优雅关闭。
- [ ] 运行服务端测试确认通过。

### Task 7: 验证、文档与经验

**Files:**
- Create: `tasks/account-automation/lessons.md`
- Modify: `docs/superpowers/specs/2026-08-18-sms688-newapi-automation-design.md`（仅在实现验证出契约差异时）

- [ ] 运行 `gofmt` 和 `go vet ./internal/accountautomation ./cmd/account-automation`。
- [ ] 运行 `go test -race -coverprofile=/tmp/account-automation.cover ./internal/accountautomation`。
- [ ] 用 `go tool cover -func=/tmp/account-automation.cover` 验证总覆盖率不低于 80%。
- [ ] 运行 `go test -race ./cmd/account-automation ./internal/accountautomation`。
- [ ] 使用测试服务启动程序，手工检查页面提交和状态轮询。
- [ ] 在经验文件记录真实 SMS688 Schema 仍需用户测试验证、首版无旧密钥回滚、日志事件名称和启动命令。
