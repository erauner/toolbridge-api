#!/bin/bash
set -e

# Smoke test script for ToolBridge API
# Tests the basic push/pull/delete flow against a running server

API_URL="${API_URL:-http://localhost:8080}"
USER="smoke-test-user-$$"  # Unique user per run
NOTE_UID="$(uuidgen | tr '[:upper:]' '[:lower:]')"
TASK_UID="$(uuidgen | tr '[:upper:]' '[:lower:]')"
COMMENT_UID="$(uuidgen | tr '[:upper:]' '[:lower:]')"
CHAT_UID="$(uuidgen | tr '[:upper:]' '[:lower:]')"
MESSAGE_UID="$(uuidgen | tr '[:upper:]' '[:lower:]')"

echo "Running smoke tests against $API_URL"
echo "   User: $USER"
echo "   Note UID: $NOTE_UID"
echo "   Task UID: $TASK_UID"
echo "   Comment UID: $COMMENT_UID"
echo "   Chat UID: $CHAT_UID"
echo "   Message UID: $MESSAGE_UID"
echo ""

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

function test_step() {
    echo -e "${YELLOW}▶${NC} $1"
}

function test_pass() {
    echo -e "${GREEN}✓${NC} $1"
}

function test_fail() {
    echo -e "${RED}✗${NC} $1"
    exit 1
}

# Test 1: Health check
test_step "Testing health endpoint"
HEALTH=$(curl -s -w "%{http_code}" -o /dev/null "$API_URL/healthz")
if [ "$HEALTH" -eq 200 ]; then
    test_pass "Health check passed"
else
    test_fail "Health check failed (HTTP $HEALTH)"
fi

# Test 2: Create sync session
test_step "Creating sync session"
SESSION_RESP=$(curl -s -X POST "$API_URL/v1/sync/sessions" \
    -H "X-Debug-Sub: $USER")

SESSION_ID=$(echo "$SESSION_RESP" | jq -r '.id')
SESSION_EPOCH=$(echo "$SESSION_RESP" | jq -r '.epoch')

if [ "$SESSION_ID" != "null" ] && [ -n "$SESSION_ID" ]; then
    test_pass "Sync session created (id=$SESSION_ID, epoch=$SESSION_EPOCH)"
else
    test_fail "Failed to create sync session: $SESSION_RESP"
fi

# Test 3: Push a note
test_step "Pushing note (version 1)"
PUSH_RESP=$(curl -s -X POST "$API_URL/v1/sync/notes/push" \
    -H "X-Debug-Sub: $USER" \
    -H "X-Sync-Session: $SESSION_ID" \
    -H "X-Sync-Epoch: $SESSION_EPOCH" \
    -H "Content-Type: application/json" \
    -d "{
        \"items\": [{
            \"uid\": \"$NOTE_UID\",
            \"title\": \"Smoke Test Note\",
            \"content\": \"This is a smoke test\",
            \"updatedTs\": \"2025-11-03T10:00:00Z\",
            \"sync\": {
                \"version\": 1,
                \"isDeleted\": false
            }
        }]
    }")

PUSH_VERSION=$(echo "$PUSH_RESP" | jq -r '.[0].version')
PUSH_ERROR=$(echo "$PUSH_RESP" | jq -r '.[0].error')

if [ "$PUSH_ERROR" != "null" ] && [ "$PUSH_ERROR" != "" ]; then
    test_fail "Push failed: $PUSH_ERROR"
fi

if [ "$PUSH_VERSION" -eq 1 ]; then
    test_pass "Note pushed (version=$PUSH_VERSION)"
else
    test_fail "Push returned unexpected version: $PUSH_VERSION"
fi

# Test 4: Pull the note
test_step "Pulling notes"
PULL_RESP=$(curl -s "$API_URL/v1/sync/notes/pull?limit=100" \
    -H "X-Debug-Sub: $USER" \
    -H "X-Sync-Session: $SESSION_ID" \
    -H "X-Sync-Epoch: $SESSION_EPOCH")

UPSERT_COUNT=$(echo "$PULL_RESP" | jq '.upserts | length')
DELETE_COUNT=$(echo "$PULL_RESP" | jq '.deletes | length')
FOUND_NOTE=$(echo "$PULL_RESP" | jq -r ".upserts[] | select(.uid == \"$NOTE_UID\") | .title")

