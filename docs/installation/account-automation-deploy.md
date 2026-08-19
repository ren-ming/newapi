# 账号自动化服务部署与测试手册（SMS688 → NewAPI）

独立单二进制服务：粘贴 `channel_id|账号行` 批量提交 SMS688，轮询完成后自动把 CPA 凭据写入 NewAPI Codex 渠道并测试。与 NewAPI 主程序完全解耦（独立进程、独立端口、独立认证）。

- 源码：`internal/accountautomation/` + `cmd/account-automation/`
- 经验记录：`tasks/account-automation/lessons.md`

## 一、构建（本地 mac，服务器 4GB 编译会卡死）

```bash
git pull   # 确保在 58255c3 或之后
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o account-automation ./cmd/account-automation
```

静态链接单文件（约 13MB），页面已编译进二进制，无其他依赖。

## 二、配置（6 个环境变量）

| 变量 | 必填 | 来源 |
|---|---|---|
| `NEWAPI_ACCESS_TOKEN` | ✅ | NewAPI 后台 → 个人设置 → 访问令牌 → 生成（**必须管理员账号**） |
| `NEWAPI_USER_ID` | ✅ | 同页面显示的用户 ID（必须与令牌同账号，否则 401） |
| `SMS688_ACCOUNT_API_KEY` | ✅ | 接码平台 Account API |
| `AUTOMATION_ADMIN_TOKEN` | ✅ | 自设强随机串，页面登录用 |
| `NEWAPI_BASE_URL` | ✅ | 宿主跑 → `http://127.0.0.1:<newapi端口>` |
| `AUTOMATION_LISTEN_ADDR` | 建议 | `127.0.0.1:8080`，**必须绑回环**（页面收账号明文） |

`SMS688_BASE_URL` 默认 `https://cdk.sms688.cc`，一般不用设。

## 三、服务器安装

```bash
# 1. 上传
scp account-automation <user>@<server>:/opt/account-automation/account-automation
ssh <user>@<server> chmod +x /opt/account-automation/account-automation

# 2. 环境文件（权限 600，密钥不进 shell 历史）
sudo tee /etc/account-automation.env >/dev/null <<'EOF'
SMS688_ACCOUNT_API_KEY=<接码平台key>
NEWAPI_BASE_URL=http://127.0.0.1:3000
NEWAPI_ACCESS_TOKEN=<管理员访问令牌>
NEWAPI_USER_ID=1
AUTOMATION_ADMIN_TOKEN=<自设页面令牌>
AUTOMATION_LISTEN_ADDR=127.0.0.1:8080
EOF
sudo chmod 600 /etc/account-automation.env
```

```ini
# 3. /etc/systemd/system/account-automation.service
[Unit]
Description=SMS688 to NewAPI account automation
After=network.target docker.service

[Service]
ExecStart=/opt/account-automation/account-automation
EnvironmentFile=/etc/account-automation.env
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
# 4. 启动
sudo systemctl daemon-reload
sudo systemctl enable --now account-automation
sudo systemctl status account-automation   # listening on 127.0.0.1:8080
```

服务只监听回环，公网天然不可达；不要改成 `0.0.0.0`。

## 四、测试清单（首次真实测试）

1. **健康检查**（服务器）：`curl http://127.0.0.1:8080/healthz` → `{"status":"ok"}`
2. **建可丢弃测试渠道**：NewAPI 后台新建空 Codex 渠道（type 57），记渠道 ID，测完可删。**不要用生产渠道**（测试失败不回滚旧密钥）
3. **页面访问**：本地 `ssh -L 8080:127.0.0.1:8080 <user>@<server>`，浏览器开 `http://127.0.0.1:8080`
4. **提交**：第一栏填 `AUTOMATION_ADMIN_TOKEN`；每行 `<渠道ID>|<邮箱>----<密码>`；提交后页面 2s 自动轮询
5. **状态判定**：正常 `created → submitting → polling → downloading → processing → succeeded`；批次终态 `completed`
6. **NewAPI 侧核验**：后台打开测试渠道，密钥应已变成 CPA auth JSON，测试按钮通过
7. **失败排查**（error_class）：见下表；`journalctl -u account-automation -f` 看日志（字段仅 batch_id/status/masked_email/channel_id/error_class，无敏感值）

| error_class | 含义 | 动作 |
|---|---|---|
| `sms688_transport_error` / `sms688_http_error` / `sms688_decode_error` | 网络/状态码/响应格式 | 看日志定位；首次真实测试重点观察 |
| `sms688_invalid_response` | 2xx 缺 batch_id | SMS688 真实 Schema 偏差，把日志发维护者 |
| `sms688_expired` / `sms688_failed` / `sms688_cancelled` | 远端任务失败 | 接码平台侧 |
| `download_failed` | CPA 下载失败 | 网络/权限 |
| `credential_invalid` | CPA 缺字段或 email 对不上 | CPA 格式问题 |
| `channel_update_failed` | 预检失败（渠道不存在/非 Codex） | 核对渠道 ID 与类型 |
| `channel_test_failed` | 密钥已写入但测试不过 | **密钥已生效**，按页面渠道 ID 人工检查 |

## 五、已知限制

- **无回滚**：渠道测试失败时新密钥已写入（旧密钥读取接口已硬下线），所以必须用可丢弃渠道试。
- **重启丢批次**：内存态，重启后批次记录消失（SMS688 任务与已写入密钥不受影响）。
- **SMS688 Schema 未经生产样本验证**：首次真实提交即验证；报 decode/invalid_response 类错误时收集日志反馈。
- 每行输入必须显式 `channel_id|` 前缀；仅支持 microsoft 账号模式。
