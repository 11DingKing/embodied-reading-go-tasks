package archive_test

import (
	"errors"
	"testing"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/archive"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
)

var archiveNow = time.Date(2026, 8, 22, 13, 0, 0, 0, time.UTC)

func job(t *testing.T, maxAttempts int) archive.Job {
	t.Helper()
	value, err := archive.NewJob("job", "tenant", "program", maxAttempts, archiveNow)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestArchiveJobSuccessfulLifecycle(t *testing.T) {
	value := job(t, 3)
	if err := value.Claim("worker", time.Minute, archiveNow); err != nil {
		t.Fatal(err)
	}
	token := value.LeaseToken
	if err := value.BeginWrite("worker", token, archiveNow.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := value.Succeed("worker", token, "0123456789abcdef0123456789abcdef", archiveNow.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if value.State != archive.Complete || value.SnapshotHash == "" || value.LeaseOwner != "" {
		t.Fatalf("completed job = %+v", value)
	}
}

func TestArchiveJobRetryAndPermanentFailure(t *testing.T) {
	value := job(t, 2)
	if err := value.Claim("worker-a", time.Minute, archiveNow); err != nil {
		t.Fatal(err)
	}
	if err := value.Fail("worker-a", value.LeaseToken, errors.New("temporary disk pressure"), time.Minute, archiveNow.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if value.State != archive.RetryWait || !value.NextTryAt.Equal(archiveNow.Add(time.Minute+time.Second)) {
		t.Fatalf("retry job = %+v", value)
	}
	if err := value.Claim("worker-b", time.Minute, archiveNow.Add(30*time.Second)); !fault.IsKind(err, fault.Precondition) {
		t.Fatalf("early retry claim error = %v", err)
	}
	if err := value.Claim("worker-b", time.Minute, value.NextTryAt); err != nil {
		t.Fatal(err)
	}
	if err := value.Fail("worker-b", value.LeaseToken, errors.New("disk permanently unavailable"), time.Minute, value.NextTryAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if value.State != archive.PermanentFailed || value.Attempt != 2 {
		t.Fatalf("permanent failure = %+v", value)
	}
}

func TestArchiveJobFencesStaleWorkers(t *testing.T) {
	value := job(t, 3)
	if err := value.Claim("worker-a", time.Second, archiveNow); err != nil {
		t.Fatal(err)
	}
	oldToken := value.LeaseToken
	if err := value.Claim("worker-b", time.Minute, archiveNow.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := value.BeginWrite("worker-a", oldToken, archiveNow.Add(2*time.Second)); !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("stale begin error = %v", err)
	}
	if err := value.Succeed("worker-a", oldToken, "0123456789abcdef", archiveNow.Add(2*time.Second)); !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("stale success error = %v", err)
	}
}

func TestArchiveJobRejectsInvalidInputs(t *testing.T) {
	if _, err := archive.NewJob("", "tenant", "program", 1, archiveNow); !fault.IsKind(err, fault.Validation) {
		t.Fatalf("missing id error = %v", err)
	}
	if _, err := archive.NewJob("job", "tenant", "program", 0, archiveNow); !fault.IsKind(err, fault.Validation) {
		t.Fatalf("zero attempts error = %v", err)
	}
	value := job(t, 2)
	if err := value.Claim("", time.Minute, archiveNow); !fault.IsKind(err, fault.Validation) {
		t.Fatalf("empty owner error = %v", err)
	}
	if err := value.Claim("worker", 0, archiveNow); !fault.IsKind(err, fault.Validation) {
		t.Fatalf("zero ttl error = %v", err)
	}
	if err := value.Claim("worker", time.Minute, archiveNow); err != nil {
		t.Fatal(err)
	}
	if err := value.BeginWrite("worker", value.LeaseToken, archiveNow); err != nil {
		t.Fatal(err)
	}
	if err := value.Succeed("worker", value.LeaseToken, "short", archiveNow); !fault.IsKind(err, fault.Validation) {
		t.Fatalf("short hash error = %v", err)
	}
	if err := value.Fail("worker", value.LeaseToken, nil, time.Minute, archiveNow); !fault.IsKind(err, fault.Validation) {
		t.Fatalf("nil failure error = %v", err)
	}
}
