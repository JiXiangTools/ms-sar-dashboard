\set ON_ERROR_STOP on

-- Compatibility bootstrap for psql-based local/E2E environments. Production
-- deployments must use /app/ms-sar-dbtool so every version and checksum is
-- validated before the application is started.
BEGIN;
SELECT pg_advisory_xact_lock(21765190250021954);

CREATE TABLE IF NOT EXISTS schema_migrations (
    version BIGINT PRIMARY KEY,
    name TEXT NOT NULL,
    checksum CHAR(64) NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM schema_migrations
        WHERE version = 1
          AND (name <> 'initial' OR checksum <> '29acffd1a22c56e3bdd7ebc181bdb2f972f34ebe60e797cc4a2162cd0ef83b45')
    ) THEN
        RAISE EXCEPTION 'migration 000001 metadata does not match this build';
    END IF;
END
$$;

SELECT NOT EXISTS (
    SELECT 1 FROM schema_migrations WHERE version = 1
) AS migration_000001_pending \gset

\if :migration_000001_pending
\ir migrations/000001_initial.sql
INSERT INTO schema_migrations (version, name, checksum)
VALUES (1, 'initial', '29acffd1a22c56e3bdd7ebc181bdb2f972f34ebe60e797cc4a2162cd0ef83b45');
\endif

COMMIT;
