CREATE EXTENSION IF NOT EXISTS pg_trgm;

BEGIN;

DROP TABLE IF EXISTS t_admin_log;
DROP TABLE IF EXISTS t_app;
DROP TABLE IF EXISTS t_admin;
DROP SEQUENCE IF EXISTS t_app_id_seq;

CREATE TABLE t_admin (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(32) NOT NULL UNIQUE,
    nickname VARCHAR(64) NOT NULL DEFAULT '',
    password VARCHAR(255) NOT NULL DEFAULT '',
    disabled BOOLEAN NOT NULL DEFAULT FALSE,
    create_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_update_time TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE SEQUENCE t_app_id_seq START WITH 100001 INCREMENT BY 1;

CREATE TABLE t_app (
    id BIGINT PRIMARY KEY DEFAULT nextval('t_app_id_seq'),
    name VARCHAR(128) NOT NULL,
    secret VARCHAR(255) NOT NULL,
    remark TEXT NOT NULL DEFAULT '',
    disabled BOOLEAN NOT NULL DEFAULT FALSE,
    create_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_update_time TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE t_admin_log (
    id BIGSERIAL PRIMARY KEY,
    admin_id BIGINT NOT NULL DEFAULT 0,
    cate VARCHAR(32) NOT NULL,
    type VARCHAR(32) NOT NULL,
    content JSONB NOT NULL DEFAULT '{}'::JSONB,
    create_time TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_admin_log_cate_type_id ON t_admin_log (cate, type, id DESC);
CREATE INDEX idx_admin_log_admin_id ON t_admin_log (admin_id, id DESC);
CREATE INDEX idx_app_disabled_id ON t_app (disabled, id DESC);
CREATE INDEX idx_app_name_search ON t_app USING gin (name gin_trgm_ops);

COMMIT;
