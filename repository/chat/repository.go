package chat

import (
	"context"
	"fmt"
	"strings"

	"gin-backend/repository"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func Default() *Repository {
	return New(repository.DefaultGorm())
}

func (r *Repository) Create(ctx context.Context, userID, title string) (repository.Chat, error) {
	db, err := r.getDB()
	if err != nil {
		return repository.Chat{}, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "New Chat"
	}

	record := repository.Chat{ID: uuid.NewString(), UserID: userID, Title: title}
	if err := db.WithContext(ctx).Create(&record).Error; err != nil {
		return repository.Chat{}, err
	}
	return record, nil
}

func (r *Repository) List(ctx context.Context, userID string, limit int) ([]repository.Chat, error) {
	db, err := r.getDB()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 10
	}
	var records []repository.Chat
	err = db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Find(&records).Error
	return records, err
}

func (r *Repository) Get(ctx context.Context, chatID, userID string) (repository.Chat, error) {
	db, err := r.getDB()
	if err != nil {
		return repository.Chat{}, err
	}
	var record repository.Chat
	err = db.WithContext(ctx).
		Where("id = ? AND user_id = ?", chatID, userID).
		First(&record).Error
	if err == gorm.ErrRecordNotFound {
		return repository.Chat{}, fmt.Errorf("chat not found")
	}
	return record, err
}

func (r *Repository) Delete(ctx context.Context, chatID, userID string) error {
	db, err := r.getDB()
	if err != nil {
		return err
	}
	result := db.WithContext(ctx).
		Where("id = ? AND user_id = ?", chatID, userID).
		Delete(&repository.Chat{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("chat not found")
	}
	return nil
}

func (r *Repository) getDB() (*gorm.DB, error) {
	if r.db != nil {
		return r.db, nil
	}
	if db := repository.DefaultGorm(); db != nil {
		return db, nil
	}
	return nil, fmt.Errorf("database store is not initialized")
}