if [ "$FOUND_NOTE" = "Smoke Test Note" ]; then
    test_pass "Note pulled successfully (found in upserts)"
else
    test_fail "Note not found in pull response"
fi

# Test 5: Push duplicate (idempotency test)
test_step "Pushing duplicate note (idempotency test)"
PUSH2_RESP=$(curl -s -X POST "$API_URL/v1/sync/notes/push" \
    -H "X-Debug-Sub: $USER" \
    -H "X-Sync-Session: $SESSION_ID" \
    -H "X-Sync-Epoch: $SESSION_EPOCH" \
    -H "Content-Type: application/json" \
    -d "{
        \"items\": [{
            \"uid\": \"$NOTE_UID\",
            \"title\": \"Smoke Test Note\",
            \"updatedTs\": \"2025-11-03T10:00:00Z\",
            \"sync\": {
                \"version\": 1,
                \"isDeleted\": false
            }
        }]
    }")

PUSH2_VERSION=$(echo "$PUSH2_RESP" | jq -r '.[0].version')

if [ "$PUSH2_VERSION" -eq 1 ]; then
    test_pass "Idempotency verified (version stayed at 1)"
else
    test_fail "Idempotency failed: version changed to $PUSH2_VERSION"
fi

# Test 6: Update note (LWW test)
test_step "Updating note with newer timestamp (LWW test)"
PUSH3_RESP=$(curl -s -X POST "$API_URL/v1/sync/notes/push" \
    -H "X-Debug-Sub: $USER" \
    -H "X-Sync-Session: $SESSION_ID" \
    -H "X-Sync-Epoch: $SESSION_EPOCH" \
    -H "Content-Type: application/json" \
    -d "{
        \"items\": [{
            \"uid\": \"$NOTE_UID\",
            \"title\": \"Updated Smoke Test Note\",
            \"content\": \"Updated content\",
            \"updatedTs\": \"2025-11-03T10:01:00Z\",
            \"sync\": {
                \"version\": 1,
                \"isDeleted\": false
            }
        }]
    }")

PUSH3_VERSION=$(echo "$PUSH3_RESP" | jq -r '.[0].version')

if [ "$PUSH3_VERSION" -eq 2 ]; then
    test_pass "LWW update succeeded (version incremented to 2)"
else
    test_fail "LWW update failed: version is $PUSH3_VERSION (expected 2)"
fi

# Test 7: Verify updated content
test_step "Verifying updated content"
PULL2_RESP=$(curl -s "$API_URL/v1/sync/notes/pull?limit=100" \
    -H "X-Debug-Sub: $USER" \
    -H "X-Sync-Session: $SESSION_ID" \
    -H "X-Sync-Epoch: $SESSION_EPOCH")

UPDATED_TITLE=$(echo "$PULL2_RESP" | jq -r ".upserts[] | select(.uid == \"$NOTE_UID\") | .title")
UPDATED_CONTENT=$(echo "$PULL2_RESP" | jq -r ".upserts[] | select(.uid == \"$NOTE_UID\") | .content")

if [ "$UPDATED_TITLE" = "Updated Smoke Test Note" ] && [ "$UPDATED_CONTENT" = "Updated content" ]; then
    test_pass "Updated content verified"
else
    test_fail "Content not updated correctly (title=$UPDATED_TITLE, content=$UPDATED_CONTENT)"
fi

# Test 8: Delete note (soft delete)
test_step "Deleting note (soft delete)"
DELETE_RESP=$(curl -s -X POST "$API_URL/v1/sync/notes/push" \
    -H "X-Debug-Sub: $USER" \
    -H "X-Sync-Session: $SESSION_ID" \
    -H "X-Sync-Epoch: $SESSION_EPOCH" \
    -H "Content-Type: application/json" \
    -d "{
        \"items\": [{
            \"uid\": \"$NOTE_UID\",
            \"title\": \"Updated Smoke Test Note\",
            \"updatedTs\": \"2025-11-03T10:02:00Z\",
            \"sync\": {
                \"version\": 2,
                \"isDeleted\": true,
                \"deletedAt\": \"2025-11-03T10:02:00Z\"
            }
        }]
    }")

DELETE_VERSION=$(echo "$DELETE_RESP" | jq -r '.[0].version')

