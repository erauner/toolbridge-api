# REST API Testing

This document describes how to test the ToolBridge REST API endpoints.

## Integration Tests

The project includes comprehensive integration tests that cover the same functionality:

```bash
# Run all tests (requires database)
make test-all

# Run only integration tests
make test-integration
```

### Key Integration Test Files

- `internal/httpapi/rest_crud_test.go` - **Comprehensive table-driven CRUD tests for all entities**
  - Tests all 5 entities: Notes, Tasks, Chats, ChatMessages, Comments
  - Full CRUD lifecycle: POST, GET, PATCH, DELETE, LIST
  - Optimistic locking with If-Match headers (RFC 7232 compliant)
  - Soft delete and tombstone handling
  - Parent-child referential integrity
  - 31 test cases covering all REST API functionality
- `internal/httpapi/rest_items_test.go` - Additional REST endpoint tests
- `internal/httpapi/sync_notes_test.go` - Sync protocol tests for notes
- `internal/httpapi/sync_tasks_test.go` - Sync protocol tests for tasks
- `internal/httpapi/session_required_test.go` - Session requirement tests
- `internal/httpapi/ratelimit_test.go` - Rate limiting tests

### Test Coverage

The `rest_crud_test.go` file provides comprehensive coverage of:

1. **Session Creation** - Creating sync sessions with `POST /v1/sync/sessions`
2. **CRUD Operations** - Full lifecycle for all entities (Notes, Tasks, Chats, ChatMessages, Comments)
3. **Optimistic Locking** - Testing `If-Match` header with quoted ETags (RFC 7232)
4. **Conflict Detection** - Testing 412 Precondition Failed for stale versions
5. **Soft Deletes** - Deleting items via `DELETE /v1/{entity}/{uid}`
6. **Tombstone Handling** - 410 Gone responses for deleted items
7. **includeDeleted Parameter** - Retrieving tombstones with `?includeDeleted=true`
8. **Mutation Prevention** - 410 responses when trying to mutate deleted items
9. **Parent-Child Integrity** - ChatMessages require valid Chat, Comments require valid Note/Task

### Test Features

All integration tests properly:
- Create test users with valid UUIDs
- Create sync sessions before making requests
- Include required headers (`X-Debug-Sub`, `X-Sync-Session`, `X-Sync-Epoch`)
- Test optimistic locking with If-Match headers returning 412 for conflicts
- Verify soft delete behavior and tombstones
- Test parent-child referential integrity
- Validate error responses

## Development Notes

The REST API requires:
- Valid sync session created via `POST /v1/sync/sessions`
- Session headers on all sync-related endpoints:
  - `X-Sync-Session`: Session ID
  - `X-Sync-Epoch`: Current epoch number
- In dev mode, `X-Debug-Sub` header can be used instead of JWT tokens

For production use, replace `X-Debug-Sub` with proper JWT authentication.
