package upload

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

func (r *Repository) Create(ctx context.Context, chatID, fileURL, fileType, originalFileName string) (repository.UserUploadedData, error) {
	db, err := r.getDB()
	if err != nil {
		return repository.UserUploadedData{}, err
	}
	record := repository.UserUploadedData{
		ID:               uuid.NewString(),
		ChatID:           chatID,
		FileURL:          strings.TrimSpace(fileURL),
		FileType:         strings.TrimSpace(fileType),
		OriginalFileName: strings.TrimSpace(originalFileName),
	}
	if err := db.WithContext(ctx).Create(&record).Error; err != nil {
		return repository.UserUploadedData{}, err
	}
	return record, nil
}

func (r *Repository) List(ctx context.Context, chatID string) ([]repository.UserUploadedData, error) {
	db, err := r.getDB()
	if err != nil {
		return nil, err
	}
	var records []repository.UserUploadedData
	err = db.WithContext(ctx).
		Where("chat_id = ?", chatID).
		Order("created_at ASC").
		Find(&records).Error
	return records, err
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