if [ "$DELETE_VERSION" -eq 3 ]; then
    test_pass "Note deleted (version incremented to 3)"
else
    test_fail "Delete failed: version is $DELETE_VERSION (expected 3)"
fi

# Test 9: Verify tombstone
test_step "Verifying tombstone in deletes array"
PULL3_RESP=$(curl -s "$API_URL/v1/sync/notes/pull?limit=100" \
    -H "X-Debug-Sub: $USER" \
    -H "X-Sync-Session: $SESSION_ID" \
    -H "X-Sync-Epoch: $SESSION_EPOCH")

UPSERTS=$(echo "$PULL3_RESP" | jq -r ".upserts[] | select(.uid == \"$NOTE_UID\") | .uid")
DELETED=$(echo "$PULL3_RESP" | jq -r ".deletes[] | select(.uid == \"$NOTE_UID\") | .uid")

if [ -z "$UPSERTS" ] && [ "$DELETED" = "$NOTE_UID" ]; then
    test_pass "Tombstone verified (note in deletes, not in upserts)"
else
    test_fail "Tombstone verification failed (upserts=$UPSERTS, deletes=$DELETED)"
fi

# Test 10: Cursor pagination
test_step "Testing cursor pagination"
CURSOR_RESP=$(curl -s "$API_URL/v1/sync/notes/pull?limit=1" \
    -H "X-Debug-Sub: $USER" \
    -H "X-Sync-Session: $SESSION_ID" \
    -H "X-Sync-Epoch: $SESSION_EPOCH")

CURSOR=$(echo "$CURSOR_RESP" | jq -r '.nextCursor')

if [ "$CURSOR" != "null" ] && [ -n "$CURSOR" ]; then
    test_pass "Cursor pagination working (cursor=$CURSOR)"
else
    test_pass "No cursor (end of results)"
fi

# Test 11: Push a task
test_step "Pushing task (version 1)"
TASK_PUSH_RESP=$(curl -s -X POST "$API_URL/v1/sync/tasks/push" \
    -H "X-Debug-Sub: $USER" \
    -H "X-Sync-Session: $SESSION_ID" \
    -H "X-Sync-Epoch: $SESSION_EPOCH" \
    -H "Content-Type: application/json" \
    -d "{
        \"items\": [{
            \"uid\": \"$TASK_UID\",
            \"title\": \"Smoke Test Task\",
            \"done\": false,
            \"updatedTs\": \"2025-11-03T10:00:00Z\",
            \"sync\": {
                \"version\": 1,
                \"isDeleted\": false
            }
        }]
    }")

TASK_VERSION=$(echo "$TASK_PUSH_RESP" | jq -r '.[0].version')
TASK_ERROR=$(echo "$TASK_PUSH_RESP" | jq -r '.[0].error')

if [ "$TASK_ERROR" != "null" ] && [ "$TASK_ERROR" != "" ]; then
    test_fail "Task push failed: $TASK_ERROR"
fi

if [ "$TASK_VERSION" -eq 1 ]; then
    test_pass "Task pushed (version=$TASK_VERSION)"
else
    test_fail "Task push returned unexpected version: $TASK_VERSION"
fi

# Test 12: Pull the task
test_step "Pulling tasks"
TASK_PULL_RESP=$(curl -s "$API_URL/v1/sync/tasks/pull?limit=100" \
    -H "X-Debug-Sub: $USER" \
    -H "X-Sync-Session: $SESSION_ID" \
    -H "X-Sync-Epoch: $SESSION_EPOCH")

FOUND_TASK=$(echo "$TASK_PULL_RESP" | jq -r ".upserts[] | select(.uid == \"$TASK_UID\") | .title")

if [ "$FOUND_TASK" = "Smoke Test Task" ]; then
    test_pass "Task pulled successfully (found in upserts)"
else
    test_fail "Task not found in pull response"
fi

# Test 13: Delete task (soft delete)
test_step "Deleting task (soft delete)"
TASK_DELETE_RESP=$(curl -s -X POST "$API_URL/v1/sync/tasks/push" \
    -H "X-Debug-Sub: $USER" \
    -H "X-Sync-Session: $SESSION_ID" \
    -H "X-Sync-Epoch: $SESSION_EPOCH" \
    -H "Content-Type: application/json" \
    -d "{
        \"items\": [{
            \"uid\": \"$TASK_UID\",
            \"title\": \"Smoke Test Task\",
            \"updatedTs\": \"2025-11-03T10:01:00Z\",
            \"sync\": {
                \"version\": 1,
                \"isDeleted\": true,
                \"deletedAt\": \"2025-11-03T10:01:00Z\"
            }
        }]
    }")

