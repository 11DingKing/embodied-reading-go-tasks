package repository

import (
	"context"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/audit"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/archive"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/catalog"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/evidence"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/identity"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/program"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/reading"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/review"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/seminar"
)

type Page struct {
	Limit  int
	Offset int
}

func (p Page) Normalize() Page {
	if p.Limit < 1 {
		p.Limit = 25
	}
	if p.Limit > 100 {
		p.Limit = 100
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	return p
}

type IdempotencyRecord struct {
	TenantID    string
	Key         string
	Method      string
	Path        string
	ActorID     string
	RequestHash string
	StatusCode  int
	Response    []byte
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

type OutboxEvent struct {
	ID          string
	TenantID    string
	Topic       string
	AggregateID string
	Payload     []byte
	State       string
	Attempt     int
	NextTryAt   time.Time
	LeaseOwner  string
	LeaseToken  int64
	LeaseUntil  time.Time
	LastError   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ArchiveSnapshot struct {
	ID        string
	TenantID  string
	ProgramID string
	Hash      string
	Payload   []byte
	CreatedAt time.Time
}

type ReviewQueueFilter struct {
	TenantID   string
	ReviewerID string
	States     []review.AssignmentState
	DueBefore  *time.Time
	Page       Page
}

type ReviewQueuePage struct {
	Items []review.Assignment
	Total int
}

type ProgramReadiness struct {
	ActiveMembers       int
	IncompleteMembers   int
	ActiveAssignments   int
	OpenReadingSessions int
	UnresolvedClaims    int
	OpenDisputes        int
	UnresolvedAgenda    int
	ActiveWorkerLeases  int
}

func (r ProgramReadiness) ReadyForReview() bool {
	return r.ActiveMembers > 0 && r.IncompleteMembers == 0 && r.ActiveAssignments == 0 && r.OpenReadingSessions == 0
}

func (r ProgramReadiness) ReadyForArchive() bool {
	return r.ReadyForReview() && r.UnresolvedClaims == 0 && r.OpenDisputes == 0 && r.UnresolvedAgenda == 0 && r.ActiveWorkerLeases == 0
}

type Reader interface {
	GetUser(ctx context.Context, tenantID, id string) (identity.User, error)
	GetUserByEmail(ctx context.Context, tenantID, email string) (identity.User, error)
	GetSessionByTokenHash(ctx context.Context, tokenHash string) (identity.Session, error)
	GetWork(ctx context.Context, tenantID, id string) (catalog.Work, error)
	GetEdition(ctx context.Context, tenantID, id string) (catalog.Edition, error)
	GetProgram(ctx context.Context, tenantID, id string) (program.Program, error)
	GetMember(ctx context.Context, tenantID, programID, userID string) (program.Member, error)
	CountActiveMembers(ctx context.Context, tenantID, programID string) (int, error)
	GetAssignment(ctx context.Context, tenantID, id string) (reading.Assignment, error)
	FindAssignmentConflicts(ctx context.Context, tenantID, programID, editionID string, pages reading.PageRange, start, end time.Time, excludeID string) ([]reading.Assignment, error)
	ListExpiredAssignments(ctx context.Context, now time.Time, page Page) ([]reading.Assignment, error)
	GetReadingSession(ctx context.Context, tenantID, id string) (reading.Session, error)
	ListObservations(ctx context.Context, tenantID, sessionID string) ([]reading.Observation, error)
	ListClaimsBySession(ctx context.Context, tenantID, sessionID string) ([]evidence.Claim, error)
	GetClaim(ctx context.Context, tenantID, id string) (evidence.Claim, error)
	GetDispute(ctx context.Context, tenantID, id string) (evidence.Dispute, error)
	CountOpenDisputes(ctx context.Context, tenantID, programID string) (int, error)
	GetProgramReadiness(ctx context.Context, tenantID, programID string, now time.Time) (ProgramReadiness, error)
	ListFinalClaimsForProgram(ctx context.Context, tenantID, programID string) ([]evidence.Claim, error)
	GetReviewAssignment(ctx context.Context, tenantID, id string) (review.Assignment, error)
	ListReviewQueue(ctx context.Context, filter ReviewQueueFilter) (ReviewQueuePage, error)
	GetAgenda(ctx context.Context, tenantID, id string) (seminar.Agenda, error)
	ListAgendaItems(ctx context.Context, tenantID, agendaID string) ([]seminar.Item, error)
	GetArchiveJob(ctx context.Context, tenantID, id string) (archive.Job, error)
	GetArchiveSnapshot(ctx context.Context, tenantID, programID string) (ArchiveSnapshot, error)
	GetIdempotency(ctx context.Context, tenantID, key, method, path, actorID string) (IdempotencyRecord, error)
}

type Writer interface {
	EnsureTenant(ctx context.Context, tenantID, name string, now time.Time) error
	InsertUser(ctx context.Context, user identity.User) error
	UpdateUser(ctx context.Context, user identity.User, expectedVersion int64) error
	InsertSession(ctx context.Context, session identity.Session) error
	UpdateSession(ctx context.Context, session identity.Session, expectedVersion int64) error
	RevokeSessionsForUser(ctx context.Context, tenantID, userID string, now time.Time) (int, error)
	InsertWork(ctx context.Context, work catalog.Work) error
	UpdateWork(ctx context.Context, work catalog.Work, expectedVersion int64) error
	InsertEdition(ctx context.Context, edition catalog.Edition) error
	UpdateEdition(ctx context.Context, edition catalog.Edition, expectedVersion int64) error
	InsertProgram(ctx context.Context, value program.Program) error
	UpdateProgram(ctx context.Context, value program.Program, expectedVersion int64) error
	InsertMember(ctx context.Context, member program.Member) error
	UpdateMember(ctx context.Context, member program.Member, expectedVersion int64) error
	InsertAssignment(ctx context.Context, assignment reading.Assignment) error
	UpdateAssignment(ctx context.Context, assignment reading.Assignment, expectedVersion int64) error
	InsertReadingSession(ctx context.Context, session reading.Session) error
	UpdateReadingSession(ctx context.Context, session reading.Session, expectedVersion int64) error
	InsertObservation(ctx context.Context, observation reading.Observation) error
	InsertClaim(ctx context.Context, claim evidence.Claim) error
	UpdateClaim(ctx context.Context, claim evidence.Claim, expectedVersion int64) error
	InsertDispute(ctx context.Context, dispute evidence.Dispute) error
	UpdateDispute(ctx context.Context, dispute evidence.Dispute, expectedVersion int64) error
	InsertReviewAssignment(ctx context.Context, assignment review.Assignment) error
	UpdateReviewAssignment(ctx context.Context, assignment review.Assignment, expectedVersion int64) error
	InsertReviewDecision(ctx context.Context, decision review.Decision) error
	InsertAgenda(ctx context.Context, agenda seminar.Agenda) error
	UpdateAgenda(ctx context.Context, agenda seminar.Agenda, expectedVersion int64) error
	InsertAgendaItem(ctx context.Context, item seminar.Item) error
	UpdateAgendaItem(ctx context.Context, item seminar.Item, expectedVersion int64) error
	InsertArchiveJob(ctx context.Context, job archive.Job) error
	UpdateArchiveJob(ctx context.Context, job archive.Job, expectedVersion int64) error
	InsertArchiveSnapshot(ctx context.Context, snapshot ArchiveSnapshot) error
	InsertAudit(ctx context.Context, event audit.Event) error
	InsertOutbox(ctx context.Context, event OutboxEvent) error
	InsertIdempotency(ctx context.Context, record IdempotencyRecord) error
}

type Store interface {
	Reader
	Writer
	WithinTx(ctx context.Context, fn func(context.Context, Tx) error) error
	Ping(ctx context.Context) error
	Close() error
	ClaimOutbox(ctx context.Context, owner string, now time.Time, ttl time.Duration) (OutboxEvent, error)
	CompleteOutbox(ctx context.Context, id, owner string, token int64, now time.Time) error
	FailOutbox(ctx context.Context, id, owner string, token int64, cause error, nextTry time.Time, maxAttempts int, now time.Time) error
}

type Tx interface {
	Reader
	Writer
}
