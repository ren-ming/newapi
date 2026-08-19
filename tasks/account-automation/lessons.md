# SMS688 → NewAPI 自动化 — 实现经验（2026-08-19）

## 首版范围（MVP，commit 39be317）

- 内存状态：服务重启丢批次，无恢复。
- 无旧密钥回滚：NewAPI 渠道密钥读取接口已硬下线，测试失败只标记 `channel_test_failed` 并保留渠道 ID 供人工检查。
- 无数据库渠道池：每行输入必须显式 `channel_id|account_line`。
- 仅 `microsoft` 账号模式。
- 真实测试必须使用专用、可丢弃的 Codex 占位渠道。

## 启动

```bash
SMS688_ACCOUNT_API_KEY=... NEWAPI_BASE_URL=... NEWAPI_ACCESS_TOKEN=... \
NEWAPI_USER_ID=... AUTOMATION_ADMIN_TOKEN=... go run ./cmd/account-automation
# 可选: SMS688_BASE_URL(默认 https://cdk.sms688.cc) AUTOMATION_LISTEN_ADDR(默认 :8080)
```

页面在 `/`，管理 API 需要 `Authorization: Bearer <AUTOMATION_ADMIN_TOKEN>`，`/healthz` 公开。

## 实现中验证出的经验

1. **SMS688 下载是批次级不是账号级**：`GET /api/v1/tasks/{batch_id}/download/cpa` 一次返回全部；单账号 JSON、多账号 ZIP。orchestrator 据此下载一次并用 `ParseCPA` 解析，凭据按归一化 email 唯一匹配；email 重复或缺失 → `credential_invalid`，绝不按 ZIP 文件顺序猜测归属。
2. **幂等键 = 本地批次 ID**：`Idempotency-Key` 与 `X-Submission-Token` 同值，重试 run 复用同一键，防止重复创建付费任务。
3. **NewAPI 更新必须先预检**：`GET /api/channel/{id}` 校验外层 `success`、`data.id` 匹配、`data.type == 57`（Codex），预检失败时 PUT 次数为 0，防止覆盖非 Codex 渠道。
4. **HTTP 200 ≠ 成功**：NewAPI 更新和测试都要解析业务 `success` 字段。
5. **并行代理各自 worktree 缺共享类型**会产出重复类型定义（sms688.go/cpa.go 各复制了 types.go 的类型），整合时必须删除；代理卡在 git 提交锁时，直接从其 worktree 读文件整合比等待更可靠。
6. **语义分离**：`BatchDeadline`（默认 45min，轮询总时限）必须与 `HTTPTimeout`（30s，单请求）分开；代理最初把 30s 的 HTTP 超时同时当批次轮询上限，会切断正常批次。
7. `context.WithoutCancel` 让后台 worker 不随 HTTP 请求结束而取消。

## 已知待验证/限制

- SMS688 真实响应 Schema 未用生产样本验证（公开文档不完整）；客户端只依赖必需字段、忽略未知字段。
- 凭据匹配强依赖 CPA JSON 的 `email` 字段；无 email 的凭据会判 `credential_invalid`。
- 日志白名单：`batch_id`/`status`/`masked_email`/`channel_id`/`error_class`，测试锁定无敏感泄漏。
- 覆盖率：internal/accountautomation 85.5%，cmd 装配层 61.7%（run/main 信号装配未单测），`go test -race` 全绿。
- 全仓存在与本功能无关的既有失败（web/classic/dist 缺失、claude/helper 包），目标包不受影响。

## 端到端冒烟（2026-08-19，已通过）

本地 mock 上游（SMS688 :19001 + NewAPI :19002，单文件标准库脚本）+ 真实进程 + 浏览器表单：

- `/healthz` 200；`/` 页面 200；无认证 POST 401；带 Bearer POST 202。
- 全链路：submitting → submitted → polling（2 次）→ downloading → processing → NewAPI type57 预检 → PUT 渠道 → 测试 → succeeded → 批次 completed。
- 幂等键 = 本地批次 ID；`Idempotency-Key` 与 `X-Submission-Token` 同值。
- 浏览器表单提交、2s 短轮询、提交后清空账号输入、多批次并排渲染（含失败批次的 `error_class` 列）均正常，无 console 错误。
- 服务端日志泄漏扫描（密码/token/邮箱明文/凭据）0 命中。
- 冒烟踩坑：发给 SMS688 的 `account_text` 已去掉 `channel_id|` 前缀（每行 `email----password`），写 mock 时不要按带前缀格式解析。

## Code review 修复（2026-08-19）

审查 9 项发现，修复 4 项 CONFIRMED（全部 `sms688.go`）：

1. **禁跟随重定向**：构造器值拷贝注入的 client 并设 `CheckRedirect → http.ErrUseLastResponse`。原因：`Idempotency-Key`/`X-Submission-Token` 不在 net/http 跨主机敏感头剥离名单，307/308 还会重发明文账号请求体；拷贝不影响调用方共享 client。
2. **错误前缀改为稳定类名**（`sms688_transport_error:` 等）：`"sms688: "` 会被 orchestrator 的 errorClass 首冒号截断成未定义类 `sms688`，日志/告警按类统计完全失真。
3. **`%w` 包装保留错误链**：传输层 `*url.Error` 的超时/取消语义（`errors.Is`）恢复可用。
4. **2xx 契约校验**：CreateTask/GetTask 校验 `batch_id` 非空，否则 `sms688_invalid_response`——防 `{}` 响应静默零值导致轮询空转到 45 分钟超时。

接受不修（已评审）：CreateTask 无重试、DownloadCPA 不校验 Content-Type、先读满再判状态码、readLimited 与 newapi.go 守卫重复、nil httpClient 回退 DefaultClient（当前接线不可达）。
