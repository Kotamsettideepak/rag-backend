package quiz

import (
	"context"
	"fmt"
	"strings"
	"time"

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

func (r *Repository) CreateSession(ctx context.Context, userID, topicID string, questionCountPerTopic int) (repository.QuizSession, error) {
	db, err := r.getDB()
	if err != nil {
		return repository.QuizSession{}, err
	}
	if questionCountPerTopic <= 0 {
		questionCountPerTopic = 5
	}
	record := repository.QuizSession{
		ID:                    uuid.NewString(),
		UserID:                strings.TrimSpace(userID),
		TopicID:               strings.TrimSpace(topicID),
		Status:                "generating",
		DisplayState:          "waiting_questions",
		ReportStatus:          "pending",
		QuestionCountPerTopic: questionCountPerTopic,
		StartedAt:             time.Now().UTC(),
	}
	if err := db.WithContext(ctx).Create(&record).Error; err != nil {
		return repository.QuizSession{}, err
	}
	return record, nil
}

func (r *Repository) CreateTopicItems(ctx context.Context, sessionID string, names []string) ([]repository.QuizTopicItem, error) {
	db, err := r.getDB()
	if err != nil {
		return nil, err
	}
	items := make([]repository.QuizTopicItem, 0, len(names))
	for index, name := range names {
		items = append(items, repository.QuizTopicItem{
			ID:            uuid.NewString(),
			QuizSessionID: strings.TrimSpace(sessionID),
			Name:          strings.TrimSpace(name),
			Status:        "queued",
			Sequence:      index + 1,
		})
	}
	if len(items) == 0 {
		return nil, nil
	}
	if err := db.WithContext(ctx).Create(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repository) GetSession(ctx context.Context, userID, sessionID string) (repository.QuizSession, error) {
	db, err := r.getDB()
	if err != nil {
		return repository.QuizSession{}, err
	}
	var record repository.QuizSession
	query := db.WithContext(ctx).Where("id = ?", strings.TrimSpace(sessionID))
	if strings.TrimSpace(userID) != "" {
		query = query.Where("user_id = ?", strings.TrimSpace(userID))
	}
	if err := query.First(&record).Error; err != nil {
		return repository.QuizSession{}, err
	}
	return record, nil
}

func (r *Repository) ListSessionsByTopic(ctx context.Context, userID, topicID string, limit int) ([]repository.QuizSession, error) {
	db, err := r.getDB()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 20
	}
	var records []repository.QuizSession
	if err := db.WithContext(ctx).
		Where("user_id = ? AND topic_id = ?", strings.TrimSpace(userID), strings.TrimSpace(topicID)).
		Order("created_at DESC").
		Limit(limit).
		Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

func (r *Repository) ListTopicItems(ctx context.Context, sessionID string) ([]repository.QuizTopicItem, error) {
	db, err := r.getDB()
	if err != nil {
		return nil, err
	}
	var records []repository.QuizTopicItem
	if err := db.WithContext(ctx).
		Where("quiz_session_id = ?", strings.TrimSpace(sessionID)).
		Order("sequence ASC, created_at ASC").
		Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

func (r *Repository) UpdateTopicItemStatus(ctx context.Context, itemID, status, notes, chaptersJSON string, generatedQuestions int) error {
	db, err := r.getDB()
	if err != nil {
		return err
	}
	updates := map[string]interface{}{
		"status": status,
	}
	if notes != "" {
		updates["notes"] = notes
	}
	if chaptersJSON != "" {
		updates["matched_chapters_json"] = chaptersJSON
	}
	if generatedQuestions >= 0 {
		updates["generated_questions"] = generatedQuestions
	}
	return db.WithContext(ctx).Model(&repository.QuizTopicItem{}).
		Where("id = ?", strings.TrimSpace(itemID)).
		Updates(updates).Error
}

func (r *Repository) SaveQuestions(ctx context.Context, questions []repository.QuizQuestion) error {
	db, err := r.getDB()
	if err != nil {
		return err
	}
	if len(questions) == 0 {
		return nil
	}
	return db.WithContext(ctx).Create(&questions).Error
}

func (r *Repository) ListQuestions(ctx context.Context, sessionID string) ([]repository.QuizQuestion, error) {
	db, err := r.getDB()
	if err != nil {
		return nil, err
	}
	var records []repository.QuizQuestion
	if err := db.WithContext(ctx).
		Where("quiz_session_id = ?", strings.TrimSpace(sessionID)).
		Order("sequence ASC, created_at ASC").
		Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

func (r *Repository) GetQuestion(ctx context.Context, sessionID, questionID string) (repository.QuizQuestion, error) {
	db, err := r.getDB()
	if err != nil {
		return repository.QuizQuestion{}, err
	}
	var record repository.QuizQuestion
	if err := db.WithContext(ctx).
		Where("id = ? AND quiz_session_id = ?", strings.TrimSpace(questionID), strings.TrimSpace(sessionID)).
		First(&record).Error; err != nil {
		return repository.QuizQuestion{}, err
	}
	return record, nil
}

func (r *Repository) SaveAnswer(ctx context.Context, sessionID, questionID, response, responseMode string, elapsedSeconds int) (repository.QuizAnswer, error) {
	db, err := r.getDB()
	if err != nil {
		return repository.QuizAnswer{}, err
	}
	now := time.Now().UTC()
	record := repository.QuizAnswer{
		ID:             uuid.NewString(),
		QuizSessionID:  strings.TrimSpace(sessionID),
		QuizQuestionID: strings.TrimSpace(questionID),
		Response:       strings.TrimSpace(response),
		ResponseMode:   strings.TrimSpace(responseMode),
		ElapsedSeconds: elapsedSeconds,
		SubmittedAt:    now,
	}
	if record.ResponseMode == "" {
		record.ResponseMode = "typed"
	}

	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing repository.QuizAnswer
		findErr := tx.Where("quiz_question_id = ?", record.QuizQuestionID).First(&existing).Error
		switch {
		case findErr == nil:
			record.ID = existing.ID
			return tx.Model(&existing).Updates(map[string]interface{}{
				"response":        record.Response,
				"response_mode":   record.ResponseMode,
				"elapsed_seconds": record.ElapsedSeconds,
				"submitted_at":    record.SubmittedAt,
			}).Error
		}
		if findErr != nil && findErr != gorm.ErrRecordNotFound {
			return findErr
		}
		return tx.Create(&record).Error
	})
	if err != nil {
		return repository.QuizAnswer{}, err
	}
	return r.GetAnswer(ctx, questionID)
}

func (r *Repository) GetAnswer(ctx context.Context, questionID string) (repository.QuizAnswer, error) {
	db, err := r.getDB()
	if err != nil {
		return repository.QuizAnswer{}, err
	}
	var record repository.QuizAnswer
	if err := db.WithContext(ctx).
		Where("quiz_question_id = ?", strings.TrimSpace(questionID)).
		First(&record).Error; err != nil {
		return repository.QuizAnswer{}, err
	}
	return record, nil
}

func (r *Repository) ListAnswers(ctx context.Context, sessionID string) ([]repository.QuizAnswer, error) {
	db, err := r.getDB()
	if err != nil {
		return nil, err
	}
	var records []repository.QuizAnswer
	if err := db.WithContext(ctx).
		Where("quiz_session_id = ?", strings.TrimSpace(sessionID)).
		Order("submitted_at ASC, created_at ASC").
		Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

func (r *Repository) SaveEvaluation(ctx context.Context, sessionID, questionID string, score int, isCorrect bool, feedback, improvementNote string) (repository.QuizEvaluation, error) {
	db, err := r.getDB()
	if err != nil {
		return repository.QuizEvaluation{}, err
	}
	record := repository.QuizEvaluation{
		ID:              uuid.NewString(),
		QuizSessionID:   strings.TrimSpace(sessionID),
		QuizQuestionID:  strings.TrimSpace(questionID),
		Score:           score,
		Level:           evaluationLevel(score),
		IsCorrect:       isCorrect,
		Feedback:        strings.TrimSpace(feedback),
		ImprovementNote: strings.TrimSpace(improvementNote),
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing repository.QuizEvaluation
		findErr := tx.Where("quiz_question_id = ?", record.QuizQuestionID).First(&existing).Error
		if findErr == nil {
			record.ID = existing.ID
			return tx.Model(&existing).Updates(map[string]interface{}{
				"score":            record.Score,
				"level":            record.Level,
				"is_correct":       record.IsCorrect,
				"feedback":         record.Feedback,
				"improvement_note": record.ImprovementNote,
			}).Error
		}
		if findErr != nil && findErr != gorm.ErrRecordNotFound {
			return findErr
		}
		return tx.Create(&record).Error
	})
	if err != nil {
		return repository.QuizEvaluation{}, err
	}
	return r.GetEvaluation(ctx, questionID)
}

func (r *Repository) GetEvaluation(ctx context.Context, questionID string) (repository.QuizEvaluation, error) {
	db, err := r.getDB()
	if err != nil {
		return repository.QuizEvaluation{}, err
	}
	var record repository.QuizEvaluation
	if err := db.WithContext(ctx).
		Where("quiz_question_id = ?", strings.TrimSpace(questionID)).
		First(&record).Error; err != nil {
		return repository.QuizEvaluation{}, err
	}
	return record, nil
}

func (r *Repository) ListEvaluations(ctx context.Context, sessionID string) ([]repository.QuizEvaluation, error) {
	db, err := r.getDB()
	if err != nil {
		return nil, err
	}
	var records []repository.QuizEvaluation
	if err := db.WithContext(ctx).
		Where("quiz_session_id = ?", strings.TrimSpace(sessionID)).
		Order("created_at ASC").
		Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

func (r *Repository) SaveReport(ctx context.Context, sessionID string, overallScore int, summary, strengthsJSON, weaknessesJSON, recommendationsJSON string) (repository.QuizReport, error) {
	db, err := r.getDB()
	if err != nil {
		return repository.QuizReport{}, err
	}
	record := repository.QuizReport{
		ID:                  uuid.NewString(),
		QuizSessionID:       strings.TrimSpace(sessionID),
		OverallScore:        overallScore,
		Summary:             strings.TrimSpace(summary),
		StrengthsJSON:       strings.TrimSpace(strengthsJSON),
		WeaknessesJSON:      strings.TrimSpace(weaknessesJSON),
		RecommendationsJSON: strings.TrimSpace(recommendationsJSON),
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing repository.QuizReport
		findErr := tx.Where("quiz_session_id = ?", record.QuizSessionID).First(&existing).Error
		if findErr == nil {
			record.ID = existing.ID
			return tx.Model(&existing).Updates(map[string]interface{}{
				"overall_score":         record.OverallScore,
				"summary":               record.Summary,
				"strengths_json":        record.StrengthsJSON,
				"weaknesses_json":       record.WeaknessesJSON,
				"recommendations_json":  record.RecommendationsJSON,
			}).Error
		}
		if findErr != nil && findErr != gorm.ErrRecordNotFound {
			return findErr
		}
		return tx.Create(&record).Error
	})
	if err != nil {
		return repository.QuizReport{}, err
	}
	return r.GetReport(ctx, sessionID)
}

func (r *Repository) GetReport(ctx context.Context, sessionID string) (repository.QuizReport, error) {
	db, err := r.getDB()
	if err != nil {
		return repository.QuizReport{}, err
	}
	var record repository.QuizReport
	if err := db.WithContext(ctx).
		Where("quiz_session_id = ?", strings.TrimSpace(sessionID)).
		First(&record).Error; err != nil {
		return repository.QuizReport{}, err
	}
	return record, nil
}

func (r *Repository) UpdateSessionStatus(ctx context.Context, sessionID, status string, completedAt *time.Time) error {
	db, err := r.getDB()
	if err != nil {
		return err
	}
	updates := map[string]interface{}{"status": strings.TrimSpace(status)}
	if completedAt != nil {
		updates["completed_at"] = *completedAt
	}
	return db.WithContext(ctx).Model(&repository.QuizSession{}).
		Where("id = ?", strings.TrimSpace(sessionID)).
		Updates(updates).Error
}

func (r *Repository) UpdateSessionDisplayState(ctx context.Context, sessionID, displayState string) error {
	db, err := r.getDB()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Model(&repository.QuizSession{}).
		Where("id = ?", strings.TrimSpace(sessionID)).
		Update("display_state", strings.TrimSpace(displayState)).Error
}

func (r *Repository) UpdateSessionReportStatus(ctx context.Context, sessionID, reportStatus string) error {
	db, err := r.getDB()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Model(&repository.QuizSession{}).
		Where("id = ?", strings.TrimSpace(sessionID)).
		Update("report_status", strings.TrimSpace(reportStatus)).Error
}

func (r *Repository) CountPendingTopicItems(ctx context.Context, sessionID string) (int64, error) {
	db, err := r.getDB()
	if err != nil {
		return 0, err
	}
	var count int64
	err = db.WithContext(ctx).
		Model(&repository.QuizTopicItem{}).
		Where("quiz_session_id = ? AND status IN ?", strings.TrimSpace(sessionID), []string{"queued", "generating"}).
		Count(&count).Error
	return count, err
}

func (r *Repository) MarkQuestionAnswered(ctx context.Context, questionID string) error {
	db, err := r.getDB()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Model(&repository.QuizQuestion{}).
		Where("id = ?", strings.TrimSpace(questionID)).
		Updates(map[string]interface{}{
			"status":            "answered",
			"evaluation_status": "queued",
		}).Error
}

func (r *Repository) UpdateQuestionEvaluationStatus(ctx context.Context, questionID, evaluationStatus string) error {
	db, err := r.getDB()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Model(&repository.QuizQuestion{}).
		Where("id = ?", strings.TrimSpace(questionID)).
		Update("evaluation_status", strings.TrimSpace(evaluationStatus)).Error
}

func (r *Repository) CountPendingEvaluations(ctx context.Context, sessionID string) (int64, error) {
	db, err := r.getDB()
	if err != nil {
		return 0, err
	}
	var count int64
	err = db.WithContext(ctx).
		Model(&repository.QuizQuestion{}).
		Where("quiz_session_id = ? AND evaluation_status IN ?", strings.TrimSpace(sessionID), []string{"queued", "processing"}).
		Count(&count).Error
	return count, err
}

func (r *Repository) CountUnansweredQuestions(ctx context.Context, sessionID string) (int64, error) {
	db, err := r.getDB()
	if err != nil {
		return 0, err
	}
	var count int64
	err = db.WithContext(ctx).
		Model(&repository.QuizQuestion{}).
		Where("quiz_session_id = ? AND status <> ?", strings.TrimSpace(sessionID), "answered").
		Count(&count).Error
	return count, err
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

func evaluationLevel(score int) string {
	switch {
	case score < 40:
		return "low"
	case score < 75:
		return "mid"
	default:
		return "good"
	}
}
