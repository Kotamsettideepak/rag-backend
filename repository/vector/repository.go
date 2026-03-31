package vector

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"gin-backend/model"
	"gin-backend/repository"

	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository() *Repository {
	return &Repository{db: repository.DefaultGorm()}
}

func (r *Repository) AddRecords(records []model.VectorRecord) error {
	if len(records) == 0 {
		return nil
	}

	db, err := r.getDB()
	if err != nil {
		return err
	}

	return db.Transaction(func(tx *gorm.DB) error {
		for _, record := range records {
			metadataJSON, err := json.Marshal(record.Metadata)
			if err != nil {
				return err
			}

			if err := tx.Exec(`
				INSERT INTO context_vectors (
					id, chat_id, user_id, file_id, file_name, file_kind,
					chunk_type, section_title, code_language, page_from, page_to, has_formula, picture_class,
					page, chunk_index, hash, document, metadata, embedding
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?::vector)
				ON CONFLICT DO NOTHING
			`,
				record.ID,
				stringMeta(record.Metadata, "chat_id"),
				stringMeta(record.Metadata, "user_id"),
				stringMeta(record.Metadata, "file_id"),
				stringMeta(record.Metadata, "file_name"),
				stringMeta(record.Metadata, "file_kind"),
				stringMetaWithDefault(record.Metadata, "chunk_type", "text"),
				stringMeta(record.Metadata, "section_title"),
				stringMeta(record.Metadata, "code_language"),
				intMetaNullable(record.Metadata, "page_from"),
				intMetaNullable(record.Metadata, "page_to"),
				boolMeta(record.Metadata, "has_formula"),
				stringMeta(record.Metadata, "picture_class"),
				intMeta(record.Metadata, "page"),
				intMeta(record.Metadata, "chunk_index"),
				stringMeta(record.Metadata, "hash"),
				record.Text,
				string(metadataJSON),
				vectorLiteral(record.Embedding),
			).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repository) Search(embedding []float64, nResults int, where map[string]interface{}) ([]model.SearchMatch, error) {
	db, err := r.getDB()
	if err != nil {
		return nil, err
	}
	if nResults <= 0 {
		nResults = 10
	}

	query := `
		SELECT id, document, metadata, (1 - (embedding <=> ?::vector)) AS score
		FROM context_vectors
	`
	args := []interface{}{vectorLiteral(embedding)}
	whereSQL, whereArgs := buildWhereClause(where)
	if whereSQL != "" {
		query += " WHERE " + whereSQL
		args = append(args, whereArgs...)
	}
	query += " ORDER BY embedding <=> ?::vector LIMIT ?"
	args = append(args, vectorLiteral(embedding), nResults)

	rows, err := db.Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanMatches(rows)
}

func (r *Repository) GetByMetadata(where map[string]interface{}, limit int) ([]model.SearchMatch, error) {
	db, err := r.getDB()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT id, document, metadata, 0 AS score
		FROM context_vectors
	`
	args := []interface{}{}
	whereSQL, whereArgs := buildWhereClause(where)
	if whereSQL != "" {
		query += " WHERE " + whereSQL
		args = append(args, whereArgs...)
	}
	query += " ORDER BY created_at ASC LIMIT ?"
	args = append(args, limit)

	rows, err := db.Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanMatches(rows)
}

func (r *Repository) DeleteByMetadata(where map[string]interface{}) error {
	db, err := r.getDB()
	if err != nil {
		return err
	}
	whereSQL, args := buildWhereClause(where)
	if whereSQL == "" {
		return nil
	}
	return db.Exec("DELETE FROM context_vectors WHERE "+whereSQL, args...).Error
}

func (r *Repository) ClearCollection() error {
	db, err := r.getDB()
	if err != nil {
		return err
	}
	return db.Exec("TRUNCATE TABLE context_vectors").Error
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

func buildWhereClause(where map[string]interface{}) (string, []interface{}) {
	if len(where) == 0 {
		return "", nil
	}

	clauses := make([]string, 0, len(where))
	args := make([]interface{}, 0, len(where))
	for key, value := range where {
		switch key {
		case "chat_id", "user_id", "file_id", "file_name", "file_kind", "hash":
			clauses = append(clauses, fmt.Sprintf("%s = ?", key))
			args = append(args, fmt.Sprintf("%v", value))
		case "page", "chunk_index":
			clauses = append(clauses, fmt.Sprintf("%s = ?", key))
			args = append(args, value)
		default:
			clauses = append(clauses, "metadata ->> ? = ?")
			args = append(args, key, fmt.Sprintf("%v", value))
		}
	}
	return strings.Join(clauses, " AND "), args
}

func scanMatches(rows *sql.Rows) ([]model.SearchMatch, error) {
	matches := make([]model.SearchMatch, 0)
	for rows.Next() {
		var (
			id       string
			document string
			rawMeta  []byte
			score    sql.NullFloat64
		)
		if err := rows.Scan(&id, &document, &rawMeta, &score); err != nil {
			return nil, err
		}
		metadata := map[string]interface{}{}
		if len(rawMeta) > 0 {
			if err := json.Unmarshal(rawMeta, &metadata); err != nil {
				return nil, err
			}
		}
		matches = append(matches, model.SearchMatch{
			ID:       id,
			Document: document,
			Metadata: metadata,
			Score:    nullFloat(score),
		})
	}
	return matches, rows.Err()
}

func vectorLiteral(values []float64) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%g", value))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func stringMeta(metadata map[string]interface{}, key string) interface{} {
	value := strings.TrimSpace(fmt.Sprintf("%v", metadata[key]))
	if value == "" || value == "<nil>" {
		return nil
	}
	return value
}

func stringMetaWithDefault(metadata map[string]interface{}, key string, fallback string) interface{} {
	value := stringMeta(metadata, key)
	if value == nil {
		return fallback
	}
	return value
}

func intMeta(metadata map[string]interface{}, key string) int {
	value := metadata[key]
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func intMetaNullable(metadata map[string]interface{}, key string) interface{} {
	value := metadata[key]
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return nil
	}
}

func boolMeta(metadata map[string]interface{}, key string) bool {
	value, ok := metadata[key]
	if !ok || value == nil {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		l := strings.TrimSpace(strings.ToLower(typed))
		return l == "true" || l == "1" || l == "yes"
	default:
		return false
	}
}

func nullFloat(v sql.NullFloat64) float64 {
	if !v.Valid {
		return 0
	}
	return v.Float64
}
