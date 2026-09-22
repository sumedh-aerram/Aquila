-- Phase 19: durable experiment run records. This is the job, not a worker queue.
-- validated is forced false; overall cannot be a pass.

CREATE TABLE aquila.runs (
    id               TEXT PRIMARY KEY,
    recorded_at      TIMESTAMPTZ NOT NULL,
    baseline_sha     TEXT NOT NULL DEFAULT '',
    dirty            BOOLEAN NOT NULL DEFAULT FALSE,
    workload_digest  TEXT NOT NULL,
    artifact_digest  TEXT NOT NULL,
    overall          TEXT NOT NULL,
    validated        BOOLEAN NOT NULL DEFAULT FALSE,
    artifact         JSONB NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT runs_validated_false CHECK (validated = FALSE),
    CONSTRAINT runs_overall_legal CHECK (overall IN ('match', 'differ', 'incomplete'))
);

CREATE INDEX runs_recorded_at_idx ON aquila.runs (recorded_at DESC, id DESC);

COMMENT ON TABLE aquila.runs IS 'Executed experiment evidence. Never a pass. Request bodies are not stored.';
