# ADR-016: CI test, evidence, and vulnerability gates

Date: 2026-10-18

Status: Accepted+Implemented

## Context

A green job can hide skipped integration work or a smoke run that never validated.

## Decision

Run format, tidy, lint, vulnerability, guarded tests, Docker pair/sandbox, evaluations, and positive/negative end-to-end assertions in `.github/workflows/ci.yml`. `.github/scripts/check-test-skips.py` rejects unexpected skips and incomplete Go test JSON streams.

## Consequences

The allowlist in `.github/test-skip-allowlist.txt` applies only to the general Go job; integration has no skip allowance. Positive and negative evidence are checked with the jq scripts. Current guard does not require named test passes or detect unused allowlist entries. Hosted results are separate from local checks.
