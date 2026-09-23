ALTER TABLE aquila.runs DROP CONSTRAINT IF EXISTS runs_validated_requires_match;
ALTER TABLE aquila.runs ADD CONSTRAINT runs_validated_false CHECK (validated = FALSE);

ALTER TABLE aquila.jobs DROP CONSTRAINT IF EXISTS jobs_status_legal;
ALTER TABLE aquila.jobs ADD CONSTRAINT jobs_status_legal CHECK (status IN ('pending', 'running', 'complete', 'failed'));
ALTER TABLE aquila.jobs ADD CONSTRAINT jobs_validated_false CHECK (validated = FALSE);
