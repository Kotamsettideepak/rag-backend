package repository

import (
	"context"
	"fmt"
	"sync"

	"gin-backend/config"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
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

		gormDB, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{})
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
	return s.db.WithContext(ctx).AutoMigrate(&User{}, &Chat{}, &Message{}, &UserUploadedData{})
}

func (s *Store) Gorm() *gorm.DB {
	if s == nil {
		return nil
	}
	return s.db
}
