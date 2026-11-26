package autonomous

// LatestBySourceLine returns the newest question the daemon itself raised as
// askedBy for the source line, while it is open or answered. A withdrawn or
// expired one does not count, so it can be asked again.
func (s *QuestionStore) LatestBySourceLine(askedBy, sourceLineSHA256 string) (Question, bool) {
	if sourceLineSHA256 == "" {
		return Question{}, false
	}
	var id string
	s.mu.Lock()
	var newest *Question
	for _, q := range s.questions {
		if q.SourceLineSHA256 != sourceLineSHA256 || q.AskedBy != askedBy || q.AskerKind != QuestionAskerSystem ||
			(q.State != QuestionOpen && q.State != QuestionAnswered) {
			continue
		}
		if newest == nil || (q.CreatedAt != nil && newest.CreatedAt != nil && q.CreatedAt.After(*newest.CreatedAt)) {
			newest = q
		}
	}
	if newest != nil {
		id = newest.ID
	}
	s.mu.Unlock()
	if id == "" {
		return Question{}, false
	}
	return s.Get(id)
}
