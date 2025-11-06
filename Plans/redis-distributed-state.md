# Redis-Backed Distributed State (Phase 8+)

## Current Limitation

As of Phase 7, the ToolBridge API uses in-memory storage for:
- **Sync session management** (`SessionStore` in `internal/httpapi/sessions.go`)
- **Rate limiter state** (`RateLimiter` in `internal/httpapi/ratelimit.go`)

This creates a scaling limitation:
- `replicaCount` must be set to `1` in the Helm chart
- Sessions are not shared between pods
- With multiple replicas, users experience intermittent `428 Precondition Required` errors
- Rate limits are enforced per-pod instead of globally

## Proposed Solution

Implement Redis-backed distributed storage for session and rate limit state, enabling horizontal scaling with multiple API replicas.

## Architecture

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  API Pod 1  │────▶│    Redis    │◀────│  API Pod 2  │
│             │     │   Cluster   │     │             │
│ - Sessions  │     │             │     │ - Sessions  │
│ - RateLimits│     │ Shared State│     │ - RateLimits│
└─────────────┘     └─────────────┘     └─────────────┘
```

## Implementation Plan

### 1. Add Redis Dependency

**chart/Chart.yaml:**
```yaml
dependencies:
  - name: redis
    version: "18.x.x"
    repository: "https://charts.bitnami.com/bitnami"
    condition: redis.enabled
```

**chart/values.yaml:**
```yaml
# Redis configuration (for distributed state)
redis:
  enabled: true
  architecture: standalone  # or 'replication' for HA
  auth:
    enabled: true
    # Password will be auto-generated or set via secret
  master:
    persistence:
      enabled: true
      size: 8Gi
    resources:
      requests:
        cpu: 100m
        memory: 128Mi
      limits:
        cpu: 500m
        memory: 512Mi
```

### 2. Session Store Implementation

**internal/httpapi/sessions.go:**
```go
// SessionStore interface for pluggable backends
type SessionStore interface {
    CreateSession(userID string) (Session, error)
    GetSession(sessionID string) (Session, bool)
    DeleteSession(sessionID string) bool
    CleanupExpired() error
}

// RedisSessionStore implements SessionStore using Redis
type RedisSessionStore struct {
    client *redis.Client
    ttl    time.Duration
}

func NewRedisSessionStore(redisURL string, ttl time.Duration) (*RedisSessionStore, error) {
    opt, err := redis.ParseURL(redisURL)
    if err != nil {
        return nil, err
    }

    client := redis.NewClient(opt)

    // Test connection
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if err := client.Ping(ctx).Err(); err != nil {
        return nil, fmt.Errorf("redis connection failed: %w", err)
    }

    return &RedisSessionStore{
        client: client,
        ttl:    ttl,
    }, nil
}

func (s *RedisSessionStore) CreateSession(userID string) (Session, error) {
    session := Session{
        ID:        uuid.New().String(),
        UserID:    userID,
        CreatedAt: time.Now().UTC(),
        ExpiresAt: time.Now().UTC().Add(s.ttl),
    }

    // Store in Redis with TTL
    ctx := context.Background()
    key := "session:" + session.ID
    data, _ := json.Marshal(session)

    err := s.client.Set(ctx, key, data, s.ttl).Err()
    if err != nil {
        return Session{}, err
    }

    // Create user->session index for cleanup
    userKey := "user_sessions:" + userID
    s.client.SAdd(ctx, userKey, session.ID)
    s.client.Expire(ctx, userKey, s.ttl)

    return session, nil
}

func (s *RedisSessionStore) GetSession(sessionID string) (Session, bool) {
    ctx := context.Background()
    key := "session:" + sessionID

    data, err := s.client.Get(ctx, key).Bytes()
    if err == redis.Nil {
        return Session{}, false
    }
    if err != nil {
        log.Error().Err(err).Str("sessionID", sessionID).Msg("redis get failed")
        return Session{}, false
    }

    var session Session
    if err := json.Unmarshal(data, &session); err != nil {
        log.Error().Err(err).Msg("session unmarshal failed")
        return Session{}, false
    }

    // Redis TTL handles expiration, but double-check
    if time.Now().UTC().After(session.ExpiresAt) {
        s.DeleteSession(sessionID)
        return Session{}, false
    }

    return session, true
}

