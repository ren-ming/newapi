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

## 合并进 new-api 主程序（2026-08-19，commit 见 git log）

架构：独立 systemd 服务 → new-api 内置功能。后端 `/api/account-automation` 挂 admin 组（AdminAuth+gin.WrapH），`controller.AccountAutomationChannelService` 内部直调 model 层取代 HTTP 自调用；前端 React 页面挂后台菜单。经验：

1. **trusted 模式而非免认证**：`NewTrustedServer` 复用同一 server 逻辑但 `adminToken == ""` 时跳过 Bearer 检查——外层 AdminAuth 已认证；独立部署 `NewServer` 仍强制 token，两种模式共用全部测试。
2. **路径前缀要去掉**：gin 组 `Any("/*any", gin.WrapH(http.StripPrefix("/api/account-automation", handler)))`，server 内部路由从 `/batches` 起匹配；独立部署用 mux 反向加回 `/api` 前缀保持 URL 兼容。
3. **错误类名沿用 HTTP 版前缀**（`newapi_channel_not_found` 等）：orchestrator 的 errorClass 按首冒号截断，内部直调实现必须产出同名类，日志统计才连续。
4. **内部直调比 HTTP 少一层坑**：testChannel 直接收 `*model.Channel`（自建 `gin.CreateTestContext(httptest.NewRecorder())`），成功判定 `result.localErr == nil`；不用管 session/New-Api-User 头。
5. **渠道更新落库三步缺一不可**：`channel.Update()` → `model.InitChannelCache()` → `service.ResetProxyClientCache()`，否则 relay 层仍用旧密钥。
6. **测试 seam 用包级函数变量**：`var accountAutomationRunChannelTest = testChannel`，测试注入 stub；sqlite in-memory（glebarez）AutoMigrate Channel/Ability + 临时替换 model.DB + t.Cleanup 恢复。
7. **TanStack Router 路由文件必须先 build**：`routeTree.gen.ts` 由 rsbuild 插件生成，`bunx tsr generate` 在 node 25 上跑不动；新增 `routes/_authenticated/account-automation/index.tsx` 后先 `bun run build` 再 typecheck。
8. **权限守卫抄现有路由**：`beforeLoad` 里 `auth.user.role < ROLE.ADMIN → redirect /403`，与 channels 页一致（不是跳登录页）。
9. **挂载条件化**：`SMS688_ACCOUNT_API_KEY` 未设时 `AccountAutomationHandler()` 返回 nil、路由不挂载（API 404），启动日志 `account automation disabled`；必须在 `SetApiRouter` 之前调 `InitAccountAutomation()`（main.go）。
10. **部署切换顺序**：先给 new-api 容器加 env（override）并 up → 验证 401/菜单 → 再停 systemd + 删 env 文件；反序会出现功能空窗。

## v2 单账号记录制（2026-08-19，本分支 5 提交）

批次概念整体删除，每次提交 = 一个账号 = 一条 `account_automation_jobs` 记录（永久落库）。经验：

1. **大切换一步到位**：Task 1 只新增 Job 类型/校验/MemoryJobStore 保编译（旧 Batch 并存），Task 2/3 一次性切 orchestrator+server 并删光旧代码——两套管线长期并存只会让 store 接口打架。
2. **Resume 必须从 store 重读快照**：调用方传入的 Job 可能陈旧（测试里 CreatedAt 零值直接误判超时）；`Resume` 先 `store.GetJob(id)` 拿权威状态再分流（终态 return / 无 batchID → submit_failed(interrupted) / 超时 → sms688_failed(interrupted) / 否则续轮询且 `Idempotency-Key` 置空防重复提交）。
3. **凭据匹配只用 masked email**：账号原文不落库（只存 `masked_email`），CPA 下载后按归一化 email 匹配再比对 mask；歧义/缺失 → `credential_invalid`，绝不按顺序猜。
4. **sqlite :memory: 每连接独立库**：glebarez 驱动下 GORM 连接池多连接互相看不到表；测试必须 `sqlDB.SetMaxOpenConns(1)`（model 与 controller 两处同坑）。
5. **前端复用 channels API**：`getChannels({ type: 57, page_size: 100 })` 直接过滤 Codex 渠道，显示 `名称 (#ID)`；zh.json 尾部追加式 key 块与 v1 先例一致，roundtrip 重排会产生整文件 diff，用 Edit 精准替换。
6. **会话隔离在 worktree 时主 checkout 不可操作**：hook 拒绝 `cd`/`git -C` 指向主工作区；合并 main 需用户执行或退出 worktree 会话。构建镜像可直接从 worktree 跑（buildx context 一次性传输），前提是核对 worktree HEAD 包含 origin/main 全部提交。

## SMS688 真实查询契约修复（2026-08-20）

1. **创建与查询响应不是同一结构**：POST 创建响应是扁平批次；GET `/api/v1/tasks/{batch_id}` 返回 `{"batches":[...]}`。测试必须使用脱敏后的真实响应形态，不能让 mock 沿用错误假设。
2. **查询结果必须按 Batch ID 精确选择**：响应可能包含多个批次，禁止默认使用 `batches[0]`；目标批次缺失时返回稳定的 `sms688_invalid_response`。
3. **真实批次汇总可能没有 `jobs`**：单账号模式仅在 `all_finished=true && total=1 && complete=1` 时推导账号成功；其他无账号详情场景继续失败，不能猜测归属或状态。
4. **凭据归属仍由 CPA email 验证**：批次完成推导只推进到 CPA 下载，最终凭据仍按 masked email 唯一匹配；缺失、冲突或不匹配均为 `credential_invalid`。
5. **恢复必须复用已保存 Batch ID**：解析或轮询失败的本地 Job 不得重新 POST；恢复后只查询原批次、下载 CPA、更新并测试渠道。