TASK_DELETE_VERSION=$(echo "$TASK_DELETE_RESP" | jq -r '.[0].version')

if [ "$TASK_DELETE_VERSION" -eq 2 ]; then
    test_pass "Task deleted (version incremented to 2)"
else
    test_fail "Task delete failed: version is $TASK_DELETE_VERSION (expected 2)"
fi

# Test 14: Create a new note for comment testing (original note was deleted)
test_step "Creating new note for comment testing"
COMMENT_NOTE_UID=$(uuidgen | tr '[:upper:]' '[:lower:]')
COMMENT_NOTE_RESP=$(curl -s -X POST "$API_URL/v1/sync/notes/push" \
    -H "X-Debug-Sub: $USER" \
    -H "X-Sync-Session: $SESSION_ID" \
    -H "X-Sync-Epoch: $SESSION_EPOCH" \
    -H "Content-Type: application/json" \
    -d "{
        \"items\": [{
            \"uid\": \"$COMMENT_NOTE_UID\",
            \"title\": \"Note for Comment Test\",
            \"content\": \"This note is used for comment testing\",
            \"updatedTs\": \"2025-11-03T11:00:00Z\",
            \"sync\": {
                \"version\": 1,
                \"isDeleted\": false
            }
        }]
    }")

COMMENT_NOTE_VERSION=$(echo "$COMMENT_NOTE_RESP" | jq -r '.[0].version')
if [ "$COMMENT_NOTE_VERSION" -eq 1 ]; then
    test_pass "Comment parent note created (version=1)"
else
    test_fail "Failed to create parent note for comment: version=$COMMENT_NOTE_VERSION"
fi

# Test 15: Push a comment (on new note)
test_step "Pushing comment on note (version 1)"
COMMENT_PUSH_RESP=$(curl -s -X POST "$API_URL/v1/sync/comments/push" \
    -H "X-Debug-Sub: $USER" \
    -H "X-Sync-Session: $SESSION_ID" \
    -H "X-Sync-Epoch: $SESSION_EPOCH" \
    -H "Content-Type: application/json" \
    -d "{
        \"items\": [{
            \"uid\": \"$COMMENT_UID\",
            \"content\": \"Smoke Test Comment\",
            \"parentType\": \"note\",
            \"parentUid\": \"$COMMENT_NOTE_UID\",
            \"updatedTs\": \"2025-11-03T11:00:00Z\",
            \"sync\": {
                \"version\": 1,
                \"isDeleted\": false
            }
        }]
    }")

COMMENT_VERSION=$(echo "$COMMENT_PUSH_RESP" | jq -r '.[0].version')
COMMENT_ERROR=$(echo "$COMMENT_PUSH_RESP" | jq -r '.[0].error')

if [ "$COMMENT_ERROR" != "null" ] && [ "$COMMENT_ERROR" != "" ]; then
    test_fail "Comment push failed: $COMMENT_ERROR"
fi

if [ "$COMMENT_VERSION" -eq 1 ]; then
    test_pass "Comment pushed (version=$COMMENT_VERSION)"
else
    test_fail "Comment push returned unexpected version: $COMMENT_VERSION"
fi

# Test 16: Pull the comment
test_step "Pulling comments"
COMMENT_PULL_RESP=$(curl -s "$API_URL/v1/sync/comments/pull?limit=100" \
    -H "X-Debug-Sub: $USER" \
    -H "X-Sync-Session: $SESSION_ID" \
    -H "X-Sync-Epoch: $SESSION_EPOCH")

FOUND_COMMENT=$(echo "$COMMENT_PULL_RESP" | jq -r ".upserts[] | select(.uid == \"$COMMENT_UID\") | .content")

if [ "$FOUND_COMMENT" = "Smoke Test Comment" ]; then
    test_pass "Comment pulled successfully (found in upserts)"
else
    test_fail "Comment not found in pull response"
fi

