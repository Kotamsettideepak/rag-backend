package message

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

func (r *Repository) Save(ctx context.Context, chatID, role, content string) (repository.Message, error) {
	db, err := r.getDB()
	if err != nil {
		return repository.Message{}, err
	}
	role = strings.TrimSpace(role)
	content = strings.TrimSpace(content)
	if role == "" || content == "" {
		return repository.Message{}, fmt.Errorf("role and content are required")
	}

	record := repository.Message{
		ID:      uuid.NewString(),
		ChatID:  chatID,
		Role:    role,
		Content: content,
	}
	if err := db.WithContext(ctx).Create(&record).Error; err != nil {
		return repository.Message{}, err
	}
	return record, nil
}

func (r *Repository) List(ctx context.Context, chatID string, limit int) ([]repository.Message, error) {
	db, err := r.getDB()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}

	var records []repository.Message
	err = db.WithContext(ctx).
		Where("chat_id = ?", chatID).
		Order("created_at DESC").
		Limit(limit).
		Find(&records).Error
	if err != nil {
		return nil, err
	}

	for left, right := 0, len(records)-1; left < right; left, right = left+1, right-1 {
		records[left], records[right] = records[right], records[left]
	}
	return records, nil
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
