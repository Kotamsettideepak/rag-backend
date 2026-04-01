package quiz

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"gin-backend/client/evaluationservice"
	"gin-backend/client/quizgenerator"
	"gin-backend/model"
	quizrepo "gin-backend/repository/quiz"
	repositoryroot "gin-backend/repository"
	topicrepo "gin-backend/repository/topic"
	"gin-backend/service/ingestion"

	"github.com/google/uuid"
)

const (
	minRequestedTopics       = 5
	defaultQuestionsPerTopic = 5
	defaultMatchLimit        = 24
	minContextMatches        = 3
)

type Service struct {
	repo       *quizrepo.Repository
	topics     *topicrepo.Repository
	generator  *quizgenerator.Client
	evaluator  *evaluationservice.Client
	subsMu     sync.RWMutex
	subs       map[string]map[string]chan SessionDetail
}

type SessionSummary struct {
	ID                    string     `json:"id"`
	TopicID               string     `json:"topic_id"`
	TopicName             string     `json:"topic_name"`
	Status                string     `json:"status"`
	DisplayState          string     `json:"display_state"`
	ReportStatus          string     `json:"report_status"`
	QuestionCountPerTopic int        `json:"question_count_per_topic"`
	RequestedTopicsCount  int        `json:"requested_topics_count"`
	GeneratedQuestions    int        `json:"generated_questions"`
	AnsweredQuestions     int        `json:"answered_questions"`
	StartedAt             time.Time  `json:"started_at"`
	CompletedAt           *time.Time `json:"completed_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type TopicItem struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Status             string   `json:"status"`
	Sequence           int      `json:"sequence"`
	GeneratedQuestions int      `json:"generated_questions"`
	MatchedChapters    []string `json:"matched_chapters"`
	Notes              string   `json:"notes,omitempty"`
}

type QuestionEvaluation struct {
	Score           int    `json:"score"`
	Level           string `json:"level"`
	IsCorrect       bool   `json:"is_correct"`
	Feedback        string `json:"feedback"`
	ImprovementNote string `json:"improvement_note"`
}

type QuestionView struct {
	ID                string              `json:"id"`
	QuizTopicItemID   string              `json:"quiz_topic_item_id"`
	RequestedTopic    string              `json:"requested_topic"`
	Sequence          int                 `json:"sequence"`
	Prompt            string              `json:"prompt"`
	QuestionType      string              `json:"question_type"`
	ChapterName       string              `json:"chapter_name,omitempty"`
	Status            string              `json:"status"`
	EvaluationStatus  string              `json:"evaluation_status"`
	UserAnswer        string              `json:"user_answer,omitempty"`
	ResponseMode      string              `json:"response_mode,omitempty"`
	ElapsedSeconds    int                 `json:"elapsed_seconds,omitempty"`
	SubmittedAt       *time.Time          `json:"submitted_at,omitempty"`
	Evaluation        *QuestionEvaluation `json:"evaluation,omitempty"`
	CorrectAnswer     string              `json:"correct_answer,omitempty"`
	SupportingContext string              `json:"supporting_context,omitempty"`
}

type ReportView struct {
	OverallScore    int      `json:"overall_score"`
	Summary         string   `json:"summary"`
	Strengths       []string `json:"strengths"`
	Weaknesses      []string `json:"weaknesses"`
	Recommendations []string `json:"recommendations"`
}

type SessionDetail struct {
	Session    SessionSummary `json:"session"`
	TopicItems []TopicItem    `json:"topic_items"`
	Questions  []QuestionView `json:"questions"`
	Report     *ReportView    `json:"report,omitempty"`
}

type GeneratedQuestion struct {
	Question         string `json:"question"`
	Type             string `json:"type"`
	Answer           string `json:"answer"`
	ChapterName      string `json:"chapter_name"`
	SupportingContext string `json:"supporting_context"`
}

type GeneratedQuestionsCallback struct {
	QuizID          string              `json:"quiz_id"`
	QuizTopicItemID string              `json:"quiz_topic_item_id"`
	Status          string              `json:"status"`
	Reason          string              `json:"reason"`
	Questions       []GeneratedQuestion `json:"questions"`
}

var defaultService *Service

func NewService() *Service {
	return &Service{
		repo:      quizrepo.Default(),
		topics:    topicrepo.Default(),
		generator: quizgenerator.New(),
		evaluator: evaluationservice.New(),
		subs:      make(map[string]map[string]chan SessionDetail),
	}
}

func SetDefault(s *Service) {
	defaultService = s
}

func Default() *Service {
	if defaultService == nil {
		defaultService = NewService()
	}
	return defaultService
}

func (s *Service) Shutdown() {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	for _, group := range s.subs {
		for _, ch := range group {
			close(ch)
		}
	}
	s.subs = map[string]map[string]chan SessionDetail{}
}

func (s *Service) StartQuiz(ctx context.Context, userID, topicID string, requestedTopics []string) (SessionDetail, error) {
	cleaned := normalizeRequestedTopics(requestedTopics)
	if len(cleaned) < minRequestedTopics {
		return SessionDetail{}, fmt.Errorf("at least %d topics are required", minRequestedTopics)
	}
	if _, err := s.topics.Get(ctx, topicID); err != nil {
		return SessionDetail{}, err
	}
	session, err := s.repo.CreateSession(ctx, userID, topicID, defaultQuestionsPerTopic)
	if err != nil {
		return SessionDetail{}, err
	}
	items, err := s.repo.CreateTopicItems(ctx, session.ID, cleaned)
	if err != nil {
		return SessionDetail{}, err
	}
	for _, item := range items {
		if err := s.dispatchGenerationJob(ctx, session, item); err != nil {
			_ = s.repo.UpdateTopicItemStatus(ctx, item.ID, "failed", err.Error(), "[]", 0)
		}
	}
	return s.GetQuiz(ctx, userID, session.ID, false)
}

func (s *Service) dispatchGenerationJob(ctx context.Context, session repositoryroot.QuizSession, item repositoryroot.QuizTopicItem) error {
	if err := s.repo.UpdateTopicItemStatus(ctx, item.ID, "generating", "", "", -1); err != nil {
		return err
	}
	matches, err := ingestion.DefaultManager().SearchTopicMatches(ctx, strings.TrimSpace(item.Name), session.TopicID, defaultMatchLimit)
	if err != nil {
		return err
	}
	scored := rankMatchesForTopic(item.Name, matches)
	if len(scored) < minContextMatches {
		return s.ReceiveGeneratedQuestions(ctx, GeneratedQuestionsCallback{
			QuizID:          session.ID,
			QuizTopicItemID: item.ID,
			Status:          "insufficient_context",
			Reason:          "Not enough matching context was found for this quiz topic.",
		})
	}
	groupedContext, chapters := buildGroupedContext(scored)
	if strings.TrimSpace(groupedContext) == "" {
		return s.ReceiveGeneratedQuestions(ctx, GeneratedQuestionsCallback{
			QuizID:          session.ID,
			QuizTopicItemID: item.ID,
			Status:          "insufficient_context",
			Reason:          "Matching context was too weak to generate grounded quiz questions.",
		})
	}
	if err := s.repo.UpdateTopicItemStatus(ctx, item.ID, "generating", "", encodeStringArray(chapters), -1); err != nil {
		return err
	}
	return s.generator.SubmitJob(ctx, quizgenerator.Job{
		QuizID:          session.ID,
		TopicID:         session.TopicID,
		QuizTopicItemID: item.ID,
		RequestedTopic:  item.Name,
		GroupedContext:  groupedContext,
		QuestionCount:   session.QuestionCountPerTopic,
	})
}

func (s *Service) ReceiveGeneratedQuestions(ctx context.Context, payload GeneratedQuestionsCallback) error {
	session, err := s.repo.GetSession(ctx, "", payload.QuizID)
	if err != nil {
		return err
	}
	items, err := s.repo.ListTopicItems(ctx, session.ID)
	if err != nil {
		return err
	}
	var topicItem repositoryroot.QuizTopicItem
	found := false
	for _, item := range items {
		if item.ID == payload.QuizTopicItemID {
			topicItem = item
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("quiz topic item not found")
	}

	status := strings.TrimSpace(payload.Status)
	if status == "" {
		status = "ready"
	}
	if status != "ready" {
		if err := s.repo.UpdateTopicItemStatus(ctx, topicItem.ID, status, strings.TrimSpace(payload.Reason), topicItem.MatchedChaptersJSON, 0); err != nil {
			return err
		}
		return s.finalizeGenerationState(ctx, session)
	}

	existingQuestions, err := s.repo.ListQuestions(ctx, session.ID)
	if err != nil {
		return err
	}
	baseSequence := len(existingQuestions)
	records := make([]repositoryroot.QuizQuestion, 0, len(payload.Questions))
	for index, question := range payload.Questions {
		promptText := strings.TrimSpace(question.Question)
		if promptText == "" {
			continue
		}
		qType := strings.TrimSpace(strings.ToLower(question.Type))
		if qType == "" {
			qType = "direct"
		}
		records = append(records, repositoryroot.QuizQuestion{
			ID:                uuid.NewString(),
			QuizSessionID:     session.ID,
			QuizTopicItemID:   topicItem.ID,
			Sequence:          baseSequence + index + 1,
			Prompt:            promptText,
			QuestionType:      qType,
			ChapterName:       strings.TrimSpace(question.ChapterName),
			CorrectAnswer:     strings.TrimSpace(question.Answer),
			SupportingContext: strings.TrimSpace(question.SupportingContext),
			Status:            "generated",
			EvaluationStatus:  "pending",
		})
	}
	if len(records) == 0 {
		if err := s.repo.UpdateTopicItemStatus(ctx, topicItem.ID, "insufficient_context", firstNonEmpty(payload.Reason, "The generated question payload was empty after validation."), topicItem.MatchedChaptersJSON, 0); err != nil {
			return err
		}
		return s.finalizeGenerationState(ctx, session)
	}
	if err := s.repo.SaveQuestions(ctx, records); err != nil {
		return err
	}
	if err := s.repo.UpdateTopicItemStatus(ctx, topicItem.ID, "ready", "", topicItem.MatchedChaptersJSON, len(records)); err != nil {
		return err
	}
	if err := s.repo.UpdateSessionDisplayState(ctx, session.ID, "questions_streaming"); err != nil {
		return err
	}
	if detail, err := s.GetQuiz(ctx, session.UserID, session.ID, true); err == nil {
		s.publish(detail)
	}
	return s.finalizeGenerationState(ctx, session)
}

func (s *Service) finalizeGenerationState(ctx context.Context, session repositoryroot.QuizSession) error {
	pending, err := s.repo.CountPendingTopicItems(ctx, session.ID)
	if err != nil {
		return err
	}
	if pending > 0 {
		return nil
	}
	if err := s.repo.UpdateSessionStatus(ctx, session.ID, "ready", nil); err != nil {
		return err
	}
	if err := s.repo.UpdateSessionDisplayState(ctx, session.ID, "questions_ready"); err != nil {
		return err
	}
	if detail, err := s.GetQuiz(ctx, session.UserID, session.ID, true); err == nil {
		s.publish(detail)
	}
	return nil
}

func (s *Service) SubmitAnswer(ctx context.Context, userID, sessionID, questionID, response, responseMode string, elapsedSeconds int) (QuestionView, error) {
	session, err := s.repo.GetSession(ctx, userID, sessionID)
	if err != nil {
		return QuestionView{}, err
	}
	question, err := s.repo.GetQuestion(ctx, session.ID, questionID)
	if err != nil {
		return QuestionView{}, err
	}
	answer, err := s.repo.SaveAnswer(ctx, session.ID, question.ID, response, responseMode, elapsedSeconds)
	if err != nil {
		return QuestionView{}, err
	}
	if err := s.repo.MarkQuestionAnswered(ctx, question.ID); err != nil {
		return QuestionView{}, err
	}

	evalMatches, err := ingestion.DefaultManager().SearchTopicMatches(ctx, question.Prompt, session.TopicID, 16)
	if err != nil {
		return QuestionView{}, err
	}
	groupedContext, _ := buildGroupedContext(evalMatches)
	if strings.TrimSpace(groupedContext) == "" {
		groupedContext = question.SupportingContext
	}
	items, err := s.repo.ListTopicItems(ctx, session.ID)
	if err != nil {
		return QuestionView{}, err
	}
	requestedTopic := ""
	for _, item := range items {
		if item.ID == question.QuizTopicItemID {
			requestedTopic = item.Name
			break
		}
	}
	if err := s.evaluator.SubmitJob(ctx, evaluationservice.Job{
		QuizID:            session.ID,
		QuestionID:        question.ID,
		TopicID:           session.TopicID,
		RequestedTopic:    requestedTopic,
		Question:          question.Prompt,
		UserAnswer:        answer.Response,
		CorrectAnswer:     question.CorrectAnswer,
		SupportingContext: groupedContext,
	}); err != nil {
		return QuestionView{}, err
	}
	return s.buildQuestionView(ctx, session, question, answer)
}

func (s *Service) buildQuestionView(ctx context.Context, session repositoryroot.QuizSession, question repositoryroot.QuizQuestion, answer repositoryroot.QuizAnswer) (QuestionView, error) {
	items, err := s.repo.ListTopicItems(ctx, session.ID)
	if err != nil {
		return QuestionView{}, err
	}
	requestedTopic := ""
	for _, item := range items {
		if item.ID == question.QuizTopicItemID {
			requestedTopic = item.Name
			break
		}
	}
	view := QuestionView{
		ID:               question.ID,
		QuizTopicItemID:  question.QuizTopicItemID,
		RequestedTopic:   requestedTopic,
		Sequence:         question.Sequence,
		Prompt:           question.Prompt,
		QuestionType:     question.QuestionType,
		ChapterName:      question.ChapterName,
		Status:           "answered",
		EvaluationStatus: "queued",
		UserAnswer:       answer.Response,
		ResponseMode:     answer.ResponseMode,
		ElapsedSeconds:   answer.ElapsedSeconds,
		SubmittedAt:      &answer.SubmittedAt,
	}
	if detail, err := s.GetQuiz(ctx, session.UserID, session.ID, true); err == nil {
		s.publish(detail)
	}
	return view, nil
}

func (s *Service) CompleteQuiz(ctx context.Context, userID, sessionID string) (SessionDetail, error) {
	session, err := s.repo.GetSession(ctx, userID, sessionID)
	if err != nil {
		return SessionDetail{}, err
	}
	unanswered, err := s.repo.CountUnansweredQuestions(ctx, session.ID)
	if err != nil {
		return SessionDetail{}, err
	}
	if unanswered > 0 {
		return SessionDetail{}, fmt.Errorf("all quiz questions must be answered before completing the quiz")
	}
	pendingEvaluations, err := s.repo.CountPendingEvaluations(ctx, session.ID)
	if err != nil {
		return SessionDetail{}, err
	}
	if pendingEvaluations > 0 {
		return SessionDetail{}, fmt.Errorf("quiz evaluation is still in progress")
	}
	questions, err := s.repo.ListQuestions(ctx, session.ID)
	if err != nil {
		return SessionDetail{}, err
	}
	evaluations, err := s.repo.ListEvaluations(ctx, session.ID)
	if err != nil {
		return SessionDetail{}, err
	}
	items, err := s.repo.ListTopicItems(ctx, session.ID)
	if err != nil {
		return SessionDetail{}, err
	}
	report := buildReport(items, questions, evaluations)
	if _, err := s.repo.SaveReport(ctx, session.ID, report.OverallScore, report.Summary, encodeStringArray(report.Strengths), encodeStringArray(report.Weaknesses), encodeStringArray(report.Recommendations)); err != nil {
		return SessionDetail{}, err
	}
	if err := s.repo.UpdateSessionReportStatus(ctx, session.ID, "ready"); err != nil {
		return SessionDetail{}, err
	}
	now := time.Now().UTC()
	if err := s.repo.UpdateSessionStatus(ctx, session.ID, "completed", &now); err != nil {
		return SessionDetail{}, err
	}
	detail, err := s.GetQuiz(ctx, userID, session.ID, true)
	if err == nil {
		s.publish(detail)
	}
	return detail, err
}

func (s *Service) GetQuiz(ctx context.Context, userID, sessionID string, includeAnswers bool) (SessionDetail, error) {
	session, err := s.repo.GetSession(ctx, userID, sessionID)
	if err != nil {
		return SessionDetail{}, err
	}
	topic, err := s.topics.Get(ctx, session.TopicID)
	if err != nil {
		return SessionDetail{}, err
	}
	items, err := s.repo.ListTopicItems(ctx, session.ID)
	if err != nil {
		return SessionDetail{}, err
	}
	questions, err := s.repo.ListQuestions(ctx, session.ID)
	if err != nil {
		return SessionDetail{}, err
	}
	answers, err := s.repo.ListAnswers(ctx, session.ID)
	if err != nil {
		return SessionDetail{}, err
	}
	evaluations, err := s.repo.ListEvaluations(ctx, session.ID)
	if err != nil {
		return SessionDetail{}, err
	}
	answerByQuestion := make(map[string]repositoryroot.QuizAnswer, len(answers))
	for _, answer := range answers {
		answerByQuestion[answer.QuizQuestionID] = answer
	}
	evalByQuestion := make(map[string]repositoryroot.QuizEvaluation, len(evaluations))
	for _, evaluation := range evaluations {
		evalByQuestion[evaluation.QuizQuestionID] = evaluation
	}
	itemByID := make(map[string]repositoryroot.QuizTopicItem, len(items))
	for _, item := range items {
		itemByID[item.ID] = item
	}
	questionViews := make([]QuestionView, 0, len(questions))
	for _, question := range questions {
		view := QuestionView{
			ID:               question.ID,
			QuizTopicItemID:  question.QuizTopicItemID,
			RequestedTopic:   itemByID[question.QuizTopicItemID].Name,
			Sequence:         question.Sequence,
			Prompt:           question.Prompt,
			QuestionType:     question.QuestionType,
			ChapterName:      question.ChapterName,
			Status:           question.Status,
			EvaluationStatus: question.EvaluationStatus,
		}
		if answer, ok := answerByQuestion[question.ID]; ok {
			view.UserAnswer = answer.Response
			view.ResponseMode = answer.ResponseMode
			view.ElapsedSeconds = answer.ElapsedSeconds
			view.SubmittedAt = &answer.SubmittedAt
		}
		if evaluation, ok := evalByQuestion[question.ID]; ok {
			view.Evaluation = &QuestionEvaluation{
				Score:           evaluation.Score,
				Level:           evaluation.Level,
				IsCorrect:       evaluation.IsCorrect,
				Feedback:        evaluation.Feedback,
				ImprovementNote: evaluation.ImprovementNote,
			}
		}
		if includeAnswers || session.Status == "completed" {
			view.CorrectAnswer = question.CorrectAnswer
			view.SupportingContext = question.SupportingContext
		}
		questionViews = append(questionViews, view)
	}
	itemViews := make([]TopicItem, 0, len(items))
	for _, item := range items {
		itemViews = append(itemViews, TopicItem{
			ID:                 item.ID,
			Name:               item.Name,
			Status:             item.Status,
			Sequence:           item.Sequence,
			GeneratedQuestions: item.GeneratedQuestions,
			MatchedChapters:    decodeStringArray(item.MatchedChaptersJSON),
			Notes:              item.Notes,
		})
	}
	summary := SessionSummary{
		ID:                    session.ID,
		TopicID:               session.TopicID,
		TopicName:             topic.Name,
		Status:                session.Status,
		DisplayState:          session.DisplayState,
		ReportStatus:          session.ReportStatus,
		QuestionCountPerTopic: session.QuestionCountPerTopic,
		RequestedTopicsCount:  len(items),
		GeneratedQuestions:    len(questions),
		AnsweredQuestions:     len(answers),
		StartedAt:             session.StartedAt,
		CompletedAt:           session.CompletedAt,
		CreatedAt:             session.CreatedAt,
		UpdatedAt:             session.UpdatedAt,
	}
	var report *ReportView
	if session.ReportStatus == "ready" || session.Status == "completed" {
		if record, err := s.repo.GetReport(ctx, session.ID); err == nil {
			report = &ReportView{
				OverallScore:    record.OverallScore,
				Summary:         record.Summary,
				Strengths:       decodeStringArray(record.StrengthsJSON),
				Weaknesses:      decodeStringArray(record.WeaknessesJSON),
				Recommendations: decodeStringArray(record.RecommendationsJSON),
			}
		}
	}
	return SessionDetail{Session: summary, TopicItems: itemViews, Questions: questionViews, Report: report}, nil
}

func (s *Service) ListHistory(ctx context.Context, userID, topicID string, limit int) ([]SessionSummary, error) {
	sessions, err := s.repo.ListSessionsByTopic(ctx, userID, topicID, limit)
	if err != nil {
		return nil, err
	}
	topic, err := s.topics.Get(ctx, topicID)
	if err != nil {
		return nil, err
	}
	out := make([]SessionSummary, 0, len(sessions))
	for _, session := range sessions {
		items, _ := s.repo.ListTopicItems(ctx, session.ID)
		questions, _ := s.repo.ListQuestions(ctx, session.ID)
		answers, _ := s.repo.ListAnswers(ctx, session.ID)
		out = append(out, SessionSummary{
			ID:                    session.ID,
			TopicID:               session.TopicID,
			TopicName:             topic.Name,
			Status:                session.Status,
			DisplayState:          session.DisplayState,
			ReportStatus:          session.ReportStatus,
			QuestionCountPerTopic: session.QuestionCountPerTopic,
			RequestedTopicsCount:  len(items),
			GeneratedQuestions:    len(questions),
			AnsweredQuestions:     len(answers),
			StartedAt:             session.StartedAt,
			CompletedAt:           session.CompletedAt,
			CreatedAt:             session.CreatedAt,
			UpdatedAt:             session.UpdatedAt,
		})
	}
	return out, nil
}

func (s *Service) Subscribe(sessionID string) (<-chan SessionDetail, func()) {
	ch := make(chan SessionDetail, 8)
	subID := uuid.NewString()
	s.subsMu.Lock()
	if _, exists := s.subs[sessionID]; !exists {
		s.subs[sessionID] = make(map[string]chan SessionDetail)
	}
	s.subs[sessionID][subID] = ch
	s.subsMu.Unlock()
	unsubscribe := func() {
		s.subsMu.Lock()
		defer s.subsMu.Unlock()
		group, exists := s.subs[sessionID]
		if !exists {
			return
		}
		channel, exists := group[subID]
		if !exists {
			return
		}
		delete(group, subID)
		close(channel)
		if len(group) == 0 {
			delete(s.subs, sessionID)
		}
	}
	return ch, unsubscribe
}

func (s *Service) publish(detail SessionDetail) {
	s.subsMu.RLock()
	group := s.subs[detail.Session.ID]
	channels := make([]chan SessionDetail, 0, len(group))
	for _, ch := range group {
		channels = append(channels, ch)
	}
	s.subsMu.RUnlock()
	for _, ch := range channels {
		select {
		case ch <- detail:
		default:
		}
	}
}

func rankMatchesForTopic(topicName string, matches []model.SearchMatch) []model.SearchMatch {
	normalizedTopic := strings.ToLower(strings.TrimSpace(topicName))
	if normalizedTopic == "" {
		return matches
	}
	type scoredMatch struct {
		match model.SearchMatch
		score float64
	}
	scored := make([]scoredMatch, 0, len(matches))
	for _, match := range matches {
		score := match.Score
		sectionTitle := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", match.Metadata["section_title"])))
		fileName := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", match.Metadata["file_name"])))
		document := strings.ToLower(match.Document)
		if strings.Contains(sectionTitle, normalizedTopic) {
			score += 0.3
		}
		if strings.Contains(fileName, normalizedTopic) {
			score += 0.15
		}
		if strings.Contains(document, normalizedTopic) {
			score += 0.1
		}
		scored = append(scored, scoredMatch{match: match, score: score})
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	out := make([]model.SearchMatch, 0, len(scored))
	for _, item := range scored {
		if strings.TrimSpace(item.match.Document) == "" {
			continue
		}
		item.match.Score = item.score
		out = append(out, item.match)
	}
	return out
}

func buildGroupedContext(matches []model.SearchMatch) (string, []string) {
	if len(matches) == 0 {
		return "", nil
	}
	grouped := make(map[string][]string)
	order := make([]string, 0)
	for _, match := range matches {
		chapter := strings.TrimSpace(fmt.Sprintf("%v", match.Metadata["section_title"]))
		if chapter == "" || chapter == "<nil>" {
			chapter = "General context"
		}
		if _, exists := grouped[chapter]; !exists {
			order = append(order, chapter)
		}
		page := strings.TrimSpace(fmt.Sprintf("%v", match.Metadata["page"]))
		prefix := ""
		if page != "" && page != "<nil>" && page != "0" {
			prefix = fmt.Sprintf("[p.%s] ", page)
		}
		grouped[chapter] = append(grouped[chapter], prefix+strings.TrimSpace(match.Document))
	}
	sections := make([]string, 0, len(order))
	for _, chapter := range order {
		parts := grouped[chapter]
		if len(parts) > 4 {
			parts = parts[:4]
		}
		sections = append(sections, fmt.Sprintf("Chapter: %s\n%s", chapter, strings.Join(parts, "\n\n")))
	}
	return strings.Join(sections, "\n\n"), order
}

func buildReport(items []repositoryroot.QuizTopicItem, questions []repositoryroot.QuizQuestion, evaluations []repositoryroot.QuizEvaluation) ReportView {
	itemByID := make(map[string]repositoryroot.QuizTopicItem, len(items))
	for _, item := range items {
		itemByID[item.ID] = item
	}
	questionByID := make(map[string]repositoryroot.QuizQuestion, len(questions))
	for _, question := range questions {
		questionByID[question.ID] = question
	}
	perTopicScores := make(map[string][]int)
	total := 0
	for _, evaluation := range evaluations {
		question, exists := questionByID[evaluation.QuizQuestionID]
		if !exists {
			continue
		}
		name := itemByID[question.QuizTopicItemID].Name
		perTopicScores[name] = append(perTopicScores[name], evaluation.Score)
		total += evaluation.Score
	}
	overallScore := 0
	if len(evaluations) > 0 {
		overallScore = total / len(evaluations)
	}
	strengths := make([]string, 0)
	weaknesses := make([]string, 0)
	recommendations := make([]string, 0)
	keys := make([]string, 0, len(perTopicScores))
	for key := range perTopicScores {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, name := range keys {
		average := averageScore(perTopicScores[name])
		switch {
		case average >= 75:
			strengths = append(strengths, fmt.Sprintf("%s (good)", name))
		case average >= 40:
			weaknesses = append(weaknesses, fmt.Sprintf("%s (mid)", name))
			recommendations = append(recommendations, fmt.Sprintf("Practice %s again to improve answer precision and completeness.", name))
		default:
			weaknesses = append(weaknesses, fmt.Sprintf("%s (low)", name))
			recommendations = append(recommendations, fmt.Sprintf("Revisit %s from the topic material and retry related questions.", name))
		}
	}
	if len(strengths) == 0 {
		strengths = []string{"Keep revising the selected topics to build stronger confidence."}
	}
	if len(weaknesses) == 0 {
		weaknesses = []string{"No major weak areas detected in this attempt."}
	}
	if len(recommendations) == 0 {
		recommendations = []string{"Repeat the quiz later to keep these topics fresh."}
	}
	summary := fmt.Sprintf("You completed the quiz with an overall score of %d/100.", overallScore)
	return ReportView{OverallScore: overallScore, Summary: summary, Strengths: strengths, Weaknesses: weaknesses, Recommendations: recommendations}
}

func averageScore(values []int) int {
	if len(values) == 0 {
		return 0
	}
	total := 0
	for _, value := range values {
		total += value
	}
	return total / len(values)
}

func normalizeRequestedTopics(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		cleaned := strings.TrimSpace(value)
		if cleaned == "" {
			continue
		}
		key := strings.ToLower(cleaned)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, cleaned)
	}
	return out
}

func encodeStringArray(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func decodeStringArray(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var parsed []string
	if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
		return parsed
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
