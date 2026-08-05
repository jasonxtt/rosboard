package mosdns

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"rosboard/internal/model"
)

type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
}

type AuditAnswer struct {
	Type string `json:"type"`
	TTL  int64  `json:"ttl"`
	Data string `json:"data"`
}

type AuditLog struct {
	ClientIP     string        `json:"client_ip"`
	QueryType    string        `json:"query_type"`
	QueryName    string        `json:"query_name"`
	QueryTime    time.Time     `json:"query_time"`
	TraceID      string        `json:"trace_id"`
	EffectiveTag string        `json:"effective_tag"`
	Answers      []AuditAnswer `json:"answers"`
}

type Pagination struct {
	TotalItems   int `json:"total_items"`
	TotalPages   int `json:"total_pages"`
	CurrentPage  int `json:"current_page"`
	ItemsPerPage int `json:"items_per_page"`
}

type AuditLogsResponse struct {
	Pagination Pagination `json:"pagination"`
	Logs       []AuditLog `json:"logs"`
}

func NewClient(baseURL string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, fmt.Errorf("parse MosDNS URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("MosDNS URL must use http or https")
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("MosDNS URL must include a host")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{baseURL: parsed, httpClient: httpClient}, nil
}

func (c *Client) AuditLogs(ctx context.Context, page, limit int) (AuditLogsResponse, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 200
	}
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/api/v2/audit/logs"
	endpoint.RawQuery = url.Values{
		"page":  []string{strconv.Itoa(page)},
		"limit": []string{strconv.Itoa(limit)},
	}.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return AuditLogsResponse{}, fmt.Errorf("create MosDNS audit request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return AuditLogsResponse{}, fmt.Errorf("request MosDNS audit logs: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return AuditLogsResponse{}, fmt.Errorf("MosDNS audit logs returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	var result AuditLogsResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return AuditLogsResponse{}, fmt.Errorf("decode MosDNS audit logs: %w", err)
	}
	return result, nil
}

func NormalizeClientIP(raw string) string {
	return normalizeIP(raw)
}

func NormalizeAnswerIP(raw string) string {
	return normalizeIP(raw)
}

func NormalizeDomain(raw string) string {
	domain := strings.ToLower(strings.TrimSpace(raw))
	domain = strings.TrimSuffix(domain, ".")
	return domain
}

func ExpandAuditLog(record AuditLog, ingestedAt time.Time) ([]model.DNSObservation, int) {
	clientIP := NormalizeClientIP(record.ClientIP)
	domain := NormalizeDomain(record.QueryName)
	queryType := strings.ToUpper(strings.TrimSpace(record.QueryType))
	if clientIP == "" || domain == "" || record.QueryTime.IsZero() {
		return nil, len(record.Answers)
	}
	traceID := strings.TrimSpace(record.TraceID)
	effectiveTag := strings.TrimSpace(record.EffectiveTag)
	queryTime := record.QueryTime.UTC()
	ingestedAt = ingestedAt.UTC()
	observations := make([]model.DNSObservation, 0, len(record.Answers))
	skipped := 0
	for _, answer := range record.Answers {
		answerType := strings.ToUpper(strings.TrimSpace(answer.Type))
		if answerType != "A" && answerType != "AAAA" {
			skipped++
			continue
		}
		answerIP := NormalizeAnswerIP(answer.Data)
		parsedIP := net.ParseIP(answerIP)
		if parsedIP == nil || (answerType == "A" && parsedIP.To4() == nil) || (answerType == "AAAA" && parsedIP.To4() != nil) {
			skipped++
			continue
		}
		ttl := answer.TTL
		if ttl < 0 {
			ttl = 0
		}
		observation := model.DNSObservation{
			TraceID:      traceID,
			ClientIP:     clientIP,
			Domain:       domain,
			AnswerIP:     answerIP,
			QueryType:    queryType,
			QueryTime:    queryTime,
			TTL:          ttl,
			EffectiveTag: effectiveTag,
			IngestedAt:   ingestedAt,
		}
		observation.DedupeKey = dedupeKey(observation, answerType)
		observations = append(observations, observation)
	}
	return observations, skipped
}

func normalizeIP(raw string) string {
	value := strings.TrimSpace(raw)
	if percent := strings.LastIndexByte(value, '%'); percent >= 0 {
		value = value[:percent]
	}
	parsed := net.ParseIP(strings.Trim(value, "[]"))
	if parsed == nil {
		return ""
	}
	if ipv4 := parsed.To4(); ipv4 != nil {
		return ipv4.String()
	}
	return parsed.String()
}

func dedupeKey(observation model.DNSObservation, answerType string) string {
	if observation.TraceID != "" {
		return "trace:" + observation.TraceID + "|" + observation.AnswerIP
	}
	payload := strings.Join([]string{
		observation.ClientIP,
		observation.Domain,
		observation.QueryType,
		answerType,
		observation.AnswerIP,
		observation.QueryTime.Format(time.RFC3339Nano),
		strconv.FormatInt(observation.TTL, 10),
		observation.EffectiveTag,
	}, "\x00")
	digest := sha256.Sum256([]byte(payload))
	return "hash:" + hex.EncodeToString(digest[:])
}
