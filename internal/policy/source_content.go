package policy

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// PreparedSourceContent is the shared boundary between URL and upload
// sources. It is created only after size, encoding, YAML structure, and Clash
// rule validation have succeeded.
type PreparedSourceContent struct {
	Size           int64
	SHA256         string
	CompressedYAML []byte
	ParseResult
}

type SourcePreview struct {
	PreparedSourceContent
	URL          string
	StatusCode   int
	ETag         string
	LastModified string
	ContentType  string
	NotModified  bool
}

// PrepareSourceContent validates raw URL/upload content and parses it with the
// parser matching the source kind ("domain" or "ip"); an empty kind reads as
// domain for backward compatibility.
func PrepareSourceContent(body []byte, kind string) (PreparedSourceContent, error) {
	if len(body) > MaxSourceBytes {
		return PreparedSourceContent{}, fmt.Errorf("source exceeds %d bytes", MaxSourceBytes)
	}
	if len(body) == 0 {
		return PreparedSourceContent{}, errors.New("source body is empty")
	}
	if bytes.IndexByte(body, 0) >= 0 {
		return PreparedSourceContent{}, errors.New("source body contains binary NUL bytes")
	}
	var parsed ParseResult
	var err error
	if kind == KindIP {
		parsed, err = ParseClashYAMLIP(body)
	} else {
		parsed, err = ParseClashYAML(body)
	}
	if err != nil {
		return PreparedSourceContent{}, err
	}
	compressed, err := gzipYAML(body)
	if err != nil {
		return PreparedSourceContent{}, err
	}
	return PreparedSourceContent{
		Size:           int64(len(body)),
		SHA256:         parsed.SHA256,
		CompressedYAML: compressed,
		ParseResult:    parsed,
	}, nil
}

func (prepared PreparedSourceContent) PendingVersion(deviceID, sourceID, versionID string) (SourceVersion, []SourceRule, error) {
	if deviceID == "" || sourceID == "" || versionID == "" {
		return SourceVersion{}, nil, errors.New("pending version identity is incomplete")
	}
	if prepared.SHA256 == "" || len(prepared.Rules) == 0 || len(prepared.CompressedYAML) == 0 {
		return SourceVersion{}, nil, errors.New("source content is not prepared")
	}
	counts := map[string]int{"valid": len(prepared.Rules)}
	for category, count := range prepared.Ignored {
		counts[category] = count
	}
	countsJSON, err := json.Marshal(counts)
	if err != nil {
		return SourceVersion{}, nil, fmt.Errorf("encode source counts: %w", err)
	}
	rules := make([]SourceRule, len(prepared.Rules))
	for i, rule := range prepared.Rules {
		rules[i] = SourceRule{DeviceID: deviceID, VersionID: versionID, RuleType: string(rule.Type), Domain: rule.Domain}
	}
	return SourceVersion{
		DeviceID:       deviceID,
		ID:             versionID,
		SourceID:       sourceID,
		SHA256:         prepared.SHA256,
		CompressedYAML: append([]byte(nil), prepared.CompressedYAML...),
		CountsJSON:     countsJSON,
		State:          "pending",
		CreatedAt:      time.Now().UTC(),
	}, rules, nil
}

func (preview SourcePreview) PendingVersion(deviceID, sourceID, versionID string) (SourceVersion, []SourceRule, error) {
	if preview.NotModified {
		return SourceVersion{}, nil, errors.New("not-modified source has no pending version")
	}
	return preview.PreparedSourceContent.PendingVersion(deviceID, sourceID, versionID)
}

func gzipYAML(body []byte) ([]byte, error) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(body); err != nil {
		return nil, fmt.Errorf("compress source YAML: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("finish compressed source YAML: %w", err)
	}
	return compressed.Bytes(), nil
}
