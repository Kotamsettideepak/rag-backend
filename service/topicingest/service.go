package topicingest

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"gin-backend/model"
	topicrepo "gin-backend/repository/topic"
	"gin-backend/repository/vector"
	"gin-backend/service/ingestion/embedding"
)

type Request struct {
	TopicID string
	Status  string
	Chunks  []model.Chunk
}

type storeMessage struct {
	TopicID string
	Status  string
	Records []model.VectorRecord
}

type Service struct {
	topics         *topicrepo.Repository
	embedder       *embedding.Service
	store          *vector.Repository
	requests       chan Request
	storeMessages  chan storeMessage
	storeBatchSize int
	wg             sync.WaitGroup
}

var defaultService *Service

func SetDefault(svc *Service) {
	defaultService = svc
}

func Default() *Service {
	return defaultService
}

func NewService(embedder *embedding.Service) *Service {
	s := &Service{
		topics:         topicrepo.Default(),
		embedder:       embedder,
		store:          vector.NewRepository(),
		requests:       make(chan Request, 128),
		storeMessages:  make(chan storeMessage, 128),
		storeBatchSize: 64,
	}
	s.wg.Add(2)
	go s.runEmbedLoop()
	go s.runStoreLoop()
	return s
}

func (s *Service) Shutdown() {
	if s == nil {
		return
	}
	close(s.requests)
	s.wg.Wait()
}

func (s *Service) CreateTopic(ctx context.Context, name string) (string, string, error) {
	record, err := s.topics.Create(ctx, name, "No Context")
	if err != nil {
		return "", "", err
	}
	return record.ID, record.Status, nil
}

func (s *Service) Enqueue(ctx context.Context, req Request) error {
	if s == nil {
		return fmt.Errorf("topic ingest service is not initialized")
	}
	req.TopicID = strings.TrimSpace(req.TopicID)
	req.Status = strings.TrimSpace(req.Status)
	if req.TopicID == "" {
		return fmt.Errorf("topic_id is required")
	}
	if len(req.Chunks) == 0 && req.Status == "" {
		return fmt.Errorf("chunks or status is required")
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.requests <- req:
		return nil
	}
}

func (s *Service) runEmbedLoop() {
	defer s.wg.Done()
	defer close(s.storeMessages)

	for req := range s.requests {
		if req.Status != "" && len(req.Chunks) > 0 {
			if err := s.topics.UpdateStatus(context.Background(), req.TopicID, req.Status); err != nil {
				log.Printf("[topic-ingest] status update failed topic=%s status=%s err=%v", req.TopicID, req.Status, err)
			}
		}

		if len(req.Chunks) == 0 {
			s.storeMessages <- storeMessage{TopicID: req.TopicID, Status: req.Status}
			continue
		}

		records, err := s.embedder.EmbedChunks(context.Background(), req.Chunks)
		if err != nil {
			log.Printf("[topic-ingest] embedding failed topic=%s chunks=%d err=%v", req.TopicID, len(req.Chunks), err)
			_ = s.topics.UpdateStatus(context.Background(), req.TopicID, "Failed")
			continue
		}

		s.storeMessages <- storeMessage{Records: records}
	}
}

func (s *Service) runStoreLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	pending := make([]model.VectorRecord, 0, s.storeBatchSize)

	flush := func() {
		if len(pending) == 0 {
			return
		}
		if err := s.store.AddRecords(pending); err != nil {
			log.Printf("[topic-ingest] vector flush failed records=%d err=%v", len(pending), err)
			for _, topicID := range collectTopicIDs(pending) {
				_ = s.topics.UpdateStatus(context.Background(), topicID, "Failed")
			}
			pending = pending[:0]
			return
		}
		log.Printf("[topic-ingest] vector flush complete records=%d", len(pending))
		pending = pending[:0]
	}

	for {
		select {
		case msg, ok := <-s.storeMessages:
			if !ok {
				flush()
				return
			}
			if msg.Status != "" && len(msg.Records) == 0 {
				flush()
				if err := s.topics.UpdateStatus(context.Background(), msg.TopicID, msg.Status); err != nil {
					log.Printf("[topic-ingest] final status update failed topic=%s status=%s err=%v", msg.TopicID, msg.Status, err)
				}
				continue
			}

			pending = append(pending, msg.Records...)
			if len(pending) >= s.storeBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func collectTopicIDs(records []model.VectorRecord) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(records))
	for _, record := range records {
		topicID := strings.TrimSpace(fmt.Sprintf("%v", record.Metadata["topic_id"]))
		if topicID == "" {
			continue
		}
		if _, ok := seen[topicID]; ok {
			continue
		}
		seen[topicID] = struct{}{}
		out = append(out, topicID)
	}
	return out
}
