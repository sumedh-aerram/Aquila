CREATE TABLE aquila.spans (
    trace_id       TEXT NOT NULL,
    span_id        TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    service_name   TEXT NOT NULL,
    span_name      TEXT NOT NULL,
    span_kind      TEXT NOT NULL DEFAULT '',
    status_code    TEXT NOT NULL DEFAULT '',
    http_method    TEXT NOT NULL DEFAULT '',
    http_route     TEXT NOT NULL DEFAULT '',
    http_status    INTEGER,
    code_function  TEXT NOT NULL DEFAULT '',
    code_file      TEXT NOT NULL DEFAULT '',
    start_time     TIMESTAMPTZ NOT NULL,
    duration_ns    BIGINT NOT NULL,
    received_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (trace_id, span_id)
);

CREATE INDEX spans_service_start_idx ON aquila.spans (service_name, start_time DESC);
CREATE INDEX spans_start_time_idx ON aquila.spans (start_time DESC);

COMMENT ON TABLE aquila.spans IS 'Normalized OTLP span metadata. Bodies and high-cardinality attributes are not stored.';
