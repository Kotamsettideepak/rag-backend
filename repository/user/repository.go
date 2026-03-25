package user

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

func (r *Repository) EnsureByEmail(ctx context.Context, email string) (repository.User, error) {
	db, err := r.getDB()
	if err != nil {
		return repository.User{}, err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return repository.User{}, fmt.Errorf("email is required")
	}

	var user repository.User
	err = db.WithContext(ctx).Where("email = ?", email).First(&user).Error
	if err == nil {
		return user, nil
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return repository.User{}, err
	}

	user = repository.User{ID: uuid.NewString(), Email: email}
	if err := db.WithContext(ctx).Create(&user).Error; err != nil {
		return repository.User{}, err
	}
	return user, nil
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
