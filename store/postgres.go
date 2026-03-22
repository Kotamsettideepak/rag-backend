package store

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"gin-backend/config"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type User struct {
	ID        string    `gorm:"type:uuid;primaryKey" json:"id"`
	Email     string    `gorm:"type:text;not null;uniqueIndex" json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

type Chat struct {
	ID        string    `gorm:"type:uuid;primaryKey" json:"id"`
	UserID    string    `gorm:"type:uuid;not null;index:idx_chats_user_created,priority:1" json:"user_id"`
	Title     string    `gorm:"type:text;not null" json:"title"`
	CreatedAt time.Time `gorm:"index:idx_chats_user_created,priority:2" json:"created_at"`
	User      User      `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
}

type Message struct {
	ID        string    `gorm:"type:uuid;primaryKey" json:"id"`
	ChatID    string    `gorm:"type:uuid;not null;index:idx_messages_chat_created,priority:1" json:"chat_id"`
	Role      string    `gorm:"type:text;not null" json:"role"`
	Content   string    `gorm:"type:text;not null" json:"content"`
	CreatedAt time.Time `gorm:"index:idx_messages_chat_created,priority:2" json:"created_at"`
	Chat      Chat      `gorm:"foreignKey:ChatID;constraint:OnDelete:CASCADE" json:"-"`
}

type UserUploadedData struct {
	ID               string    `gorm:"type:uuid;primaryKey" json:"id"`
	ChatID           string    `gorm:"type:uuid;not null;index:idx_uploaded_data_chat_created,priority:1" json:"chat_id"`
	FileURL          string    `gorm:"type:text;not null" json:"file_url"`
	FileType         string    `gorm:"type:text;not null" json:"file_type"`
	OriginalFileName string    `gorm:"type:text" json:"original_file_name"`
	CreatedAt        time.Time `gorm:"index:idx_uploaded_data_chat_created,priority:2" json:"created_at"`
	Chat             Chat      `gorm:"foreignKey:ChatID;constraint:OnDelete:CASCADE" json:"-"`
}

func (UserUploadedData) TableName() string {
	return "user_uploaded_data"
}

type Store struct {
	db *gorm.DB
}

var (
	defaultStore *Store
	storeOnce    sync.Once
)

func DefaultStore() *Store {
	return defaultStore
}

func InitDefaultStore(ctx context.Context) error {
	var initErr error
	storeOnce.Do(func() {
		databaseURL := config.GetDatabaseURL()
		if databaseURL == "" {
			initErr = fmt.Errorf("DATABASE_URL is required")
			return
		}

		db, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{})
		if err != nil {
			initErr = err
			return
		}

		store := &Store{db: db}
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

func (s *Store) EnsureUserByEmail(ctx context.Context, email string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return User{}, fmt.Errorf("email is required")
	}

	var user User
	err := s.db.WithContext(ctx).Where("email = ?", email).First(&user).Error
	if err == nil {
		return user, nil
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return User{}, err
	}

	user = User{
		ID:    uuid.NewString(),
		Email: email,
	}
	if err := s.db.WithContext(ctx).Create(&user).Error; err != nil {
		return User{}, err
	}

	return user, nil
}

func (s *Store) CreateChat(ctx context.Context, userID string, title string) (Chat, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "New Chat"
	}

	chat := Chat{
		ID:     uuid.NewString(),
		UserID: userID,
		Title:  title,
	}
	if err := s.db.WithContext(ctx).Create(&chat).Error; err != nil {
		return Chat{}, err
	}

	return chat, nil
}

func (s *Store) ListChats(ctx context.Context, userID string, limit int) ([]Chat, error) {
	if limit <= 0 {
		limit = 10
	}

	var chats []Chat
	err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Find(&chats).Error
	if err != nil {
		return nil, err
	}

	return chats, nil
}

func (s *Store) GetChat(ctx context.Context, chatID string, userID string) (Chat, error) {
	var chat Chat
	err := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", chatID, userID).
		First(&chat).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return Chat{}, fmt.Errorf("chat not found")
		}
		return Chat{}, err
	}

	return chat, nil
}

func (s *Store) SaveMessage(ctx context.Context, chatID string, role string, content string) (Message, error) {
	role = strings.TrimSpace(role)
	content = strings.TrimSpace(content)
	if role == "" || content == "" {
		return Message{}, fmt.Errorf("role and content are required")
	}

	message := Message{
		ID:      uuid.NewString(),
		ChatID:  chatID,
		Role:    role,
		Content: content,
	}
	if err := s.db.WithContext(ctx).Create(&message).Error; err != nil {
		return Message{}, err
	}

	return message, nil
}

func (s *Store) ListMessages(ctx context.Context, chatID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}

	var recent []Message
	err := s.db.WithContext(ctx).
		Where("chat_id = ?", chatID).
		Order("created_at DESC").
		Limit(limit).
		Find(&recent).Error
	if err != nil {
		return nil, err
	}

	for left, right := 0, len(recent)-1; left < right; left, right = left+1, right-1 {
		recent[left], recent[right] = recent[right], recent[left]
	}

	return recent, nil
}

func (s *Store) CreateUserUploadedData(ctx context.Context, chatID string, fileURL string, fileType string, originalFileName string) (UserUploadedData, error) {
	uploaded := UserUploadedData{
		ID:               uuid.NewString(),
		ChatID:           chatID,
		FileURL:          strings.TrimSpace(fileURL),
		FileType:         strings.TrimSpace(fileType),
		OriginalFileName: strings.TrimSpace(originalFileName),
	}
	if err := s.db.WithContext(ctx).Create(&uploaded).Error; err != nil {
		return UserUploadedData{}, err
	}

	return uploaded, nil
}

func (s *Store) ListUserUploadedData(ctx context.Context, chatID string) ([]UserUploadedData, error) {
	var uploaded []UserUploadedData
	err := s.db.WithContext(ctx).
		Where("chat_id = ?", chatID).
		Order("created_at ASC").
		Find(&uploaded).Error
	if err != nil {
		return nil, err
	}

	return uploaded, nil
}
