PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  applied_at TEXT NOT NULL
);

CREATE TABLE tenants (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE users (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  email TEXT NOT NULL,
  display_name TEXT NOT NULL,
  password_hash BLOB NOT NULL,
  roles_json TEXT NOT NULL,
  active INTEGER NOT NULL CHECK (active IN (0, 1)),
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (tenant_id, email)
);
CREATE INDEX idx_users_tenant_active ON users(tenant_id, active);

CREATE TABLE auth_sessions (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  user_id TEXT NOT NULL REFERENCES users(id),
  token_hash TEXT NOT NULL UNIQUE,
  state TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  revoked_at TEXT,
  version INTEGER NOT NULL
);
CREATE INDEX idx_sessions_user_state ON auth_sessions(tenant_id, user_id, state);
CREATE INDEX idx_sessions_expiry ON auth_sessions(state, expires_at);

CREATE TABLE works (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  title TEXT NOT NULL,
  author TEXT NOT NULL,
  description TEXT NOT NULL,
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (tenant_id, title, author)
);

CREATE TABLE editions (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  work_id TEXT NOT NULL REFERENCES works(id),
  label TEXT NOT NULL,
  publisher TEXT NOT NULL,
  published_at TEXT NOT NULL,
  page_count INTEGER NOT NULL CHECK (page_count > 0),
  fingerprint TEXT NOT NULL,
  state TEXT NOT NULL,
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (tenant_id, fingerprint)
);
CREATE INDEX idx_editions_work_state ON editions(tenant_id, work_id, state);

CREATE TABLE study_programs (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  edition_id TEXT NOT NULL REFERENCES editions(id),
  coordinator_id TEXT NOT NULL REFERENCES users(id),
  name TEXT NOT NULL,
  timezone TEXT NOT NULL,
  capacity INTEGER NOT NULL CHECK (capacity >= 2),
  starts_at TEXT NOT NULL,
  ends_at TEXT NOT NULL,
  state TEXT NOT NULL,
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (tenant_id, name, starts_at)
);
CREATE INDEX idx_program_state_window ON study_programs(tenant_id, state, starts_at, ends_at);

CREATE TABLE program_members (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  program_id TEXT NOT NULL REFERENCES study_programs(id),
  user_id TEXT NOT NULL REFERENCES users(id),
  state TEXT NOT NULL,
  joined_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  version INTEGER NOT NULL,
  UNIQUE (tenant_id, program_id, user_id)
);
CREATE INDEX idx_members_program_state ON program_members(tenant_id, program_id, state);

CREATE TABLE section_assignments (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  program_id TEXT NOT NULL REFERENCES study_programs(id),
  edition_id TEXT NOT NULL REFERENCES editions(id),
  member_id TEXT NOT NULL REFERENCES program_members(id),
  page_start INTEGER NOT NULL,
  page_end INTEGER NOT NULL,
  window_start TEXT NOT NULL,
  window_end TEXT NOT NULL,
  state TEXT NOT NULL,
  lease_owner TEXT NOT NULL DEFAULT '',
  lease_token INTEGER NOT NULL DEFAULT 0,
  lease_until TEXT,
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  CHECK (page_start > 0 AND page_end >= page_start)
);
CREATE INDEX idx_assignments_conflict ON section_assignments(tenant_id, program_id, edition_id, state, window_start, window_end, page_start, page_end);
CREATE INDEX idx_assignments_expiry ON section_assignments(state, lease_until, window_end);

CREATE TABLE reading_sessions (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  program_id TEXT NOT NULL REFERENCES study_programs(id),
  assignment_id TEXT NOT NULL REFERENCES section_assignments(id),
  member_id TEXT NOT NULL REFERENCES program_members(id),
  state TEXT NOT NULL,
  intention TEXT NOT NULL,
  started_at TEXT,
  submitted_at TEXT,
  accepted_at TEXT,
  abandoned_reason TEXT NOT NULL DEFAULT '',
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (tenant_id, assignment_id)
);
CREATE INDEX idx_reading_program_state ON reading_sessions(tenant_id, program_id, state);

CREATE TABLE observation_notes (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  session_id TEXT NOT NULL REFERENCES reading_sessions(id),
  page INTEGER NOT NULL,
  channel TEXT NOT NULL,
  body TEXT NOT NULL,
  sequence INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE (tenant_id, session_id, sequence)
);

CREATE TABLE quotation_claims (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  session_id TEXT NOT NULL REFERENCES reading_sessions(id),
  edition_id TEXT NOT NULL REFERENCES editions(id),
  author_id TEXT NOT NULL REFERENCES users(id),
  page_start INTEGER NOT NULL,
  page_end INTEGER NOT NULL,
  quoted_text TEXT NOT NULL,
  interpretation TEXT NOT NULL,
  state TEXT NOT NULL,
  current_review_id TEXT NOT NULL DEFAULT '',
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX idx_claims_session_state ON quotation_claims(tenant_id, session_id, state);

CREATE TABLE review_assignments (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  claim_id TEXT NOT NULL REFERENCES quotation_claims(id),
  claim_author_id TEXT NOT NULL REFERENCES users(id),
  reviewer_id TEXT NOT NULL REFERENCES users(id),
  state TEXT NOT NULL,
  lease_owner TEXT NOT NULL DEFAULT '',
  lease_token INTEGER NOT NULL DEFAULT 0,
  lease_until TEXT,
  attempt INTEGER NOT NULL DEFAULT 0,
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (tenant_id, claim_id, reviewer_id)
);
CREATE INDEX idx_reviews_queue ON review_assignments(tenant_id, reviewer_id, state, lease_until);

CREATE TABLE review_decisions (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  assignment_id TEXT NOT NULL REFERENCES review_assignments(id),
  claim_id TEXT NOT NULL REFERENCES quotation_claims(id),
  reviewer_id TEXT NOT NULL REFERENCES users(id),
  outcome TEXT NOT NULL,
  rationale TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE (tenant_id, assignment_id)
);

CREATE TABLE claim_disputes (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  claim_id TEXT NOT NULL REFERENCES quotation_claims(id),
  opened_by TEXT NOT NULL REFERENCES users(id),
  reason TEXT NOT NULL,
  state TEXT NOT NULL,
  resolver_id TEXT NOT NULL DEFAULT '',
  resolution TEXT NOT NULL DEFAULT '',
  version INTEGER NOT NULL,
  opened_at TEXT NOT NULL,
  resolved_at TEXT,
  UNIQUE (tenant_id, claim_id)
);
CREATE INDEX idx_disputes_state ON claim_disputes(tenant_id, state);

CREATE TABLE seminar_agendas (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  program_id TEXT NOT NULL REFERENCES study_programs(id),
  state TEXT NOT NULL,
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (tenant_id, program_id)
);

CREATE TABLE agenda_items (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  agenda_id TEXT NOT NULL REFERENCES seminar_agendas(id),
  claim_id TEXT NOT NULL REFERENCES quotation_claims(id),
  position INTEGER NOT NULL,
  prompt TEXT NOT NULL,
  state TEXT NOT NULL,
  outcome TEXT NOT NULL DEFAULT '',
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (tenant_id, agenda_id, position),
  UNIQUE (tenant_id, agenda_id, claim_id)
);

CREATE TABLE archive_jobs (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  program_id TEXT NOT NULL REFERENCES study_programs(id),
  state TEXT NOT NULL,
  lease_owner TEXT NOT NULL DEFAULT '',
  lease_token INTEGER NOT NULL DEFAULT 0,
  lease_until TEXT,
  attempt INTEGER NOT NULL DEFAULT 0,
  max_attempts INTEGER NOT NULL,
  next_try_at TEXT NOT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  snapshot_hash TEXT NOT NULL DEFAULT '',
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (tenant_id, program_id)
);
CREATE INDEX idx_archive_jobs_due ON archive_jobs(state, next_try_at, lease_until);

CREATE TABLE archive_snapshots (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  program_id TEXT NOT NULL REFERENCES study_programs(id),
  hash TEXT NOT NULL,
  payload BLOB NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE (tenant_id, program_id),
  UNIQUE (tenant_id, hash)
);

CREATE TABLE outbox_events (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  topic TEXT NOT NULL,
  aggregate_id TEXT NOT NULL,
  payload BLOB NOT NULL,
  state TEXT NOT NULL,
  attempt INTEGER NOT NULL DEFAULT 0,
  next_try_at TEXT NOT NULL,
  lease_owner TEXT NOT NULL DEFAULT '',
  lease_token INTEGER NOT NULL DEFAULT 0,
  lease_until TEXT,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX idx_outbox_due ON outbox_events(state, next_try_at, lease_until, created_at);

CREATE VIEW outbox_claim_candidates AS
SELECT id, tenant_id, topic, aggregate_id, payload, state, attempt, next_try_at, lease_owner, lease_token, lease_until, last_error, created_at, updated_at
FROM outbox_events;

CREATE TABLE idempotency_keys (
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  key TEXT NOT NULL,
  method TEXT NOT NULL,
  path TEXT NOT NULL,
  actor_id TEXT NOT NULL,
  request_hash TEXT NOT NULL,
  status_code INTEGER NOT NULL,
  response BLOB NOT NULL,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  PRIMARY KEY (tenant_id, key)
);
CREATE INDEX idx_idempotency_expiry ON idempotency_keys(expires_at);

CREATE TABLE audit_events (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  actor_id TEXT NOT NULL,
  request_id TEXT NOT NULL,
  action TEXT NOT NULL,
  object_type TEXT NOT NULL,
  object_id TEXT NOT NULL,
  result TEXT NOT NULL,
  details BLOB NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX idx_audit_object ON audit_events(tenant_id, object_type, object_id, created_at);
CREATE INDEX idx_audit_request ON audit_events(tenant_id, request_id);
