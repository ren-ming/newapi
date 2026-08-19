# 账号自动化 v2：单账号记录制 + 落库 + microsoft/totp 双模式

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans（TDD 每任务先红后绿）。

**Goal:** 取消批次概念，每次提交=一个账号一条记录（类型选择+单账号输入+渠道下拉），记录永久落库可查历史，支持 microsoft/totp 两种模式。

**Architecture:** accountautomation 内核从 Batch 模型重构为 Job（单账号）模型；Store 接口双实现（MemoryStore 测试用 / model 包 GORM 生产用）；controller 装配 DB store 并在启动时恢复未完成 Job；前端表单/列表按 Job 重写。

**Tech Stack:** Go(GORM 三库兼容) + React(TanStack) 。生产仅内嵌模式（cmd 独立模式降级为 MemoryStore API-only，删旧页面）。

**Spec:** 用户对话确认（2026-08-19）：单账号一条记录、渠道下拉点选、历史永久存库、先上 microsoft+totp、manual/link 不做、批量不做。

## Global Constraints

- 不记录账号原文/密码/凭据，日志字段白名单不变（masked_email 等）
- JSON 编解码只用 common.Marshal/Unmarshal
- 三库兼容（GORM、无 DB 特有语法）、迁移进 model/main.go
- 不改 go.mod / docker-compose 主文件 / CI
- 唯一字符串 ID 寻址

---

### Task 1: accountautomation Job 模型 + 校验 + MemoryStore

**Files:**
- Modify: `internal/accountautomation/types.go`（Job 类型/状态常量，删 Batch 系列）
- Modify: `internal/accountautomation/accounts.go`（ParseSingleAccount 校验；删 ParseAccountLines）
- Modify: `internal/accountautomation/store.go`（Job 语义 Store + MemoryStore）
- Test: `internal/accountautomation/types_test.go` / `accounts_test.go` / `store_test.go`（改造既有）

**Interfaces（Produces）:**
```go
type JobStatus string // submitting, sms688_queued/running/waiting, credential_ready,
                      // channel_updated, testing, succeeded, submit_failed,
                      // sms688_failed/expired/cancelled, download_failed,
                      // credential_invalid, channel_update_failed, channel_test_failed
type Job struct {
    ID string; AccountMode string; MaskedEmail string; ChannelID int
    BindFree bool; Status JobStatus; Stage string; ErrorClass string
    SMS688BatchID string `json:"sms688_batch_id,omitempty"`
    CreatedAt, UpdatedAt time.Time
}
type CreateJobRequest struct { AccountMode string; AccountText string; ChannelID int; BindFree bool }
type Store interface {
    CreateJob(Job) error
    GetJob(string) (Job, error)
    ListJobs(offset, limit int) ([]Job, int64, error) // updated_at 倒序+总数
    UpdateJob(id string, change func(*Job)) error
    ActiveJobs() ([]Job, error) // 非终态（succeeded 与 *_failed/expired/cancelled 之外）
}
func ParseSingleAccount(mode, text string) (email, masked, line string, err error)
// microsoft: ---- 分隔 2/4 段且首段含@；totp: 3 段且首段含@；否则 account_invalid
```

步骤：写失败测试（校验矩阵 + store CRUD/分页/ActiveJobs）→ 实现 → 绿。

### Task 2: orchestrator 单账号化 + Resume

**Files:**
- Modify: `internal/accountautomation/orchestrator.go`（SubmitJob/runJob/Resume；删 Batch 流程）
- Test: `internal/accountautomation/orchestrator_test.go`（改造既有用例到 Job 断言）

**Interfaces:**
```go
type JobService interface { // server 依赖
    SubmitJob(ctx, CreateJobRequest) (Job, error)
    GetJob(string) (Job, bool)
    ListJobs(offset, limit int) ([]Job, int64, error)
}
func (o *Orchestrator) SubmitJob(ctx, CreateJobRequest) (Job, error) // 校验→CreateJob(submitting)→go runJob
func (o *Orchestrator) Resume(ctx, Job)              // 有 SMS688BatchID→续 poll；无→submit_failed(interrupted)；创建超 BatchDeadline→sms688_failed(interrupted)
```
runJob 流程：CreateTask(单行, mode, idempotency=job.ID) → 存 batch id → poll（RemoteJob 状态映射 Job.Status，Stage 透传）→ AllFinished 且 job 成功 → DownloadCPA → ParseCPA → email 归一化匹配唯一凭据 → UpdateChannel(ChannelID) → TestChannel → succeeded；每失败分支落对应 JobStatus+error_class，日志白名单字段（job_id/masked_email/channel_id/status/error_class）。

### Task 3: server API v2

**Files:**
- Modify: `internal/accountautomation/server.go`（/jobs 路由；删 /batches 与 embed 旧页面）
- Delete: `internal/accountautomation/web/index.html`
- Test: `internal/accountautomation/server_test.go`（改造）

API：`POST /jobs`（校验错误 400+error，成功 202+Job）；`GET /jobs?offset=&limit=` → `{jobs:[],total}`；`GET /jobs/{id}`；`/healthz` 保留。trusted/Bearer 双模式行为不变。

### Task 4: model GORM 表 + Store 实现 + 迁移注册

**Files:**
- Create: `model/account_automation_job.go` + `model/account_automation_job_test.go`
- Modify: `model/main.go`（AutoMigrate 注册）

```go
type AccountAutomationJob struct { // 表 account_automation_jobs
    Id string `gorm:"primaryKey;size:64"`; AccountMode string `gorm:"size:16;index"`
    MaskedEmail string `gorm:"size:128"`; ChannelId int; BindFree bool
    Status string `gorm:"size:32;index"`; Stage string `gorm:"size:64"`; ErrorClass string `gorm:"size:64"`
    SmsBatchId string `gorm:"size:64"`; CreatedAt time.Time; UpdatedAt time.Time
}
// 实现 accountautomation.Store（model import internal/accountautomation 方向合法）
```
sqlite 内存测试：CRUD/倒序分页/ActiveJobs/终态判定。controller/account_automation_test.go 的 DB helper 复用（AutoMigrate 加新表）。

### Task 5: controller 装配 + 启动恢复

**Files:**
- Modify: `controller/account_automation.go`（DB store 注入、Resume 未完成 Job）、`controller/account_automation_test.go`
- Modify: `cmd/account-automation/main.go`（JobService 形态适配、MemoryStore、无页面仅 API）

InitAccountAutomation：`store := model.NewAccountAutomationJobStore()` → orchestrator → `for _, job := range store.ActiveJobs() { go orchestrator.Resume(ctx, job) }`。

### Task 6: 前端改造

**Files:**
- Modify: `web/default/src/features/account-automation/index.tsx`（表单：类型 Select[microsoft=微软邮箱/totp=2FA 动态口令] + 单行 Input[按类型 placeholder] + 渠道 Select[import { getChannels } from '@/features/channels/api' 过滤 type===57 显示 `名称 (#ID)`] + bindFree + 提交；列表：jobs 表格[时间/类型/脱敏邮箱/渠道/状态/错误类]+分页+2s 轮询首页）
- Modify: `web/default/src/i18n/locales/zh.json`
- Build: `bun run build`（生成 routeTree）→ `bun run typecheck`

### Task 7: 收尾

全量 `go test -race ./internal/accountautomation ./controller ./model ./cmd/account-automation` + gofmt/vet → 提交 → 合并 main → push → buildx（amd64builder，缓存热）→ scp/load/up → 线上验证（logs enabled、无登录 401、旧 /batches 404）→ lessons + 记忆更新。
