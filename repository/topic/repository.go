package topic

import (
	"context"
	"fmt"
	"strings"

	"gin-backend/repository"

	"github.com/google/uuid"
)

type Repository struct{}

var defaultRepo = &Repository{}

func Default() *Repository {
	return defaultRepo
}

func (r *Repository) Create(ctx context.Context, name, status string) (repository.Topic, error) {
	db := repository.DefaultGorm()
	if db == nil {
		return repository.Topic{}, fmt.Errorf("database store is not initialized")
	}

	record := repository.Topic{
		ID:     uuid.NewString(),
		Name:   strings.TrimSpace(name),
		Status: strings.TrimSpace(status),
	}
	if record.Name == "" {
		return repository.Topic{}, fmt.Errorf("topic_name is required")
	}
	if record.Status == "" {
		record.Status = "No Context"
	}
	if err := db.WithContext(ctx).Create(&record).Error; err != nil {
		return repository.Topic{}, err
	}
	return record, nil
}

func (r *Repository) UpdateStatus(ctx context.Context, topicID, status string) error {
	db := repository.DefaultGorm()
	if db == nil {
		return fmt.Errorf("database store is not initialized")
	}
	topicID = strings.TrimSpace(topicID)
	status = strings.TrimSpace(status)
	if topicID == "" || status == "" {
		return nil
	}
	result := db.WithContext(ctx).Model(&repository.Topic{}).Where("id = ?", topicID).Update("status", status)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("topic not found")
	}
	return nil
}

func (r *Repository) List(ctx context.Context, limit int) ([]repository.Topic, error) {
	db := repository.DefaultGorm()
	if db == nil {
		return nil, fmt.Errorf("database store is not initialized")
	}
	if limit <= 0 {
		limit = 100
	}

	var records []repository.Topic
	if err := db.WithContext(ctx).
		Order("updated_at DESC, created_at DESC").
		Limit(limit).
		Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

func (r *Repository) Get(ctx context.Context, topicID string) (repository.Topic, error) {
	db := repository.DefaultGorm()
	if db == nil {
		return repository.Topic{}, fmt.Errorf("database store is not initialized")
	}
	var record repository.Topic
	if err := db.WithContext(ctx).Where("id = ?", strings.TrimSpace(topicID)).First(&record).Error; err != nil {
		return repository.Topic{}, err
	}
	return record, nil
}
