#!/usr/bin/env bash
# TeamPulse 全链路 Smoke Test
# 覆盖全部 28 个 API 端点，验证主流程通畅
# 用法: bash hack/smoke-test.sh [BASE_URL]
# 前置条件: 服务已启动，MySQL 和 Redis 可用

set -euo pipefail

BASE="${1:-http://127.0.0.1:8000}"
PASS=0
FAIL=0
TOTAL=0

# ─── Helpers ────────────────────────────────────────────────────────────────────

check() {
  local label="$1"
  local expected="$2"
  local actual="$3"
  TOTAL=$((TOTAL + 1))
  if echo "$actual" | grep -q "$expected"; then
    echo "  ✅ $label"
    PASS=$((PASS + 1))
  else
    echo "  ❌ $label"
    echo "     期望包含: $expected"
    echo "     实际返回: $actual"
    FAIL=$((FAIL + 1))
  fi
}

json_field() {
  echo "$1" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d$2)" 2>/dev/null || echo ""
}

# ─── 1. 注册 ────────────────────────────────────────────────────────────────────

echo ""
echo "═══════════════════════════════════════════════════"
echo "  TeamPulse Smoke Test — 全链路 28 端点验证"
echo "═══════════════════════════════════════════════════"
echo ""
echo "▶ [1/14] 注册用户"

REG1=$(curl -s -X POST "$BASE/auth/register" \
  -H 'Content-Type: application/json' \
  -d '{"username":"smoketest1","password":"123456","nickname":"烟雾测试员1"}')
check "注册用户1" '"userId"' "$REG1"
USER1_ID=$(json_field "$REG1" "['data']['userId']")
echo "     用户1 ID: $USER1_ID"

REG2=$(curl -s -X POST "$BASE/auth/register" \
  -H 'Content-Type: application/json' \
  -d '{"username":"smoketest2","password":"123456","nickname":"烟雾测试员2"}')
check "注册用户2" '"userId"' "$REG2"
USER2_ID=$(json_field "$REG2" "['data']['userId']")
echo "     用户2 ID: $USER2_ID"

# ─── 2. 获取验证码 ──────────────────────────────────────────────────────────────

echo ""
echo "▶ [2/14] 获取验证码"

CAP1=$(curl -s -X POST "$BASE/auth/captcha" \
  -H 'Content-Type: application/json' \
  -d '{"username":"smoketest1"}')
check "获取验证码" '"code"' "$CAP1"
CODE1=$(json_field "$CAP1" "['data']['code']")
echo "     验证码: $CODE1"

# ─── 3. 登录 ────────────────────────────────────────────────────────────────────

echo ""
echo "▶ [3/14] 登录"

LOGIN1=$(curl -s -X POST "$BASE/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"smoketest1\",\"password\":\"123456\",\"captcha\":\"$CODE1\"}")
check "用户1登录" '"token"' "$LOGIN1"
TOKEN1=$(json_field "$LOGIN1" "['data']['token']")
echo "     Token1: ${TOKEN1:0:20}..."

# 用户2登录
CAP2=$(curl -s -X POST "$BASE/auth/captcha" \
  -H 'Content-Type: application/json' \
  -d '{"username":"smoketest2"}')
CODE2=$(json_field "$CAP2" "['data']['code']")

LOGIN2=$(curl -s -X POST "$BASE/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"smoketest2\",\"password\":\"123456\",\"captcha\":\"$CODE2\"}")
check "用户2登录" '"token"' "$LOGIN2"
TOKEN2=$(json_field "$LOGIN2" "['data']['token']")

AUTH1="Authorization: Bearer $TOKEN1"
AUTH2="Authorization: Bearer $TOKEN2"

# ─── 4. 用户资料 ────────────────────────────────────────────────────────────────

echo ""
echo "▶ [4/14] 用户资料 (Cache Aside)"

PROFILE=$(curl -s "$BASE/user/profile" -H "$AUTH1")
check "获取个人资料" '"nickname"' "$PROFILE"
echo "     昵称: $(json_field "$PROFILE" "['data']['nickname']")"

UPD_PROFILE=$(curl -s -X PUT "$BASE/user/profile" \
  -H "$AUTH1" \
  -H 'Content-Type: application/json' \
  -d '{"nickname":"烟雾一号"}')
check "更新昵称" '"code":0' "$UPD_PROFILE"

# ─── 5. 创建团队 ────────────────────────────────────────────────────────────────

echo ""
echo "▶ [5/14] 创建团队"

