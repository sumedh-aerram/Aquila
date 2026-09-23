-- Allow earned validation when overall is match. False remains the default.

ALTER TABLE aquila.runs DROP CONSTRAINT IF EXISTS runs_validated_false;
ALTER TABLE aquila.runs ADD CONSTRAINT runs_validated_requires_match CHECK (NOT validated OR overall = 'match');

ALTER TABLE aquila.jobs DROP CONSTRAINT IF EXISTS jobs_validated_false;
ALTER TABLE aquila.jobs DROP CONSTRAINT IF EXISTS jobs_status_legal;
ALTER TABLE aquila.jobs ADD CONSTRAINT jobs_status_legal CHECK (status IN ('pending', 'running', 'complete', 'failed', 'canceled'));

COMMENT ON TABLE aquila.runs IS 'Executed experiment evidence. validated is earned only on match after required steps run.';
COMMENT ON TABLE aquila.jobs IS 'Experiment DAG. validated is earned after required executable tasks succeed.';
