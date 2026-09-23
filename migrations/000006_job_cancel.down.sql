ALTER TABLE aquila.jobs DROP CONSTRAINT IF EXISTS jobs_status_legal;
ALTER TABLE aquila.jobs ADD CONSTRAINT jobs_status_legal CHECK (status IN ('pending', 'running', 'complete', 'failed'));
