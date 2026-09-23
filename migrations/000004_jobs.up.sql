-- Phase 19: durable experiment DAG. validated is forced false.

CREATE TABLE aquila.jobs (
    id            TEXT PRIMARY KEY,
    created_at    TIMESTAMPTZ NOT NULL,
    status        TEXT NOT NULL,
    baseline      TEXT NOT NULL,
    patch         TEXT NOT NULL,
    baseline_sha  TEXT NOT NULL DEFAULT '',
    dirty         BOOLEAN NOT NULL DEFAULT FALSE,
    validated     BOOLEAN NOT NULL DEFAULT FALSE,
    payload       JSONB NOT NULL,
    CONSTRAINT jobs_validated_false CHECK (validated = FALSE),
    CONSTRAINT jobs_status_legal CHECK (status IN ('pending', 'running', 'complete', 'failed'))
);

CREATE INDEX jobs_created_at_idx ON aquila.jobs (created_at DESC, id DESC);

COMMENT ON TABLE aquila.jobs IS 'Experiment DAG. Operator steps are skipped. Never a pass.';
