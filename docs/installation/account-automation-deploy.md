# 账号自动化功能部署与测试手册（SMS688 → NewAPI）

**2026-08-19 起该功能已合并进 new-api 主程序**：NewAPI 后台（管理员）多一个「账号自动化」菜单，登录即用，无独立服务、无第二套令牌、无 SSH 端口转发。粘贴 `channel_id|账号行` 批量提交 SMS688，轮询完成后自动把 CPA 凭据写入 NewAPI Codex 渠道并测试（内部直调 model 层，不再走 HTTP 自调用）。

- 源码：`internal/accountautomation/`（核心）+ `controller/account_automation.go`（内部直调+装配）+ `web/default/src/features/account-automation/`（前端）
- 独立部署模式（`cmd/account-automation`）仍保留，用于本地开发调试；生产用内嵌模式
- 经验记录：`tasks/account-automation/lessons.md`

## 一、启用（唯一必填项）

给 new-api 容器加环境变量 `SMS688_ACCOUNT_API_KEY=<接码平台 Account API Key>`（docker-compose.override.yml，不改冻结主文件），然后 `docker compose up -d`。

- 未设置该变量时功能整体不挂载（后台菜单可点但 API 404）
- 可选：`SMS688_BASE_URL`（默认 `https://cdk.sms688.cc`）
- 不再需要 `NEWAPI_ACCESS_TOKEN` / `NEWAPI_USER_ID` / `AUTOMATION_ADMIN_TOKEN` / `AUTOMATION_LISTEN_ADDR`

启动日志出现 `account automation enabled` 即生效。

## 二、升级

功能随 new-api 主程序一起发布：本地改码 → buildx 构建镜像 → 上传 → `up -d`（见既有部署流程）。**注意：new-api 容器重启会丢失进行中的批次记录**（SMS688 远端任务与已写入密钥不受影响），升级前等批次跑完。

## 三、测试清单（首次真实测试）

1. **启动日志**：`docker logs new-api 2>&1 | grep automation` → `account automation enabled`
2. **API 挂载**（服务器）：`curl -o /dev/null -w "%{http_code}" http://127.0.0.1:3005/api/account-automation/batches` → 401（无登录；404=未启用）
3. **页面**：NewAPI 后台 → 管理员登录 → 左侧「账号自动化」菜单
4. **建可丢弃测试渠道**：后台新建空 Codex 渠道（type 57），记渠道 ID。**不要用生产渠道**（测试失败不回滚旧密钥）
5. **提交**：每行 `<渠道ID>|<邮箱>----<密码>`，提交后页面 2s 自动轮询
6. **状态判定**：正常 `created → submitting → polling → downloading → processing → succeeded`；批次终态 `completed`
7. **核验**：后台打开测试渠道，密钥应已变成 CPA auth JSON，测试按钮通过
8. **失败排查**（error_class）：见下表；`docker logs new-api` 看日志（字段仅 batch_id/status/masked_email/channel_id/error_class，无敏感值）

| error_class | 含义 | 动作 |
|---|---|---|
| `sms688_transport_error` / `sms688_http_error` / `sms688_decode_error` | 网络/状态码/响应格式 | 看日志定位；首次真实测试重点观察 |
| `sms688_invalid_response` | 2xx 缺 batch_id | SMS688 真实 Schema 偏差，把日志发维护者 |
| `sms688_expired` / `sms688_failed` / `sms688_cancelled` | 远端任务失败 | 接码平台侧 |
| `download_failed` | CPA 下载失败 | 网络/权限 |
| `credential_invalid` | CPA 缺字段或 email 对不上 | CPA 格式问题 |
| `newapi_channel_not_found` / `newapi_channel_precondition_failed` | 渠道不存在/非 Codex | 核对渠道 ID 与类型 |
| `newapi_channel_update_failed` | 密钥写入 DB 失败 | 看日志 GORM 错误 |
| `channel_test_failed` | 密钥已写入但测试不过 | **密钥已生效**，按渠道 ID 人工检查 |

## 四、独立部署模式（仅开发调试）

```bash
SMS688_ACCOUNT_API_KEY=... NEWAPI_BASE_URL=http://127.0.0.1:3000 \
NEWAPI_ACCESS_TOKEN=<管理员令牌> NEWAPI_USER_ID=1 AUTOMATION_ADMIN_TOKEN=<自设> \
go run ./cmd/account-automation   # 默认监听 :8080，页面在 /
```

独立模式走 HTTP 客户端更新渠道（需要管理员访问令牌），与内嵌模式的内部直调实现共用同一套 orchestrator。历史 systemd 部署（/opt/account-automation + /etc/account-automation.env）已于合并后下线，如残留可删除。

## 五、已知限制

- **无回滚**：渠道测试失败时新密钥已写入（旧密钥读取接口已硬下线），所以必须用可丢弃渠道试。
- **容器重启丢批次**：内存态，new-api 重启后批次记录消失（SMS688 任务与已写入密钥不受影响）。
- **SMS688 Schema 未经生产样本验证**：首次真实提交即验证；报 decode/invalid_response 类错误时收集日志反馈。
- 每行输入必须显式 `channel_id|` 前缀；仅支持 microsoft 账号模式。
