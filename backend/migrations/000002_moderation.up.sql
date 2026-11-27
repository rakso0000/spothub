ALTER TABLE users ADD COLUMN is_moderator BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN banned BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE spots ALTER COLUMN status SET DEFAULT 'pending';
ALTER TABLE messages ADD COLUMN hidden BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE reports (
  id BIGSERIAL PRIMARY KEY,
  reporter_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  target_type TEXT NOT NULL CHECK (target_type IN ('spot','message')),
  target_id BIGINT NOT NULL,
  reason TEXT NOT NULL CHECK (char_length(reason) <= 500),
  resolved BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (reporter_id, target_type, target_id)
);
CREATE INDEX reports_open_idx ON reports (resolved, created_at DESC);