func (s *RedisSessionStore) DeleteSession(sessionID string) bool {
    ctx := context.Background()
    key := "session:" + sessionID

    result := s.client.Del(ctx, key).Val()
    return result > 0
}
```

### 3. Rate Limiter Implementation

**internal/httpapi/ratelimit.go:**
```go
// RateLimiter interface for pluggable backends
type RateLimiter interface {
    Allow(userID string) (allowed bool, remaining int, resetTime time.Time)
}

// RedisRateLimiter implements token bucket using Redis
type RedisRateLimiter struct {
    client     *redis.Client
    config     RateLimitInfo
    refillRate float64
}

func NewRedisRateLimiter(redisURL string, config RateLimitInfo) (*RedisRateLimiter, error) {
    opt, err := redis.ParseURL(redisURL)
    if err != nil {
        return nil, err
    }

    client := redis.NewClient(opt)

    refillRate := float64(config.MaxRequests) / float64(config.WindowSeconds)

    return &RedisRateLimiter{
        client:     client,
        config:     config,
        refillRate: refillRate,
    }, nil
}

func (rl *RedisRateLimiter) Allow(userID string) (bool, int, time.Time) {
    ctx := context.Background()
    key := "ratelimit:" + userID
    now := time.Now()

    // Lua script for atomic token bucket refill + consume
    // This ensures correct behavior even with multiple API pods
    script := redis.NewScript(`
        local key = KEYS[1]
        local capacity = tonumber(ARGV[1])
        local refill_rate = tonumber(ARGV[2])
        local now_ms = tonumber(ARGV[3])

        local bucket = redis.call('HMGET', key, 'tokens', 'last_refill')
        local tokens = tonumber(bucket[1]) or capacity
        local last_refill = tonumber(bucket[2]) or now_ms

        -- Refill tokens based on elapsed time
        local elapsed = (now_ms - last_refill) / 1000.0
        tokens = math.min(capacity, tokens + (elapsed * refill_rate))

        -- Try to consume 1 token
        local allowed = 0
        if tokens >= 1.0 then
            tokens = tokens - 1.0
            allowed = 1
        end

        -- Update bucket
        redis.call('HMSET', key, 'tokens', tokens, 'last_refill', now_ms)
        redis.call('EXPIRE', key, 3600)  -- Auto-expire after 1 hour of inactivity

        return {allowed, math.floor(tokens)}
    `)

    result, err := script.Run(ctx, rl.client,
        []string{key},
        rl.config.Burst,
        rl.refillRate,
        now.UnixMilli(),
    ).Result()

    if err != nil {
        log.Error().Err(err).Str("userID", userID).Msg("redis rate limit check failed")
        // Fail open in case of Redis error
        return true, rl.config.Burst, now.Add(time.Minute)
    }

    results := result.([]interface{})
    allowed := results[0].(int64) == 1
    remaining := int(results[1].(int64))

    // Calculate reset time
    tokensNeeded := float64(rl.config.Burst - remaining)
    resetTime := now.Add(time.Duration(tokensNeeded/rl.refillRate) * time.Second)

    return allowed, remaining, resetTime
}
```

### 4. Configuration

**Environment Variables:**
```bash
REDIS_URL=redis://:password@redis-master:6379/0
SESSION_BACKEND=redis    # or 'memory' for development
RATELIMIT_BACKEND=redis  # or 'memory' for development
```

**chart/templates/deployment.yaml:**
```yaml
- name: REDIS_URL
  valueFrom:
    secretKeyRef:
      name: {{ include "toolbridge-api.secretName" . }}
      key: redis-url
- name: SESSION_BACKEND
  value: "redis"
- name: RATELIMIT_BACKEND
  value: "redis"
