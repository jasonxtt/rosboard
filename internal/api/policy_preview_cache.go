package api

import (
	"time"

	"github.com/google/uuid"

	"rosboard/internal/policy"
)

const policyPreviewLifetime = 15 * time.Minute

type policyPreviewEntry struct {
	DeviceID     string
	SourceType   string
	Kind         string
	URL          string
	ETag         string
	LastModified string
	Content      policy.PreparedSourceContent
	ExpiresAt    time.Time
}

func (s *Server) savePolicyPreview(entry policyPreviewEntry) string {
	s.previewMu.Lock()
	defer s.previewMu.Unlock()
	if s.previews == nil {
		s.previews = make(map[string]policyPreviewEntry)
	}
	now := time.Now().UTC()
	for id, candidate := range s.previews {
		if !candidate.ExpiresAt.After(now) {
			delete(s.previews, id)
		}
	}
	id := uuid.NewString()
	entry.ExpiresAt = now.Add(policyPreviewLifetime)
	s.previews[id] = entry
	return id
}

func (s *Server) policyPreview(id, deviceID string) (policyPreviewEntry, bool) {
	s.previewMu.Lock()
	defer s.previewMu.Unlock()
	entry, ok := s.previews[id]
	if !ok || entry.DeviceID != deviceID || !entry.ExpiresAt.After(time.Now().UTC()) {
		if ok {
			delete(s.previews, id)
		}
		return policyPreviewEntry{}, false
	}
	return entry, true
}

func (s *Server) discardPolicyPreview(id string) {
	s.previewMu.Lock()
	defer s.previewMu.Unlock()
	delete(s.previews, id)
}
