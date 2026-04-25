-- Initial schema draft. See .tocodex/plans/go-im-ten-million-architecture.md for full table definitions.
CREATE TABLE IF NOT EXISTS users (
  id BIGINT UNSIGNED NOT NULL,
  account VARCHAR(128) NOT NULL,
  nickname VARCHAR(128) NOT NULL DEFAULT '',
  avatar_url VARCHAR(512) NOT NULL DEFAULT '',
  status TINYINT NOT NULL DEFAULT 1,
  created_at BIGINT UNSIGNED NOT NULL,
  updated_at BIGINT UNSIGNED NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_account (account)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS conversations (
  id BIGINT UNSIGNED NOT NULL,
  type TINYINT NOT NULL,
  single_key VARCHAR(128) NOT NULL DEFAULT '',
  group_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
  last_msg_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
  last_seq BIGINT UNSIGNED NOT NULL DEFAULT 0,
  created_at BIGINT UNSIGNED NOT NULL,
  updated_at BIGINT UNSIGNED NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_single_key (single_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