```

### 5. Backward Compatibility

Keep in-memory implementations for:
- Local development (no Redis required)
- Testing (simpler setup)
- Single-replica deployments (lower overhead)

```go
// Factory function based on config
func NewSessionStore(backend string, config SessionConfig) (SessionStore, error) {
    switch backend {
    case "redis":
        return NewRedisSessionStore(config.RedisURL, config.TTL)
    case "memory":
        return NewMemorySessionStore(config.TTL), nil
    default:
        return nil, fmt.Errorf("unknown session backend: %s", backend)
    }
}
```

## Testing Strategy

### Unit Tests
- Test Redis implementation with `miniredis` (in-memory Redis mock)
- Verify token bucket algorithm with concurrent requests
- Test session expiration and cleanup

### Integration Tests
- Test with real Redis container
- Verify multi-pod behavior with session sharing
- Load test rate limiting with multiple pods

### E2E Tests
- Deploy with 3 replicas
- Run full sync flow, verify no 428 errors
- Verify rate limits work globally across all pods

## Rollout Plan

### Phase 1: Implementation (1-2 weeks)
- [ ] Add Redis dependency to Helm chart
- [ ] Implement `RedisSessionStore`
- [ ] Implement `RedisRateLimiter`
- [ ] Add configuration and factory functions
- [ ] Write unit tests

### Phase 2: Testing (1 week)
- [ ] Integration tests with Redis
- [ ] Load testing with multiple replicas
- [ ] Verify session sharing works
- [ ] Verify rate limits work globally

### Phase 3: Documentation (3 days)
- [ ] Update DEPLOY.md with Redis setup instructions
- [ ] Document environment variables
- [ ] Add Redis troubleshooting guide
- [ ] Update production checklist

### Phase 4: Rollout (1 week)
- [ ] Deploy to staging with `replicaCount: 3`
- [ ] Monitor for errors
- [ ] Performance testing
- [ ] Deploy to production
- [ ] Update Helm chart default to `replicaCount: 2`

## Monitoring & Observability

Add metrics for:
- Redis connection pool stats
- Session cache hit/miss rates
- Rate limiter decisions (allowed/denied)
- Redis operation latencies

Add alerts for:
- Redis connection failures
- High error rates in session/rate limit operations
- Redis memory usage

## Alternative Approaches Considered

### 1. Session Affinity (Sticky Sessions)
**Pros:** No dependencies, simple
**Cons:** Uneven load distribution, doesn't solve rate limiting, IP changes break sessions
**Decision:** Not sufficient for production

### 2. PostgreSQL for Sessions
**Pros:** Already have Postgres, no new dependency
**Cons:** Too slow for rate limiting (need sub-millisecond operations), adds DB load
**Decision:** Not suitable for high-frequency operations

### 3. Memcached
**Pros:** Simple, fast
**Cons:** No Lua scripts (needed for atomic rate limiting), less feature-rich than Redis
**Decision:** Redis is better fit

## Dependencies

- Go Redis client: `github.com/redis/go-redis/v9`
- Redis Helm chart: `bitnami/redis`
- Testing: `github.com/alicebob/miniredis/v2` (mock Redis)

## Success Metrics

- Zero 428 errors due to session not found (with multi-replica)
- Rate limits enforced globally (sum across all pods)
- P99 latency for session operations < 5ms
- P99 latency for rate limit checks < 3ms
- Support 5+ API replicas without issues

## References

- [Redis Token Bucket Pattern](https://redis.io/glossary/rate-limiting/)
- [Distributed Rate Limiting](https://blog.cloudflare.com/counting-things-a-lot-of-different-things/)
- [Bitnami Redis Helm Chart](https://github.com/bitnami/charts/tree/main/bitnami/redis)
- [go-redis Documentation](https://redis.uptrace.dev/)

## Status

- **Current**: Documented, not implemented
- **Target**: Phase 8 or later
- **Priority**: High (blocks horizontal scaling)
- **Owner**: TBD
