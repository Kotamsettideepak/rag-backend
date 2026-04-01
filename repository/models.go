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
	ChatID           *string   `gorm:"type:uuid;index:idx_uploaded_data_chat_created,priority:1;index" json:"chat_id,omitempty"`
	FileURL          string    `gorm:"type:text;not null" json:"file_url"`
	FileType         string    `gorm:"type:text;not null" json:"file_type"`
	OriginalFileName string    `gorm:"type:text" json:"original_file_name"`
	CreatedAt        time.Time `gorm:"index:idx_uploaded_data_chat_created,priority:2" json:"created_at"`
	Chat             *Chat     `gorm:"foreignKey:ChatID;constraint:OnDelete:CASCADE" json:"-"`
}

func (UserUploadedData) TableName() string {
	return "user_uploaded_data"
}

type Topic struct {
	ID        string    `gorm:"type:uuid;primaryKey" json:"id"`
	Name      string    `gorm:"type:text;not null;uniqueIndex" json:"name"`
	Status    string    `gorm:"type:text;not null;default:'No Context'" json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Topic) TableName() string {
	return "topics"
}

type TopicChunkFailure struct {
	ID         string    `gorm:"type:uuid;primaryKey" json:"id"`
	TopicID    string    `gorm:"type:uuid;not null;index" json:"topic_id"`
	JobID      string    `gorm:"type:text;index" json:"job_id"`
	FileID     string    `gorm:"type:text;not null" json:"file_id"`
	ChunkIndex int       `gorm:"not null;default:0" json:"chunk_index"`
	Reason     string    `gorm:"type:text;not null" json:"reason"`
	Attempts   int       `gorm:"not null;default:0" json:"attempts"`
	Payload    string    `gorm:"type:jsonb;not null;default:'{}'" json:"payload"`
	CreatedAt  time.Time `json:"created_at"`
}

func (TopicChunkFailure) TableName() string {
	return "topic_chunk_failures"
}

type QuizSession struct {
	ID                    string     `gorm:"type:uuid;primaryKey" json:"id"`
	UserID                string     `gorm:"type:uuid;not null;index" json:"user_id"`
	TopicID               string     `gorm:"type:uuid;not null;index" json:"topic_id"`
	Status                string     `gorm:"type:text;not null;default:'generating'" json:"status"`
	DisplayState          string     `gorm:"type:text;not null;default:'waiting_questions'" json:"display_state"`
	ReportStatus          string     `gorm:"type:text;not null;default:'pending'" json:"report_status"`
	QuestionCountPerTopic int        `gorm:"not null;default:5" json:"question_count_per_topic"`
	StartedAt             time.Time  `json:"started_at"`
	CompletedAt           *time.Time `json:"completed_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

func (QuizSession) TableName() string {
	return "quiz_sessions"
}

type QuizTopicItem struct {
	ID                  string    `gorm:"type:uuid;primaryKey" json:"id"`
	QuizSessionID       string    `gorm:"type:uuid;not null;index:idx_quiz_topic_items_session_sequence,priority:1" json:"quiz_session_id"`
	Name                string    `gorm:"type:text;not null" json:"name"`
	Status              string    `gorm:"type:text;not null;default:'queued'" json:"status"`
	Sequence            int       `gorm:"not null;default:0;index:idx_quiz_topic_items_session_sequence,priority:2" json:"sequence"`
	GeneratedQuestions  int       `gorm:"not null;default:0" json:"generated_questions"`
	MatchedChaptersJSON string    `gorm:"type:text;not null;default:'[]'" json:"matched_chapters_json"`
	Notes               string    `gorm:"type:text" json:"notes"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func (QuizTopicItem) TableName() string {
	return "quiz_topic_items"
}

type QuizQuestion struct {
	ID                string    `gorm:"type:uuid;primaryKey" json:"id"`
	QuizSessionID     string    `gorm:"type:uuid;not null;index:idx_quiz_questions_session_sequence,priority:1" json:"quiz_session_id"`
	QuizTopicItemID   string    `gorm:"type:uuid;not null;index" json:"quiz_topic_item_id"`
	Sequence          int       `gorm:"not null;default:0;index:idx_quiz_questions_session_sequence,priority:2" json:"sequence"`
	Prompt            string    `gorm:"type:text;not null" json:"prompt"`
	QuestionType      string    `gorm:"type:text;not null;default:'direct'" json:"question_type"`
	ChapterName       string    `gorm:"type:text" json:"chapter_name"`
	CorrectAnswer     string    `gorm:"type:text;not null" json:"correct_answer"`
	SupportingContext string    `gorm:"type:text;not null" json:"supporting_context"`
	Status            string    `gorm:"type:text;not null;default:'generated'" json:"status"`
	EvaluationStatus  string    `gorm:"type:text;not null;default:'pending'" json:"evaluation_status"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (QuizQuestion) TableName() string {
	return "quiz_questions"
}

type QuizAnswer struct {
	ID            string    `gorm:"type:uuid;primaryKey" json:"id"`
	QuizSessionID string    `gorm:"type:uuid;not null;index" json:"quiz_session_id"`
	QuizQuestionID string   `gorm:"type:uuid;not null;uniqueIndex" json:"quiz_question_id"`
	Response      string    `gorm:"type:text;not null" json:"response"`
	ResponseMode  string    `gorm:"type:text;not null;default:'typed'" json:"response_mode"`
	ElapsedSeconds int      `gorm:"not null;default:0" json:"elapsed_seconds"`
	SubmittedAt   time.Time `json:"submitted_at"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (QuizAnswer) TableName() string {
	return "quiz_answers"
}

type QuizEvaluation struct {
	ID              string    `gorm:"type:uuid;primaryKey" json:"id"`
	QuizSessionID   string    `gorm:"type:uuid;not null;index" json:"quiz_session_id"`
	QuizQuestionID  string    `gorm:"type:uuid;not null;uniqueIndex" json:"quiz_question_id"`
	Score           int       `gorm:"not null;default:0" json:"score"`
	Level           string    `gorm:"type:text;not null;default:'mid'" json:"level"`
	IsCorrect       bool      `gorm:"not null;default:false" json:"is_correct"`
	Feedback        string    `gorm:"type:text;not null" json:"feedback"`
	ImprovementNote string    `gorm:"type:text" json:"improvement_note"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (QuizEvaluation) TableName() string {
	return "quiz_evaluations"
}

type QuizReport struct {
	ID                   string     `gorm:"type:uuid;primaryKey" json:"id"`
	QuizSessionID        string     `gorm:"type:uuid;not null;uniqueIndex" json:"quiz_session_id"`
	OverallScore         int        `gorm:"not null;default:0" json:"overall_score"`
	Summary              string     `gorm:"type:text;not null" json:"summary"`
	StrengthsJSON        string     `gorm:"type:text;not null;default:'[]'" json:"strengths_json"`
	WeaknessesJSON       string     `gorm:"type:text;not null;default:'[]'" json:"weaknesses_json"`
	RecommendationsJSON  string     `gorm:"type:text;not null;default:'[]'" json:"recommendations_json"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

func (QuizReport) TableName() string {
	return "quiz_reports"
}