TEAM=$(curl -s -X POST "$BASE/teams" \
  -H "$AUTH1" \
  -H 'Content-Type: application/json' \
  -d '{"name":"SmokeTest团队"}')
check "创建团队" '"teamId"' "$TEAM"
TEAM_ID=$(json_field "$TEAM" "['data']['teamId']")
echo "     团队ID: $TEAM_ID"

# ─── 6. 添加成员 ────────────────────────────────────────────────────────────────

echo ""
echo "▶ [6/14] 添加团队成员"

ADD_MEM=$(curl -s -X POST "$BASE/teams/$TEAM_ID/members" \
  -H "$AUTH1" \
  -H 'Content-Type: application/json' \
  -d "{\"userId\":$USER2_ID}")
check "添加成员" '"code":0' "$ADD_MEM"

MEMBERS=$(curl -s "$BASE/teams/$TEAM_ID/members" -H "$AUTH1")
check "查询成员列表" '"members"' "$MEMBERS"

# ─── 7. 在线心跳 ────────────────────────────────────────────────────────────────

echo ""
echo "▶ [7/14] 在线心跳"

HB=$(curl -s -X POST "$BASE/presence/heartbeat" \
  -H "$AUTH1" \
  -H 'Content-Type: application/json' \
  -d "{\"teamId\":$TEAM_ID}")
check "心跳上报" '"code":0' "$HB"

ONLINE=$(curl -s "$BASE/teams/$TEAM_ID/online-members" -H "$AUTH1")
check "查询在线成员" '"members"' "$ONLINE"

# ─── 8. 创建任务 ────────────────────────────────────────────────────────────────

echo ""
echo "▶ [8/14] 创建任务"

TASK1=$(curl -s -X POST "$BASE/teams/$TEAM_ID/tasks" \
  -H "$AUTH1" \
  -H 'Content-Type: application/json' \
  -d '{"title":"Smoke Test 任务","description":"验证全链路","assigneeId":0,"priority":2}')
check "创建任务1" '"taskId"' "$TASK1"
TASK1_ID=$(json_field "$TASK1" "['data']['taskId']")
echo "     任务1 ID: $TASK1_ID"

TASK2=$(curl -s -X POST "$BASE/teams/$TEAM_ID/tasks" \
  -H "$AUTH1" \
  -H 'Content-Type: application/json' \
  -d "{\"title\":\"指派给用户2\",\"description\":\"测试指派通知\",\"assigneeId\":$USER2_ID,\"priority\":3}")
check "创建任务2（指派）" '"taskId"' "$TASK2"
TASK2_ID=$(json_field "$TASK2" "['data']['taskId']")
echo "     任务2 ID: $TASK2_ID"

# ─── 9. 任务操作 ────────────────────────────────────────────────────────────────

echo ""
echo "▶ [9/14] 任务编辑、状态流转、详情、热门"

# 详情
DETAIL=$(curl -s "$BASE/tasks/$TASK1_ID" -H "$AUTH1")
check "任务详情" '"task"' "$DETAIL"

# 编辑
UPD_TASK=$(curl -s -X PUT "$BASE/tasks/$TASK1_ID" \
  -H "$AUTH1" \
  -H 'Content-Type: application/json' \
  -d '{"title":"Smoke Test 任务（已编辑）","description":"编辑验证","assigneeId":0,"priority":1}')
check "编辑任务" '"code":0' "$UPD_TASK"

# 状态流转
STATUS_DOING=$(curl -s -X PATCH "$BASE/tasks/$TASK1_ID/status" \
  -H "$AUTH1" \
  -H 'Content-Type: application/json' \
  -d '{"status":"doing"}')
check "状态→doing" '"code":0' "$STATUS_DOING"

STATUS_DONE=$(curl -s -X PATCH "$BASE/tasks/$TASK1_ID/status" \
  -H "$AUTH1" \
  -H 'Content-Type: application/json' \
  -d '{"status":"done"}')
check "状态→done" '"code":0' "$STATUS_DONE"

# 任务列表
TASKS=$(curl -s "$BASE/teams/$TEAM_ID/tasks" -H "$AUTH1")
check "任务列表" '"tasks"' "$TASKS"

# 热门排行
HOT=$(curl -s "$BASE/teams/$TEAM_ID/tasks/hot" -H "$AUTH1")
check "热门任务排行" '"tasks"' "$HOT"

# ─── 10. 评论 ───────────────────────────────────────────────────────────────────

echo ""
echo "▶ [10/14] 评论与提及通知"

