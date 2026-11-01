#!/bin/sh
set -eu
: "${POSTGRES_USER:?Migration owner required}"
: "${POSTGRES_DB:?Database required}"
: "${APP_DB_PASSWORD:?Runtime password required}"
psql --no-psqlrc --set=ON_ERROR_STOP=1 --username="$POSTGRES_USER" --dbname="$POSTGRES_DB" --set=runtime_password="$APP_DB_PASSWORD" <<'SQL'
SELECT 'CREATE ROLE walletledger_runtime LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS'
WHERE NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='walletledger_runtime') \gexec
ALTER ROLE walletledger_runtime PASSWORD :'runtime_password';
ALTER ROLE walletledger_runtime NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
SELECT format('REVOKE ALL ON DATABASE %I FROM PUBLIC', current_database()) \gexec
SELECT format('GRANT CONNECT ON DATABASE %I TO walletledger_runtime', current_database()) \gexec
SELECT format('REVOKE CREATE,TEMPORARY ON DATABASE %I FROM walletledger_runtime', current_database()) \gexec
REVOKE CREATE ON SCHEMA public FROM PUBLIC, walletledger_runtime;
GRANT USAGE ON SCHEMA public TO walletledger_runtime;
GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA public TO walletledger_runtime;
GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA public TO walletledger_runtime;
REVOKE INSERT,UPDATE,DELETE ON migration_versions FROM walletledger_runtime;
SQL
