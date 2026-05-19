#!/bin/bash
#
# 批量导入用户脚本
#
# 使用方法：
#   1. 启动服务后，登录管理员账号，在设置中生成一个 Access Token
#   2. 执行：bash scripts/import_users.sh <base_url> <admin_token> <admin_user_id>
#   例如：bash scripts/import_users.sh http://localhost:3005 sk-xxx 1
#
# 用户信息文件格式（每行）：姓名,手机号,部门
#   例如：龚建波,13270217941,机器人与无人化平台事业部

BASE_URL="$1"
TOKEN="$2"
ADMIN_USER_ID="$3"
USER_FILE="$(dirname "$0")/user_info.txt"

if [ -z "$BASE_URL" ] || [ -z "$TOKEN" ] || [ -z "$ADMIN_USER_ID" ]; then
    echo "用法: $0 <base_url> <admin_token> <admin_user_id>"
    echo "例如: $0 http://localhost:3005 sk-xxx 1"
    exit 1
fi

if [ ! -f "$USER_FILE" ]; then
    echo "错误: 找不到用户文件 $USER_FILE"
    exit 1
fi

SUCCESS=0
FAIL=0
SKIP=0
TOTAL=0

while IFS=',' read -r name phone department; do
    name=$(echo "$name" | xargs)
    phone=$(echo "$phone" | xargs)
    department=$(echo "$department" | xargs)

    [ -z "$name" ] && continue

    TOTAL=$((TOTAL + 1))
    password="wattman${phone: -4}"

    echo -n "[$TOTAL] $name ($phone) -> $department ... "

    # Step 1: 查询用户是否已存在
    user_id=$(curl -s "${BASE_URL}/api/user/search?keyword=${phone}" \
        -H "Authorization: Bearer ${TOKEN}" \
        -H "New-Api-User: ${ADMIN_USER_ID}" | \
        grep -o '"id":[[:space:]]*[0-9]*' | head -1 | grep -o '[0-9]*')
    sleep 1

    if [ -n "$user_id" ]; then
        # 用户已存在，直接更新分组
        echo -n "(已存在，更新分组) "
        update_resp=$(curl -s -X PUT "${BASE_URL}/api/user/" \
            -H "Authorization: Bearer ${TOKEN}" \
            -H "New-Api-User: ${ADMIN_USER_ID}" \
            -H "Content-Type: application/json" \
            -d "{
                \"id\": ${user_id},
                \"username\": \"${phone}\",
                \"display_name\": \"${name}\",
                \"group\": \"${department}\"
            }")
        sleep 1

        update_success=$(echo "$update_resp" | grep -o '"success":[[:space:]]*true')
        if [ -z "$update_success" ]; then
            echo "失败 (更新分组)"
            FAIL=$((FAIL + 1))
            continue
        fi
        echo "成功"
        SUCCESS=$((SUCCESS + 1))
        continue
    fi

    # Step 2: 用户不存在，创建用户
    create_resp=$(curl -s -X POST "${BASE_URL}/api/user/" \
        -H "Authorization: Bearer ${TOKEN}" \
        -H "New-Api-User: ${ADMIN_USER_ID}" \
        -H "Content-Type: application/json" \
        -d "{
            \"username\": \"${phone}\",
            \"password\": \"${password}\",
            \"display_name\": \"${name}\"
        }")
    sleep 1

    create_success=$(echo "$create_resp" | grep -o '"success":[[:space:]]*true')
    if [ -z "$create_success" ]; then
        msg=$(echo "$create_resp" | grep -o '"message":"[^"]*"' | head -1)
        echo "失败 (创建) $msg"
        FAIL=$((FAIL + 1))
        continue
    fi

    # Step 3: 获取新创建用户的 ID
    user_id=$(curl -s "${BASE_URL}/api/user/search?keyword=${phone}" \
        -H "Authorization: Bearer ${TOKEN}" \
        -H "New-Api-User: ${ADMIN_USER_ID}" | \
        grep -o '"id":[[:space:]]*[0-9]*' | head -1 | grep -o '[0-9]*')
    sleep 1

    if [ -z "$user_id" ]; then
        echo "失败 (未找到用户ID)"
        FAIL=$((FAIL + 1))
        continue
    fi

    # Step 4: 更新分组
    update_resp=$(curl -s -X PUT "${BASE_URL}/api/user/" \
        -H "Authorization: Bearer ${TOKEN}" \
        -H "New-Api-User: ${ADMIN_USER_ID}" \
        -H "Content-Type: application/json" \
        -d "{
            \"id\": ${user_id},
            \"username\": \"${phone}\",
            \"display_name\": \"${name}\",
            \"group\": \"${department}\"
        }")
    sleep 1

    update_success=$(echo "$update_resp" | grep -o '"success":[[:space:]]*true')
    if [ -z "$update_success" ]; then
        echo "失败 (更新分组)"
        FAIL=$((FAIL + 1))
        continue
    fi

    echo "成功 (新建)"
    SUCCESS=$((SUCCESS + 1)

done < "$USER_FILE"

echo ""
echo "=============================="
echo "导入完成: 总计 $TOTAL, 成功 $SUCCESS, 失败 $FAIL"
echo "=============================="
