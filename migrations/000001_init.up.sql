-- Phase 0 establishes migration machinery and the Aquila schema namespace.
-- Domain tables (graph, experiments, tasks, artifacts) land in later phases.

CREATE SCHEMA IF NOT EXISTS aquila;

COMMENT ON SCHEMA aquila IS 'Durable Aquila coordination state and derived system graph.';