COMMENT=$(curl -s -X POST "$BASE/tasks/$TASK2_ID/comments" \
  -H "$AUTH1" \
  -H 'Content-Type: application/json' \
  -d "{\"content\":\"请同步进度\",\"mentionUserIds\":[$USER2_ID]}")
check "创建评论" '"commentId"' "$COMMENT"

COMMENTS=$(curl -s "$BASE/tasks/$TASK2_ID/comments" -H "$AUTH1")
check "查询评论列表" '"list"' "$COMMENTS"

# ─── 11. 点赞 ───────────────────────────────────────────────────────────────────

echo ""
echo "▶ [11/14] 点赞"

LIKE=$(curl -s -X POST "$BASE/tasks/$TASK1_ID/like" -H "$AUTH1")
check "点赞" '"likeCount"' "$LIKE"

LIKE_STATUS=$(curl -s "$BASE/tasks/$TASK1_ID/like-status" -H "$AUTH1")
check "点赞状态" '"isLiked"' "$LIKE_STATUS"

UNLIKE=$(curl -s -X DELETE "$BASE/tasks/$TASK1_ID/like" -H "$AUTH1")
check "取消点赞" '"likeCount"' "$UNLIKE"

# ─── 12. 提醒 ───────────────────────────────────────────────────────────────────

echo ""
echo "▶ [12/14] 延迟提醒"

REMIND_AT=$(($(date +%s) + 3600))
REMINDER=$(curl -s -X POST "$BASE/tasks/$TASK1_ID/reminder" \
  -H "$AUTH1" \
  -H 'Content-Type: application/json' \
  -d "{\"remindAt\":$REMIND_AT}")
check "设置提醒" '"code":0' "$REMINDER"

CANCEL_REM=$(curl -s -X DELETE "$BASE/tasks/$TASK1_ID/reminder" -H "$AUTH1")
check "取消提醒" '"code":0' "$CANCEL_REM"

# ─── 13. 通知 ───────────────────────────────────────────────────────────────────

echo ""
echo "▶ [13/14] 通知中心"

NOTIFS=$(curl -s "$BASE/notifications" -H "$AUTH2")
check "通知列表（用户2）" '"notifications"' "$NOTIFS"

UNREAD=$(curl -s "$BASE/notifications/unread-count" -H "$AUTH2")
check "未读通知数" '"count"' "$UNREAD"
UNREAD_COUNT=$(json_field "$UNREAD" "['data']['count']")
echo "     未读数: $UNREAD_COUNT"

# 标记已读（如果有通知）
if [ -n "$UNREAD_COUNT" ] && [ "$UNREAD_COUNT" != "0" ]; then
  FIRST_NOTIF_ID=$(echo "$NOTIFS" | python3 -c "
import sys,json
d=json.load(sys.stdin)
ns=d.get('data',{}).get('notifications',[])
print(ns[0]['notificationId'] if ns else '')
" 2>/dev/null || echo "")
  if [ -n "$FIRST_NOTIF_ID" ]; then
    MARK_READ=$(curl -s -X PATCH "$BASE/notifications/$FIRST_NOTIF_ID/read" -H "$AUTH2")
    check "标记通知已读" '"code":0' "$MARK_READ"
  fi
fi

# ─── 14. 动态流 ─────────────────────────────────────────────────────────────────

echo ""
echo "▶ [14/14] 团队动态流"

ACTS=$(curl -s "$BASE/teams/$TEAM_ID/activities" -H "$AUTH1")
check "团队动态" '"activities"' "$ACTS"

# ─── 15. 登出 ───────────────────────────────────────────────────────────────────

echo ""
echo "▶ [收尾] 登出与黑名单"

LOGOUT=$(curl -s -X POST "$BASE/auth/logout" -H "$AUTH1")
check "登出" '"code":0' "$LOGOUT"

# 验证 Token 已失效
AFTER_LOGOUT=$(curl -s "$BASE/user/profile" -H "$AUTH1")
check "登出后访问被拒" '401' "$AFTER_LOGOUT"

# ─── Summary ────────────────────────────────────────────────────────────────────

echo ""
echo "═══════════════════════════════════════════════════"
echo "  测试结果: $PASS/$TOTAL 通过, $FAIL 失败"
echo "═══════════════════════════════════════════════════"

if [ "$FAIL" -gt 0 ]; then
  echo "  ⚠️  有失败项，请检查上方输出"
  exit 1
else
  echo "  🎉 全部通过！项目后端链路完整通畅。"
  exit 0
fi