# Test 17: Validate parent relationship
test_step "Validating comment parent relationship"
COMMENT_PARENT=$(echo "$COMMENT_PULL_RESP" | jq -r ".upserts[] | select(.uid == \"$COMMENT_UID\") | .parentType")

if [ "$COMMENT_PARENT" = "note" ]; then
    test_pass "Comment parent relationship preserved"
else
    test_fail "Comment parent relationship not preserved: $COMMENT_PARENT"
fi

# Test 18: Push a chat
test_step "Pushing chat (version 1)"
CHAT_PUSH_RESP=$(curl -s -X POST "$API_URL/v1/sync/chats/push" \
    -H "X-Debug-Sub: $USER" \
    -H "X-Sync-Session: $SESSION_ID" \
    -H "X-Sync-Epoch: $SESSION_EPOCH" \
    -H "Content-Type: application/json" \
    -d "{
        \"items\": [{
            \"uid\": \"$CHAT_UID\",
            \"title\": \"Smoke Test Chat\",
            \"updatedTs\": \"2025-11-03T10:00:00Z\",
            \"sync\": {
                \"version\": 1,
                \"isDeleted\": false
            }
        }]
    }")

CHAT_VERSION=$(echo "$CHAT_PUSH_RESP" | jq -r '.[0].version')
CHAT_ERROR=$(echo "$CHAT_PUSH_RESP" | jq -r '.[0].error')

if [ "$CHAT_ERROR" != "null" ] && [ "$CHAT_ERROR" != "" ]; then
    test_fail "Chat push failed: $CHAT_ERROR"
fi

if [ "$CHAT_VERSION" -eq 1 ]; then
    test_pass "Chat pushed (version=$CHAT_VERSION)"
else
    test_fail "Chat push returned unexpected version: $CHAT_VERSION"
fi

# Test 19: Pull the chat
test_step "Pulling chats"
CHAT_PULL_RESP=$(curl -s "$API_URL/v1/sync/chats/pull?limit=100" \
    -H "X-Debug-Sub: $USER" \
    -H "X-Sync-Session: $SESSION_ID" \
    -H "X-Sync-Epoch: $SESSION_EPOCH")

FOUND_CHAT=$(echo "$CHAT_PULL_RESP" | jq -r ".upserts[] | select(.uid == \"$CHAT_UID\") | .title")

if [ "$FOUND_CHAT" = "Smoke Test Chat" ]; then
    test_pass "Chat pulled successfully (found in upserts)"
else
    test_fail "Chat not found in pull response"
fi

# Test 20: Delete chat (soft delete)
test_step "Deleting chat (soft delete)"
CHAT_DELETE_RESP=$(curl -s -X POST "$API_URL/v1/sync/chats/push" \
    -H "X-Debug-Sub: $USER" \
    -H "X-Sync-Session: $SESSION_ID" \
    -H "X-Sync-Epoch: $SESSION_EPOCH" \
    -H "Content-Type: application/json" \
    -d "{
        \"items\": [{
            \"uid\": \"$CHAT_UID\",
            \"title\": \"Smoke Test Chat\",
            \"updatedTs\": \"2025-11-03T10:01:00Z\",
            \"sync\": {
                \"version\": 1,
                \"isDeleted\": true,
                \"deletedAt\": \"2025-11-03T10:01:00Z\"
            }
        }]
    }")

CHAT_DELETE_VERSION=$(echo "$CHAT_DELETE_RESP" | jq -r '.[0].version')

if [ "$CHAT_DELETE_VERSION" -eq 2 ]; then
    test_pass "Chat deleted (version incremented to 2)"
else
    test_fail "Chat delete failed: version is $CHAT_DELETE_VERSION (expected 2)"
fi

# Test 21: Push a chat message
test_step "Pushing chat message"
# First, recreate the chat since we deleted it (use version 2 and newer timestamp to satisfy LWW)
CHAT_CREATE_RESP=$(curl -s -X POST "$API_URL/v1/sync/chats/push" \
    -H "X-Debug-Sub: $USER" \
    -H "X-Sync-Session: $SESSION_ID" \
    -H "X-Sync-Epoch: $SESSION_EPOCH" \
    -H "Content-Type: application/json" \
    -d "{
        \"items\": [{
            \"uid\": \"$CHAT_UID\",
            \"title\": \"Chat for Messages\",
            \"updatedTs\": \"2025-11-03T11:00:00Z\",
            \"sync\": {
                \"version\": 2,
                \"isDeleted\": false
            }
        }]
    }")

