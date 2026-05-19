#!/bin/bash
KEY="361bfc14a800466599bcbd7d858a6573.KybreXLy6J5sQb3v"

echo "=== Test 1: /v4/chat/completions ==="
curl -s -w "\n---HTTP_CODE:%{http_code}" -X POST "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions" -H "Content-Type: application/json" -H "Authorization: Bearer ${KEY}" -d '{"model":"glm-4-flash","messages":[{"role":"user","content":"hi"}]}'

echo ""
echo ""
echo "=== Test 2: /v4/v1/chat/completions ==="
curl -s -w "\n---HTTP_CODE:%{http_code}" -X POST "https://open.bigmodel.cn/api/coding/paas/v4/v1/chat/completions" -H "Content-Type: application/json" -H "Authorization: Bearer ${KEY}" -d '{"model":"glm-4-flash","messages":[{"role":"user","content":"hi"}]}'
