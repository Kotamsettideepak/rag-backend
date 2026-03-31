package repository

import (
	"context"
	"fmt"
	"sync"

	"gin-backend/config"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Store struct {
	db *gorm.DB
}

var (
	defaultStore *Store
	storeOnce    sync.Once
)

func Default() *Store {
	return defaultStore
}

func DefaultGorm() *gorm.DB {
	if defaultStore == nil {
		return nil
	}
	return defaultStore.db
}

func InitDefault(ctx context.Context) error {
	var initErr error
	storeOnce.Do(func() {
		databaseURL := config.GetDatabaseURL()
		if databaseURL == "" {
			initErr = fmt.Errorf("DATABASE_URL is required")
			return
		}

		gormDB, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{
			// Keep DB errors visible but suppress SQL/slow-query statement logs.
			Logger: logger.Default.LogMode(logger.Error),
		})
		if err != nil {
			initErr = err
			return
		}

		store := &Store{db: gormDB}
		if err := store.AutoMigrate(ctx); err != nil {
			initErr = err
			return
		}

		defaultStore = store
	})
	return initErr
}

func (s *Store) Close() {
	if s == nil || s.db == nil {
		return
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		return
	}
	_ = sqlDB.Close()
}

func (s *Store) AutoMigrate(ctx context.Context) error {
	db := s.db.WithContext(ctx)
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS vector`).Error; err != nil {
		return err
	}
	if err := db.AutoMigrate(&User{}, &Chat{}, &Message{}, &UserUploadedData{}); err != nil {
		return err
	}
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS context_vectors (
			id text PRIMARY KEY,
			chat_id uuid NULL,
			user_id uuid NULL,
			file_id text NOT NULL,
			file_name text NOT NULL,
			file_kind text NOT NULL,
			chunk_type text NOT NULL DEFAULT 'text',
			section_title text NULL,
			code_language text NULL,
			page_from integer NULL,
			page_to integer NULL,
			has_formula boolean NOT NULL DEFAULT false,
			picture_class text NULL,
			page integer NOT NULL DEFAULT 0,
			chunk_index integer NOT NULL DEFAULT 0,
			hash text NOT NULL DEFAULT '',
			document text NOT NULL,
			metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
			embedding vector(1024) NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now()
		)
	`).Error; err != nil {
		return err
	}
	alterStmts := []string{
		`ALTER TABLE context_vectors ADD COLUMN IF NOT EXISTS chunk_type text NOT NULL DEFAULT 'text'`,
		`ALTER TABLE context_vectors ADD COLUMN IF NOT EXISTS section_title text NULL`,
		`ALTER TABLE context_vectors ADD COLUMN IF NOT EXISTS code_language text NULL`,
		`ALTER TABLE context_vectors ADD COLUMN IF NOT EXISTS page_from integer NULL`,
		`ALTER TABLE context_vectors ADD COLUMN IF NOT EXISTS page_to integer NULL`,
		`ALTER TABLE context_vectors ADD COLUMN IF NOT EXISTS has_formula boolean NOT NULL DEFAULT false`,
		`ALTER TABLE context_vectors ADD COLUMN IF NOT EXISTS picture_class text NULL`,
	}
	for _, stmt := range alterStmts {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_context_vectors_chat_user ON context_vectors (chat_id, user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_context_vectors_file_id ON context_vectors (file_id)`,
		`CREATE INDEX IF NOT EXISTS idx_context_vectors_chat_user_chunk_type ON context_vectors (chat_id, user_id, chunk_type)`,
		`CREATE INDEX IF NOT EXISTS idx_context_vectors_metadata ON context_vectors USING gin (metadata)`,
		`CREATE INDEX IF NOT EXISTS idx_context_vectors_embedding_hnsw ON context_vectors USING hnsw (embedding vector_cosine_ops)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_context_vectors_chat_file_hash ON context_vectors (chat_id, user_id, file_id, hash) WHERE chat_id IS NOT NULL AND user_id IS NOT NULL AND hash <> ''`,
	}
	for _, stmt := range indexes {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Gorm() *gorm.DB {
	if s == nil {
		return nil
	}
	return s.db
}
