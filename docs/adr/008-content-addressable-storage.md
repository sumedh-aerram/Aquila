# ADR-008: Local SHA-256 object store

Date: 2026-10-18

Status: Accepted; implemented as a local store used by the worker action cache; no remote backend

## Context

Retry results need integrity checks and immutable object identity.

## Decision

Hash bytes to local object paths and verify reads (`internal/cas/cas.go:Put`, `internal/cas/cas.go:Get`); map action digests separately (`internal/action/cache.go`).

## Consequences

The implementation is directory-backed; remote object stores and an artifact protocol are not present. Cacheable kinds are only behavior and tests. `TestCorruptRefused` covers digest mismatch.
