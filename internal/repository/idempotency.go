package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
)

func HashRequest(method, path string, body []byte) string {
	hash := sha256.New()
	hash.Write([]byte(strings.ToUpper(method)))
	hash.Write([]byte{0})
	hash.Write([]byte(path))
	hash.Write([]byte{0})
	hash.Write(body)
	return hex.EncodeToString(hash.Sum(nil))
}

func PersistIdempotency(ctx context.Context, writer Writer, record IdempotencyRecord) error {
	return writer.InsertIdempotency(ctx, record)
}

func NewIdempotencyRecord(tenantID, key, method, path, actorID, requestHash string, status int, response []byte, now time.Time, ttl time.Duration) (IdempotencyRecord, error) {
	if tenantID == "" || strings.TrimSpace(key) == "" || method == "" || path == "" || actorID == "" {
		return IdempotencyRecord{}, fault.Invalid("idempotency", "requires tenant, key, route, and actor")
	}
	if len(requestHash) != 64 {
		return IdempotencyRecord{}, fault.Invalid("request_hash", "must be a SHA-256 value")
	}
	if ttl <= 0 {
		return IdempotencyRecord{}, fault.Invalid("idempotency_ttl", "must be positive")
	}
	now = now.UTC()
	return IdempotencyRecord{
		TenantID: tenantID, Key: strings.TrimSpace(key), Method: strings.ToUpper(method), Path: path,
		ActorID: actorID, RequestHash: requestHash, StatusCode: status, Response: append([]byte(nil), response...),
		CreatedAt: now, ExpiresAt: now.Add(ttl),
	}, nil
}

func (r IdempotencyRecord) Matches(method, path, actorID, requestHash string) bool {
	return r.Method == strings.ToUpper(method) && r.Path == path && r.ActorID == actorID && r.RequestHash == requestHash
}

func (r IdempotencyRecord) Clone() IdempotencyRecord {
	clone := r
	clone.Response = append([]byte(nil), r.Response...)
	return clone
}