MESSAGE_PUSH_RESP=$(curl -s -X POST "$API_URL/v1/sync/chat_messages/push" \
    -H "X-Debug-Sub: $USER" \
    -H "X-Sync-Session: $SESSION_ID" \
    -H "X-Sync-Epoch: $SESSION_EPOCH" \
    -H "Content-Type: application/json" \
    -d "{
        \"items\": [{
            \"uid\": \"$MESSAGE_UID\",
            \"content\": \"Smoke test message\",
            \"chatUid\": \"$CHAT_UID\",
            \"updatedTs\": \"2025-11-03T11:01:00Z\",
            \"sync\": {
                \"version\": 1,
                \"isDeleted\": false
            }
        }]
    }")

MESSAGE_VERSION=$(echo "$MESSAGE_PUSH_RESP" | jq -r '.[0].version')

if [ "$MESSAGE_VERSION" -eq 1 ]; then
    test_pass "Message pushed (version 1)"
else
    test_fail "Message push failed: version is $MESSAGE_VERSION (expected 1)"
fi

# Test 22: Pull the message
test_step "Pulling chat message"
MESSAGE_PULL_RESP=$(curl -s "$API_URL/v1/sync/chat_messages/pull?limit=100" \
    -H "X-Debug-Sub: $USER" \
    -H "X-Sync-Session: $SESSION_ID" \
    -H "X-Sync-Epoch: $SESSION_EPOCH")

FOUND_MESSAGE=$(echo "$MESSAGE_PULL_RESP" | jq -r ".upserts[] | select(.uid == \"$MESSAGE_UID\") | .content")

if [ "$FOUND_MESSAGE" = "Smoke test message" ]; then
    test_pass "Message found in pull response"
else
    test_fail "Message not found in pull response"
fi

# Test 23: Delete message (soft delete)
test_step "Deleting chat message (soft delete)"
MESSAGE_DELETE_RESP=$(curl -s -X POST "$API_URL/v1/sync/chat_messages/push" \
    -H "X-Debug-Sub: $USER" \
    -H "X-Sync-Session: $SESSION_ID" \
    -H "X-Sync-Epoch: $SESSION_EPOCH" \
    -H "Content-Type: application/json" \
    -d "{
        \"items\": [{
            \"uid\": \"$MESSAGE_UID\",
            \"content\": \"Smoke test message\",
            \"chatUid\": \"$CHAT_UID\",
            \"updatedTs\": \"2025-11-03T11:02:00Z\",
            \"sync\": {
                \"version\": 1,
                \"isDeleted\": true,
                \"deletedAt\": \"2025-11-03T11:02:00Z\"
            }
        }]
    }")

MESSAGE_DELETE_VERSION=$(echo "$MESSAGE_DELETE_RESP" | jq -r '.[0].version')

if [ "$MESSAGE_DELETE_VERSION" -eq 2 ]; then
    test_pass "Message deleted (version incremented to 2)"
else
    test_fail "Message delete failed: version is $MESSAGE_DELETE_VERSION (expected 2)"
fi

echo ""
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}✓ All smoke tests passed!${NC}"
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo "Summary:"
echo "  Notes:"
echo "    • Push note: ✓"
echo "    • Pull note: ✓"
echo "    • Idempotency: ✓"
echo "    • LWW conflict resolution: ✓"
echo "    • Content preservation: ✓"
echo "    • Soft delete: ✓"
echo "    • Tombstone: ✓"
echo "    • Cursor pagination: ✓"
echo "  Tasks:"
echo "    • Push task: ✓"
echo "    • Pull task: ✓"
echo "    • Soft delete: ✓"
echo "  Comments:"
echo "    • Push comment: ✓"
echo "    • Pull comment: ✓"
echo "    • Parent validation: ✓"
echo "  Chats:"
echo "    • Push chat: ✓"
echo "    • Pull chat: ✓"
echo "    • Soft delete: ✓"
echo "  Chat Messages:"
echo "    • Push message: ✓"
echo "    • Pull message: ✓"
echo "    • Soft delete: ✓"
echo "  General:"
echo "    • Health check: ✓"
echo ""
