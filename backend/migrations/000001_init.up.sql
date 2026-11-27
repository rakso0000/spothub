CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE users (
  id BIGSERIAL PRIMARY KEY,
  nickname TEXT UNIQUE NOT NULL,
  email TEXT UNIQUE NOT NULL,
  password_hash TEXT NOT NULL,
  created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE spots (
  id BIGSERIAL PRIMARY KEY,
  author_id BIGINT REFERENCES users(id),
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  location GEOGRAPHY(Point, 4326) NOT NULL,
  sports TEXT[] NOT NULL DEFAULT '{}',
  status TEXT NOT NULL DEFAULT 'approved',
  created_at TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX spots_location_idx ON spots USING GIST (location);

CREATE TABLE spot_ratings (
  spot_id BIGINT REFERENCES spots(id) ON DELETE CASCADE,
  user_id BIGINT REFERENCES users(id),
  surface SMALLINT CHECK (surface BETWEEN 1 AND 5),
  lighting SMALLINT CHECK (lighting BETWEEN 1 AND 5),
  security SMALLINT CHECK (security BETWEEN 1 AND 5),
  updated_at TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (spot_id, user_id)
);

CREATE TABLE messages (
  id BIGSERIAL PRIMARY KEY,
  spot_id BIGINT REFERENCES spots(id) ON DELETE CASCADE,
  user_id BIGINT REFERENCES users(id),
  text TEXT NOT NULL CHECK (char_length(text) <= 500),
  created_at TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX messages_spot_idx ON messages (spot_id, id DESC);

CREATE TABLE checkins (
  spot_id BIGINT REFERENCES spots(id) ON DELETE CASCADE,
  user_id BIGINT REFERENCES users(id),
  expires_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (spot_id, user_id)
);
