package vector

func normalizeWhereClause(where map[string]interface{}) interface{} {
	if len(where) == 0 {
		return nil
	}
	if len(where) == 1 {
		for key, value := range where {
			return map[string]interface{}{key: map[string]interface{}{"$eq": value}}
		}
	}
	clauses := make([]map[string]interface{}, 0, len(where))
	for key, value := range where {
		clauses = append(clauses, map[string]interface{}{key: map[string]interface{}{"$eq": value}})
	}
	return map[string]interface{}{"$and": clauses}
}
