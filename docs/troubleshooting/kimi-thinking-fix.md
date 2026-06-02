# Claude Code 使用 Kimi K2.6 模型报错修复

## 问题描述

Claude Code 通过 new-api 代理使用 `kimi-k2.6` 模型时，多轮工具调用报错：

```
API Error: 400 thinking is enabled but reasoning_content is missing in assistant tool call message at index 78
```

## 根因分析

### 请求链路

```
Claude Code → new-api (/v1/messages, Anthropic格式)
            → ClaudeHelper (claude_handler.go)
            → OpenAI Adaptor (渠道类型=1)
            → ClaudeToOpenAIRequest (格式转换，thinking字段被丢弃)
            → nginx代理 (http://my_nginx:3004)
            → Kimi API
```

### 错误原因

1. **Kimi K2.6 默认启用思考模式**：即使请求中没有 `thinking` 参数，Kimi 也会启用思考并返回 `reasoning_content`
2. **格式转换丢失 thinking 字段**：渠道类型为 OpenAI(1) 时，Anthropic 格式会被转换为 OpenAI 格式，`thinking` 字段在转换中被丢弃
3. **param_override 的 delete 操作无效**：转换后的 OpenAI 格式 JSON 中没有 `thinking` 字段，`delete` 操作等于无效
4. **多轮对话 reasoning_content 丢失**：Claude Code 在构建后续请求时不保留 `reasoning_content`，导致 Kimi 报 400 错误

### 为什么最初的 param_override 不生效

```json
// 错误配置 - 不生效
{
  "operations": [
    {"path": "thinking", "mode": "delete"}
  ]
}
```

原因：`delete` 操作只能删除已存在的字段。Claude → OpenAI 格式转换后，`thinking` 字段已经被丢弃，无字段可删。

## 修复方案

### 方法：使用 set 操作主动注入 thinking: disabled

在渠道的 `param_override` 中使用 `set` 操作，主动向请求中注入 `thinking: {"type": "disabled"}`：

```json
{
  "operations": [
    {
      "path": "thinking",
      "mode": "set",
      "value": {"type": "disabled"}
    }
  ]
}
```

这样即使格式转换丢弃了原始的 `thinking` 字段，param_override 也会在最终 JSON 中注入 `thinking: {"type": "disabled"}`，告诉 Kimi 禁用思考模式。

### 操作步骤

1. 登录 new-api 管理后台
2. 进入 **渠道管理** → 找到对应的 Kimi 渠道 → **编辑**
3. 在 **参数覆盖 (param_override)** 字段中填入上面的 JSON
4. 保存

### 通过 API 修改

```bash
curl -X PUT "http://<new-api-host>/api/channel/" \
  -H "Content-Type: application/json" \
  -H "Cookie: session=<session-cookie>" \
  -H "New-Api-User: <user-id>" \
  -d '{
    "id": <channel-id>,
    "param_override": "{\"operations\":[{\"path\":\"thinking\",\"mode\":\"set\",\"value\":{\"type\":\"disabled\"}}]}"
  }'
```

## 验证方法

### 1. 单轮请求测试

```bash
curl -X POST http://<new-api-host>/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: <api-key>" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "kimi-k2.6",
    "max_tokens": 200,
    "thinking": {"type": "enabled", "budget_tokens": 5000},
    "messages": [{"role": "user", "content": "1+1等于几？只回答数字"}]
  }'
```

响应中不应包含 `reasoning_content`，且不应报错。

### 2. OpenAI 格式测试

```bash
curl -X POST http://<new-api-host>/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <api-key>" \
  -d '{
    "model": "kimi-k2.6",
    "max_tokens": 200,
    "thinking": {"type": "disabled"},
    "messages": [{"role": "user", "content": "1+1等于几？只回答数字"}]
  }'
```

## 注意事项

- 渠道类型为 OpenAI(1) 时，Anthropic 格式请求会被自动转换为 OpenAI 格式
- Kimi 的 OpenAI 兼容接口支持 `thinking` 参数
- 如果需要启用 Kimi 的思考能力，需要解决 Claude Code 多轮对话中 `reasoning_content` 的保留问题（目前 Claude Code 不支持）

## 参考

- [Kimi K2 Thinking 模型文档](https://platform.kimi.com/docs/guide/use-kimi-k2-thinking-model)
- [new-api param_override 文档](../relay/common/override.go)
