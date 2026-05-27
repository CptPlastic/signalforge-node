package database

func (d *DB) migrate() error {
	_, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id          TEXT PRIMARY KEY,
			email       TEXT    NOT NULL UNIQUE,
			role        TEXT    NOT NULL DEFAULT 'user',
			status      TEXT    NOT NULL DEFAULT 'active',
			created_at  BIGINT  NOT NULL,
			updated_at  BIGINT  NOT NULL
		);
		CREATE TABLE IF NOT EXISTS auth_magic_links (
			token       TEXT PRIMARY KEY,
			user_id     TEXT    NOT NULL,
			email       TEXT    NOT NULL,
			expires_at  BIGINT  NOT NULL,
			used_at     BIGINT  NOT NULL DEFAULT 0,
			created_at  BIGINT  NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(id)
		);
		CREATE TABLE IF NOT EXISTS auth_sessions (
			token       TEXT PRIMARY KEY,
			user_id     TEXT    NOT NULL,
			expires_at  BIGINT  NOT NULL,
			revoked_at  BIGINT  NOT NULL DEFAULT 0,
			created_at  BIGINT  NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(id)
		);
		CREATE TABLE IF NOT EXISTS audit_log (
			id           BIGSERIAL PRIMARY KEY,
			user_id      TEXT,
			action       TEXT    NOT NULL,
			target_type  TEXT    NOT NULL,
			target_id    TEXT    NOT NULL,
			metadata     JSONB   NOT NULL DEFAULT '{}'::jsonb,
			created_at   BIGINT  NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(id)
		);
		CREATE INDEX IF NOT EXISTS idx_auth_magic_links_email ON auth_magic_links(email);
		CREATE INDEX IF NOT EXISTS idx_auth_sessions_user_id ON auth_sessions(user_id);
		CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
		CREATE INDEX IF NOT EXISTS idx_audit_log_user_created ON audit_log(user_id, created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_audit_log_action_created ON audit_log(action, created_at DESC);
		CREATE TABLE IF NOT EXISTS hub_identity (
			id                          TEXT PRIMARY KEY DEFAULT 'local',
			hub_id                      TEXT NOT NULL UNIQUE,
			name                        TEXT NOT NULL DEFAULT '',
			public_url                  TEXT NOT NULL DEFAULT '',
			region                      TEXT NOT NULL DEFAULT '',
			contact                     TEXT NOT NULL DEFAULT '',
			public_key                  TEXT NOT NULL DEFAULT '',
			federation_enabled          BOOLEAN NOT NULL DEFAULT FALSE,
			directory_validation_status TEXT NOT NULL DEFAULT 'unverified',
			created_at                  BIGINT NOT NULL,
			updated_at                  BIGINT NOT NULL
		);
		ALTER TABLE hub_identity ADD COLUMN IF NOT EXISTS trust_level TEXT NOT NULL DEFAULT 'community';
		ALTER TABLE hub_identity ADD COLUMN IF NOT EXISTS trust_issuer_hub_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE hub_identity ADD COLUMN IF NOT EXISTS trust_certificate TEXT NOT NULL DEFAULT '';
		ALTER TABLE hub_identity ADD COLUMN IF NOT EXISTS trust_expires_at BIGINT NOT NULL DEFAULT 0;
		ALTER TABLE hub_identity ADD COLUMN IF NOT EXISTS trust_verified_at BIGINT NOT NULL DEFAULT 0;
		ALTER TABLE hub_identity ADD COLUMN IF NOT EXISTS private_key TEXT NOT NULL DEFAULT '';
		CREATE TABLE IF NOT EXISTS hub_invites (
			id                 TEXT PRIMARY KEY,
			token              TEXT NOT NULL UNIQUE,
			created_by_user_id TEXT,
			expires_at         BIGINT NOT NULL,
			used_at            BIGINT NOT NULL DEFAULT 0,
			revoked_at         BIGINT NOT NULL DEFAULT 0,
			created_at         BIGINT NOT NULL,
			FOREIGN KEY (created_by_user_id) REFERENCES users(id)
		);
		CREATE INDEX IF NOT EXISTS idx_hub_invites_expires_at ON hub_invites(expires_at);
		CREATE TABLE IF NOT EXISTS hub_peers (
			id           TEXT PRIMARY KEY,
			hub_id       TEXT NOT NULL UNIQUE,
			name         TEXT NOT NULL DEFAULT '',
			public_url   TEXT NOT NULL DEFAULT '',
			region       TEXT NOT NULL DEFAULT '',
			contact      TEXT NOT NULL DEFAULT '',
			status       TEXT NOT NULL DEFAULT 'connected',
			direction    TEXT NOT NULL DEFAULT 'outbound',
			accepted_at  BIGINT NOT NULL DEFAULT 0,
			last_seen_at BIGINT NOT NULL DEFAULT 0,
			created_at   BIGINT NOT NULL,
			updated_at   BIGINT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_hub_peers_status ON hub_peers(status);
		CREATE TABLE IF NOT EXISTS calls (
			id              BIGSERIAL PRIMARY KEY,
			user_id         TEXT,
			datetime        BIGINT NOT NULL,
			system          INTEGER NOT NULL DEFAULT 0,
			system_label    TEXT    NOT NULL DEFAULT '',
			talkgroup       INTEGER NOT NULL DEFAULT 0,
			talkgroup_label TEXT    NOT NULL DEFAULT '',
			talkgroup_group TEXT    NOT NULL DEFAULT '',
			talkgroup_tag   TEXT    NOT NULL DEFAULT '',
			frequency       INTEGER NOT NULL DEFAULT 0,
			duration        DOUBLE PRECISION NOT NULL DEFAULT 0,
			audio_name      TEXT    NOT NULL DEFAULT '',
			audio_type      TEXT    NOT NULL DEFAULT 'audio/mpeg',
			audio           BYTEA   NOT NULL,
			created_at      BIGINT NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(id)
		);
		CREATE TABLE IF NOT EXISTS talkgroup_settings (
			talkgroup   INTEGER PRIMARY KEY,
			favorite    BOOLEAN NOT NULL DEFAULT FALSE,
			muted       BOOLEAN NOT NULL DEFAULT FALSE,
			transcribe  BOOLEAN NOT NULL DEFAULT FALSE,
			updated_at  BIGINT NOT NULL
		);
		ALTER TABLE talkgroup_settings ADD COLUMN IF NOT EXISTS transcribe BOOLEAN NOT NULL DEFAULT FALSE;
		CREATE TABLE IF NOT EXISTS call_transcripts (
			call_id       BIGINT PRIMARY KEY,
			status        TEXT NOT NULL DEFAULT 'pending',
			transcript    TEXT NOT NULL DEFAULT '',
			provider      TEXT NOT NULL DEFAULT '',
			language      TEXT NOT NULL DEFAULT '',
			confidence    DOUBLE PRECISION NOT NULL DEFAULT 0,
			error         TEXT NOT NULL DEFAULT '',
			attempts      INTEGER NOT NULL DEFAULT 0,
			claimed_by    TEXT NOT NULL DEFAULT '',
			claimed_until BIGINT NOT NULL DEFAULT 0,
			created_at    BIGINT NOT NULL,
			updated_at    BIGINT NOT NULL,
			FOREIGN KEY (call_id) REFERENCES calls(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_call_transcripts_status_claimed ON call_transcripts(status, claimed_until, call_id);
		CREATE TABLE IF NOT EXISTS federation_call_imports (
			peer_hub_id    TEXT   NOT NULL,
			remote_call_id BIGINT NOT NULL,
			local_call_id  BIGINT NOT NULL,
			created_at     BIGINT NOT NULL,
			PRIMARY KEY (peer_hub_id, remote_call_id),
			FOREIGN KEY (local_call_id) REFERENCES calls(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_federation_call_imports_peer_remote ON federation_call_imports(peer_hub_id, remote_call_id DESC);
		CREATE INDEX IF NOT EXISTS idx_calls_datetime ON calls(datetime DESC);
		CREATE INDEX IF NOT EXISTS idx_calls_talkgroup ON calls(talkgroup);
		CREATE TABLE IF NOT EXISTS ingestion_sources (
			id                  TEXT PRIMARY KEY,
			user_id             TEXT,
			label               TEXT    NOT NULL,
			enabled             BOOLEAN NOT NULL DEFAULT TRUE,
			deleted_at          BIGINT NOT NULL DEFAULT 0,
			system_id           INTEGER NOT NULL,
			system_label        TEXT    NOT NULL,
			last_seen_unix      BIGINT NOT NULL DEFAULT 0,
			error_count         INTEGER NOT NULL DEFAULT 0,
			calls_received      INTEGER NOT NULL DEFAULT 0,
			updated_at          BIGINT NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(id)
		);
		CREATE TABLE IF NOT EXISTS ingestion_source_keys (
			id              TEXT PRIMARY KEY,
			source_id       TEXT    NOT NULL,
			user_id         TEXT,
			api_key         TEXT    NOT NULL UNIQUE,
			created_at      BIGINT NOT NULL,
			last_used_at    BIGINT NOT NULL DEFAULT 0,
			FOREIGN KEY (user_id) REFERENCES users(id),
			FOREIGN KEY (source_id) REFERENCES ingestion_sources(id)
		);
		CREATE INDEX IF NOT EXISTS idx_source_keys_api_key ON ingestion_source_keys(api_key);
		CREATE TABLE IF NOT EXISTS ingestion_source_user_shares (
			source_id  TEXT   NOT NULL,
			user_id    TEXT   NOT NULL,
			created_at BIGINT NOT NULL,
			PRIMARY KEY (source_id, user_id),
			FOREIGN KEY (source_id) REFERENCES ingestion_sources(id) ON DELETE CASCADE,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_source_user_shares_user_id ON ingestion_source_user_shares(user_id);
		CREATE TABLE IF NOT EXISTS radio_sets (
			id          TEXT PRIMARY KEY,
			user_id     TEXT NOT NULL,
			name        TEXT NOT NULL,
			talkgroups  JSONB NOT NULL DEFAULT '[]',
			created_at  BIGINT NOT NULL,
			updated_at  BIGINT NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(id)
		);
		CREATE INDEX IF NOT EXISTS idx_radio_sets_user_id ON radio_sets(user_id);
		ALTER TABLE radio_sets ADD COLUMN IF NOT EXISTS share_token TEXT UNIQUE;
		ALTER TABLE ingestion_sources ADD COLUMN IF NOT EXISTS user_id TEXT;
		ALTER TABLE ingestion_sources ADD COLUMN IF NOT EXISTS deleted_at BIGINT NOT NULL DEFAULT 0;
		ALTER TABLE ingestion_sources ADD COLUMN IF NOT EXISTS is_shared BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE calls ADD COLUMN IF NOT EXISTS user_id TEXT;
		ALTER TABLE calls ADD COLUMN IF NOT EXISTS source_id TEXT;
		ALTER TABLE ingestion_source_keys ADD COLUMN IF NOT EXISTS user_id TEXT;
		CREATE INDEX IF NOT EXISTS idx_calls_source_id ON calls(source_id);
		CREATE INDEX IF NOT EXISTS idx_ingestion_sources_is_shared ON ingestion_sources(is_shared);
		DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'calls_user_id_fkey') THEN
				ALTER TABLE calls ADD CONSTRAINT calls_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id);
			END IF;
		END $$;
		DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'calls_source_id_fkey') THEN
				ALTER TABLE calls ADD CONSTRAINT calls_source_id_fkey FOREIGN KEY (source_id) REFERENCES ingestion_sources(id);
			END IF;
		END $$;
		DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'ingestion_sources_user_id_fkey') THEN
				ALTER TABLE ingestion_sources ADD CONSTRAINT ingestion_sources_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id);
			END IF;
		END $$;
		DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'ingestion_source_keys_user_id_fkey') THEN
				ALTER TABLE ingestion_source_keys ADD CONSTRAINT ingestion_source_keys_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id);
			END IF;
		END $$;

		-- PTT Phase 1: per-hub push-to-talk inside a radio set.
		-- dispatcher_enabled lets a user broadcast a single PTT to many radio
		-- sets at once via the /ptt/broadcast endpoint (e.g. a dispatcher who
		-- coordinates multiple talkgroups).
		ALTER TABLE users      ADD COLUMN IF NOT EXISTS tx_enabled         BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE users      ADD COLUMN IF NOT EXISTS dispatcher_enabled BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE radio_sets ADD COLUMN IF NOT EXISTS ptt_talkgroup  INTEGER;
		ALTER TABLE calls      ADD COLUMN IF NOT EXISTS origin         TEXT    NOT NULL DEFAULT 'rf';
		ALTER TABLE calls      ADD COLUMN IF NOT EXISTS sender_user_id TEXT;
		DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'calls_sender_user_id_fkey') THEN
				ALTER TABLE calls ADD CONSTRAINT calls_sender_user_id_fkey FOREIGN KEY (sender_user_id) REFERENCES users(id);
			END IF;
		END $$;
		CREATE SEQUENCE IF NOT EXISTS ptt_talkgroup_seq START 9000001;
		-- Backfill: every existing radio set gets a unique PTT talkgroup so the
		-- POST /ptt endpoint can rely on the column always being set.
		UPDATE radio_sets SET ptt_talkgroup = nextval('ptt_talkgroup_seq') WHERE ptt_talkgroup IS NULL;
		CREATE TABLE IF NOT EXISTS ptt_uploads (
			client_id  TEXT   PRIMARY KEY,
			call_id    BIGINT NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
			user_id    TEXT   NOT NULL REFERENCES users(id),
			created_at BIGINT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_ptt_uploads_user_created ON ptt_uploads(user_id, created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_calls_origin ON calls(origin);
	`)
	return err
}
