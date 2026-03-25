package repository

import "time"

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
