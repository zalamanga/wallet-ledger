CREATE TABLE workspaces (
    id uuid PRIMARY KEY,
    singleton boolean NOT NULL DEFAULT true UNIQUE CHECK (singleton),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE users (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    email text NOT NULL UNIQUE CHECK (email = lower(email) AND length(email) BETWEEN 3 AND 254),
    full_name text NOT NULL CHECK (char_length(full_name) BETWEEN 1 AND 120),
    role text NOT NULL CHECK (role IN ('Admin','Member')),
    password_hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE UNIQUE INDEX one_administrator ON users (role) WHERE role='Admin';
CREATE TABLE wallets (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    user_id uuid UNIQUE REFERENCES users(id),
    is_system boolean NOT NULL DEFAULT false,
    currency text NOT NULL DEFAULT 'IDR' CHECK (currency='IDR'),
    balance_minor bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK ((is_system AND user_id IS NULL AND balance_minor BETWEEN -100000000000000 AND 0)
       OR (NOT is_system AND user_id IS NOT NULL AND balance_minor BETWEEN 0 AND 1000000000000))
);
CREATE UNIQUE INDEX one_funding_wallet ON wallets(workspace_id) WHERE is_system;
CREATE TABLE sessions (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id),
    token_hash text NOT NULL UNIQUE CHECK (length(token_hash)=64),
    csrf_token text NOT NULL CHECK (length(csrf_token)=43),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL
);
CREATE INDEX session_expiration ON sessions(expires_at);
CREATE TABLE login_attempts (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    normalized_email text NOT NULL,
    peer_ip text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX login_email_window ON login_attempts(normalized_email,created_at);
CREATE INDEX login_peer_window ON login_attempts(peer_ip,created_at);
CREATE TABLE transactions (
    id uuid PRIMARY KEY,
    seq bigint GENERATED ALWAYS AS IDENTITY UNIQUE NOT NULL,
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    kind text NOT NULL CHECK (kind IN ('TRANSFER','ALLOCATION','REVERSAL')),
    amount_minor bigint NOT NULL CHECK (amount_minor BETWEEN 1 AND 1000000000),
    note text NOT NULL CHECK (char_length(note) BETWEEN 1 AND 500),
    created_by uuid NOT NULL REFERENCES users(id),
    from_wallet_id uuid NOT NULL REFERENCES wallets(id),
    to_wallet_id uuid NOT NULL REFERENCES wallets(id),
    request_key uuid NOT NULL,
    reversal_of uuid UNIQUE REFERENCES transactions(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (from_wallet_id<>to_wallet_id),
    CHECK ((kind='REVERSAL') = (reversal_of IS NOT NULL)),
    CHECK (kind<>'REVERSAL' OR char_length(note)>=10),
    UNIQUE(created_by,request_key)
);
CREATE INDEX transactions_history ON transactions(seq DESC);
CREATE INDEX transactions_from_history ON transactions(from_wallet_id,seq DESC);
CREATE INDEX transactions_to_history ON transactions(to_wallet_id,seq DESC);
CREATE TABLE journal_entries (
    id uuid PRIMARY KEY,
    transaction_id uuid NOT NULL REFERENCES transactions(id),
    wallet_id uuid NOT NULL REFERENCES wallets(id),
    direction text NOT NULL CHECK (direction IN ('DEBIT','CREDIT')),
    amount_minor bigint NOT NULL CHECK (amount_minor BETWEEN 1 AND 1000000000),
    UNIQUE(transaction_id,direction),
    UNIQUE(transaction_id,wallet_id)
);
CREATE INDEX journal_wallet ON journal_entries(wallet_id);
CREATE TABLE receipts (
    actor_id uuid NOT NULL REFERENCES users(id),
    request_key uuid NOT NULL,
    fingerprint text NOT NULL CHECK (length(fingerprint)=64),
    operation text NOT NULL CHECK (operation IN ('USER_CREATED','TRANSFER','ALLOCATION','REVERSAL')),
    status integer NOT NULL CHECK (status IN (201,409)),
    response jsonb NOT NULL CHECK (jsonb_typeof(response)='object'),
    entity_id uuid,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(actor_id,request_key),
    CHECK ((status=201)=(entity_id IS NOT NULL)),
    CHECK (status<>409 OR response->>'code' IN ('INSUFFICIENT_FUNDS','LIMIT_EXCEEDED','ALREADY_REVERSED'))
);
CREATE UNIQUE INDEX one_success_receipt ON receipts(entity_id) WHERE status=201;
CREATE TABLE audit_events (
    id uuid PRIMARY KEY,
    seq bigint GENERATED ALWAYS AS IDENTITY UNIQUE NOT NULL,
    actor_id uuid NOT NULL REFERENCES users(id),
    kind text NOT NULL CHECK (kind IN ('USER_CREATED','TRANSFER','ALLOCATION','REVERSAL')),
    entity_id uuid NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE FUNCTION reject_history_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'immutable history: %', TG_TABLE_NAME USING ERRCODE='23514';
END $$;

DO $$ DECLARE tab text; BEGIN
    FOREACH tab IN ARRAY ARRAY['transactions','journal_entries','receipts','audit_events','users','workspaces'] LOOP
        EXECUTE format('CREATE TRIGGER immutable_rows BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION reject_history_mutation()',tab);
        EXECUTE format('CREATE TRIGGER immutable_truncate BEFORE TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION reject_history_mutation()',tab);
    END LOOP;
END $$;
CREATE TRIGGER wallet_no_delete BEFORE DELETE ON wallets FOR EACH ROW EXECUTE FUNCTION reject_history_mutation();
CREATE TRIGGER wallet_no_truncate BEFORE TRUNCATE ON wallets FOR EACH STATEMENT EXECUTE FUNCTION reject_history_mutation();
CREATE FUNCTION wallet_identity_unchanged() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id<>OLD.id OR NEW.workspace_id<>OLD.workspace_id OR NEW.user_id IS DISTINCT FROM OLD.user_id
       OR NEW.is_system<>OLD.is_system OR NEW.currency<>OLD.currency OR NEW.created_at<>OLD.created_at THEN
        RAISE EXCEPTION 'wallet identity is immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER wallet_identity BEFORE UPDATE ON wallets FOR EACH ROW EXECUTE FUNCTION wallet_identity_unchanged();

CREATE FUNCTION validate_financial_transaction(target uuid) RETURNS void LANGUAGE plpgsql AS $$
DECLARE t transactions%ROWTYPE; source wallets%ROWTYPE; destination wallets%ROWTYPE;
        original transactions%ROWTYPE; actor users%ROWTYPE; legs integer; debit numeric; credit numeric;
BEGIN
    SELECT * INTO t FROM transactions WHERE id=target;
    IF NOT FOUND THEN RETURN; END IF;
    SELECT * INTO source FROM wallets WHERE id=t.from_wallet_id;
    SELECT * INTO destination FROM wallets WHERE id=t.to_wallet_id;
    SELECT * INTO actor FROM users WHERE id=t.created_by;
    IF source.workspace_id<>t.workspace_id OR destination.workspace_id<>t.workspace_id OR actor.workspace_id<>t.workspace_id THEN
        RAISE EXCEPTION 'transaction workspace mismatch' USING ERRCODE='23514';
    END IF;
    SELECT count(*), coalesce(sum(amount_minor) FILTER (WHERE direction='DEBIT'),0),
        coalesce(sum(amount_minor) FILTER (WHERE direction='CREDIT'),0)
        INTO legs,debit,credit FROM journal_entries WHERE transaction_id=target;
    IF legs<>2 OR debit<>t.amount_minor OR credit<>t.amount_minor
       OR NOT EXISTS(SELECT 1 FROM journal_entries WHERE transaction_id=target AND wallet_id=t.from_wallet_id AND direction='DEBIT')
       OR NOT EXISTS(SELECT 1 FROM journal_entries WHERE transaction_id=target AND wallet_id=t.to_wallet_id AND direction='CREDIT') THEN
        RAISE EXCEPTION 'journal must contain the matching balanced pair' USING ERRCODE='23514';
    END IF;
    IF t.kind='TRANSFER' AND (source.is_system OR destination.is_system OR source.user_id<>t.created_by) THEN
        RAISE EXCEPTION 'transfer ownership mismatch' USING ERRCODE='23514';
    END IF;
    IF t.kind='ALLOCATION' AND (NOT source.is_system OR destination.is_system OR actor.role<>'Admin') THEN
        RAISE EXCEPTION 'allocation shape mismatch' USING ERRCODE='23514';
    END IF;
    IF t.kind='REVERSAL' THEN
        SELECT * INTO original FROM transactions WHERE id=t.reversal_of;
        IF original.kind IS DISTINCT FROM 'TRANSFER' OR actor.role<>'Admin' OR t.amount_minor<>original.amount_minor
           OR t.from_wallet_id<>original.to_wallet_id OR t.to_wallet_id<>original.from_wallet_id THEN
            RAISE EXCEPTION 'reversal must mirror one transfer' USING ERRCODE='23514';
        END IF;
    END IF;
    IF NOT EXISTS(SELECT 1 FROM receipts r WHERE r.actor_id=t.created_by AND r.request_key=t.request_key
       AND r.status=201 AND r.entity_id=t.id AND r.operation=t.kind)
       OR NOT EXISTS(SELECT 1 FROM audit_events a WHERE a.entity_id=t.id AND a.actor_id=t.created_by AND a.kind=t.kind) THEN
        RAISE EXCEPTION 'transaction requires one receipt and audit event' USING ERRCODE='23514';
    END IF;
END $$;

CREATE FUNCTION check_transaction_trigger() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME='journal_entries' THEN PERFORM validate_financial_transaction(NEW.transaction_id);
    ELSE PERFORM validate_financial_transaction(NEW.id); END IF;
    RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER balanced_transaction AFTER INSERT ON transactions DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION check_transaction_trigger();
CREATE CONSTRAINT TRIGGER balanced_journal AFTER INSERT ON journal_entries DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION check_transaction_trigger();

CREATE FUNCTION validate_wallet_balance(target uuid) RETURNS void LANGUAGE plpgsql AS $$
DECLARE w wallets%ROWTYPE; ledger numeric; owner_workspace uuid;
BEGIN
    SELECT * INTO w FROM wallets WHERE id=target;
    IF NOT FOUND THEN RETURN; END IF;
    SELECT coalesce(sum(CASE WHEN direction='CREDIT' THEN amount_minor ELSE -amount_minor END),0)
       INTO ledger FROM journal_entries WHERE wallet_id=target;
    IF ledger<>w.balance_minor THEN RAISE EXCEPTION 'wallet cache does not match journal' USING ERRCODE='23514'; END IF;
    IF NOT w.is_system THEN
        SELECT workspace_id INTO owner_workspace FROM users WHERE id=w.user_id;
        IF owner_workspace IS DISTINCT FROM w.workspace_id THEN RAISE EXCEPTION 'wallet owner workspace mismatch' USING ERRCODE='23514'; END IF;
    END IF;
END $$;
CREATE FUNCTION check_wallet_trigger() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME='journal_entries' THEN PERFORM validate_wallet_balance(NEW.wallet_id);
    ELSE PERFORM validate_wallet_balance(NEW.id); END IF;
    RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER wallet_cache AFTER INSERT OR UPDATE ON wallets DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION check_wallet_trigger();
CREATE CONSTRAINT TRIGGER journal_cache AFTER INSERT ON journal_entries DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION check_wallet_trigger();

CREATE FUNCTION check_mutation_evidence() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target uuid; operation_name text; actor uuid; evidence receipts%ROWTYPE;
BEGIN
    IF TG_TABLE_NAME='receipts' THEN
        IF NEW.status=409 THEN RETURN NULL; END IF;
        target:=NEW.entity_id; operation_name:=NEW.operation; actor:=NEW.actor_id;
    ELSIF TG_TABLE_NAME='audit_events' THEN
        target:=NEW.entity_id; operation_name:=NEW.kind; actor:=NEW.actor_id;
    ELSE
        IF NEW.role='Admin' THEN RETURN NULL; END IF;
        target:=NEW.id; operation_name:='USER_CREATED';
        SELECT actor_id INTO actor FROM receipts WHERE entity_id=target AND status=201;
    END IF;
    SELECT * INTO evidence FROM receipts WHERE entity_id=target AND status=201;
    IF NOT FOUND OR evidence.operation IS DISTINCT FROM operation_name OR evidence.actor_id IS DISTINCT FROM actor
       OR NOT EXISTS(SELECT 1 FROM audit_events WHERE entity_id=target AND kind=operation_name AND actor_id=actor) THEN
        RAISE EXCEPTION 'mutation requires matching immutable receipt and audit' USING ERRCODE='23514';
    END IF;
    IF operation_name='USER_CREATED' THEN
        IF NOT EXISTS(SELECT 1 FROM users WHERE id=actor AND role='Admin')
           OR NOT EXISTS(SELECT 1 FROM users WHERE id=target AND role='Member') THEN
            RAISE EXCEPTION 'account evidence mismatch' USING ERRCODE='23514';
        END IF;
    ELSE
        IF NOT EXISTS(SELECT 1 FROM transactions WHERE id=target AND created_by=actor AND kind=operation_name AND request_key=evidence.request_key) THEN
            RAISE EXCEPTION 'transaction evidence mismatch' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER receipt_evidence AFTER INSERT ON receipts DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION check_mutation_evidence();
CREATE CONSTRAINT TRIGGER audit_evidence AFTER INSERT ON audit_events DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION check_mutation_evidence();
CREATE CONSTRAINT TRIGGER account_evidence AFTER INSERT ON users DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION check_mutation_evidence();

CREATE FUNCTION check_user_wallet() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM wallets WHERE user_id=NEW.id AND workspace_id=NEW.workspace_id AND NOT is_system) THEN
        RAISE EXCEPTION 'user requires one personal wallet' USING ERRCODE='23514';
    END IF;
    RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER user_wallet AFTER INSERT ON users DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION check_user_wallet();
