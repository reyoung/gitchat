package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/reyoung/gitchat/model"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir db dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS refs (
			name TEXT PRIMARY KEY,
			head_hash TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			branch TEXT NOT NULL,
			avatar_url TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS channels (
			id TEXT PRIMARY KEY,
			branch TEXT NOT NULL,
			creator TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			is_public INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS experiments (
			id TEXT PRIMARY KEY,
			branch TEXT NOT NULL,
			creator TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS messages (
			commit_hash TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			branch TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			subject TEXT NOT NULL,
			body TEXT NOT NULL,
			reply_to TEXT NOT NULL DEFAULT '',
			edit_of TEXT NOT NULL DEFAULT '',
			delete_of TEXT NOT NULL DEFAULT '',
			follows_json TEXT NOT NULL,
			experiment_id TEXT NOT NULL DEFAULT '',
			experiment_sha TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS channel_events (
			commit_hash TEXT PRIMARY KEY,
			branch TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			actor TEXT NOT NULL DEFAULT '',
			member TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS experiment_events (
			commit_hash TEXT PRIMARY KEY,
			branch TEXT NOT NULL,
			experiment_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			actor TEXT NOT NULL DEFAULT '',
			retained_sha TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate %q: %w", stmt, err)
		}
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE messages ADD COLUMN edit_of TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add messages.edit_of: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE messages ADD COLUMN delete_of TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add messages.delete_of: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE users ADD COLUMN avatar_url TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add users.avatar_url: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE channels ADD COLUMN is_public INTEGER NOT NULL DEFAULT 0`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add channels.is_public: %w", err)
	}
	return nil
}

func (s *Store) GetRefHead(ctx context.Context, name string) (string, bool, error) {
	var head string
	err := s.db.QueryRowContext(ctx, `SELECT head_hash FROM refs WHERE name = ?`, name).Scan(&head)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return head, true, nil
}

func (s *Store) ReplaceUserBranch(ctx context.Context, user model.User) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO users(id, branch, avatar_url) VALUES(?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET branch = excluded.branch, avatar_url = excluded.avatar_url`, user.ID, user.Branch, user.AvatarURL)
	return err
}

func (s *Store) ReplaceChannelBranch(ctx context.Context, channel model.Channel) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO channels(id, branch, creator, title, is_public) VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET branch = excluded.branch, creator = excluded.creator, title = excluded.title, is_public = excluded.is_public`,
		channel.ID, channel.Branch, channel.Creator, channel.Title, boolToInt(channel.IsPublic))
	return err
}

func (s *Store) ReplaceExperimentBranch(ctx context.Context, experiment model.Experiment) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO experiments(id, branch, creator, title) VALUES(?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET branch = excluded.branch, creator = excluded.creator, title = excluded.title`,
		experiment.ID, experiment.Branch, experiment.Creator, experiment.Title)
	return err
}

func (s *Store) ReplaceUserMessages(ctx context.Context, branch string, messages []model.Message) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE branch = ?`, branch); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO messages(
			commit_hash, user_id, branch, channel_id, subject, body, reply_to, edit_of, delete_of, follows_json, experiment_id, experiment_sha, created_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, message := range messages {
		follows, err := json.Marshal(message.Follows)
		if err != nil {
			return err
		}
		if _, err := stmt.ExecContext(ctx,
			message.CommitHash,
			message.UserID,
			message.Branch,
			message.ChannelID,
			message.Subject,
			message.Body,
			message.ReplyTo,
			message.EditOf,
			message.DeleteOf,
			string(follows),
			message.ExperimentID,
			message.ExperimentSHA,
			message.CreatedAt.UTC().Format(time.RFC3339),
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ReplaceChannelEvents(ctx context.Context, branch string, events []model.ChannelEvent) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM channel_events WHERE branch = ?`, branch); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO channel_events(commit_hash, branch, channel_id, event_type, actor, member, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, event := range events {
		if _, err := stmt.ExecContext(ctx, event.CommitHash, event.Branch, event.ChannelID, event.EventType, event.Actor, event.Member, event.CreatedAt.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ReplaceExperimentEvents(ctx context.Context, branch string, events []model.ExperimentEvent) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM experiment_events WHERE branch = ?`, branch); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO experiment_events(commit_hash, branch, experiment_id, event_type, actor, retained_sha, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, event := range events {
		if _, err := stmt.ExecContext(ctx, event.CommitHash, event.Branch, event.ExperimentID, event.EventType, event.Actor, event.RetainedSHA, event.CreatedAt.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) UpdateRefHead(ctx context.Context, ref model.RefState) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO refs(name, head_hash, updated_at) VALUES(?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET head_hash = excluded.head_hash, updated_at = excluded.updated_at`,
		ref.Name, ref.HeadHash, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *Store) ListChannels(ctx context.Context) ([]model.Channel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, branch, creator, title, is_public FROM channels ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var channels []model.Channel
	for rows.Next() {
		var channel model.Channel
		var isPublic int
		if err := rows.Scan(&channel.ID, &channel.Branch, &channel.Creator, &channel.Title, &isPublic); err != nil {
			return nil, err
		}
		channel.IsPublic = isPublic != 0
		channels = append(channels, channel)
	}
	return channels, rows.Err()
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (s *Store) ListUsers(ctx context.Context) ([]model.User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, branch, avatar_url FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []model.User
	for rows.Next() {
		var user model.User
		if err := rows.Scan(&user.ID, &user.Branch, &user.AvatarURL); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *Store) ListExperiments(ctx context.Context) ([]model.Experiment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, branch, creator, title FROM experiments ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var experiments []model.Experiment
	for rows.Next() {
		var experiment model.Experiment
		if err := rows.Scan(&experiment.ID, &experiment.Branch, &experiment.Creator, &experiment.Title); err != nil {
			return nil, err
		}
		experiments = append(experiments, experiment)
	}
	return experiments, rows.Err()
}

func (s *Store) ListMessagesByChannel(ctx context.Context, channelID string) ([]model.Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT commit_hash, user_id, branch, channel_id, subject, body, reply_to, edit_of, delete_of, follows_json, experiment_id, experiment_sha, created_at
		FROM messages WHERE channel_id = ? ORDER BY created_at, commit_hash`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var messages []model.Message
	for rows.Next() {
		var message model.Message
		var followsJSON string
		var createdAt string
		if err := rows.Scan(
			&message.CommitHash,
			&message.UserID,
			&message.Branch,
			&message.ChannelID,
			&message.Subject,
			&message.Body,
			&message.ReplyTo,
			&message.EditOf,
			&message.DeleteOf,
			&followsJSON,
			&message.ExperimentID,
			&message.ExperimentSHA,
			&createdAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(followsJSON), &message.Follows); err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return nil, err
		}
		message.CreatedAt = t
		messages = append(messages, message)
	}
	return messages, rows.Err()
}
