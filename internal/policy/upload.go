package policy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const maxMultipartBytes = MaxSourceBytes + 256<<10

type UploadService struct {
	tempDir string
}

type UploadPreview struct {
	Filename string
	PreparedSourceContent
	// Parse is retained as a compatibility-shaped view; prepared content is
	// still the single producer of the parse result.
	Parse ParseResult
}

func NewUploadService(tempDir string) *UploadService {
	return &UploadService{tempDir: tempDir}
}

// Preview copies an upload to a random 0600 temporary file, validates it with
// the parser matching kind, and removes that file on every exit path. The user
// filename is display metadata only and is never used to construct a
// filesystem path.
func (s *UploadService) Preview(ctx context.Context, filename string, source io.Reader, kind string) (UploadPreview, error) {
	if s == nil || strings.TrimSpace(s.tempDir) == "" {
		return UploadPreview{}, errors.New("upload temp directory is not configured")
	}
	displayName, err := sanitizeUploadFilename(filename)
	if err != nil {
		return UploadPreview{}, err
	}
	if err := os.MkdirAll(s.tempDir, 0o700); err != nil {
		return UploadPreview{}, fmt.Errorf("create upload temp directory: %w", err)
	}
	tempFile, err := os.CreateTemp(s.tempDir, ".rosboard-upload-*")
	if err != nil {
		return UploadPreview{}, fmt.Errorf("create upload temp file: %w", err)
	}
	tempName := tempFile.Name()
	defer os.Remove(tempName)
	if err := tempFile.Chmod(0o600); err != nil {
		tempFile.Close()
		return UploadPreview{}, fmt.Errorf("secure upload temp file: %w", err)
	}
	if source == nil {
		tempFile.Close()
		return UploadPreview{}, errors.New("upload body is nil")
	}
	read := &contextReader{ctx: ctx, reader: io.LimitReader(source, MaxSourceBytes+1)}
	size, err := io.Copy(tempFile, read)
	if closeErr := tempFile.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return UploadPreview{}, fmt.Errorf("write upload temp file: %w", err)
	}
	if size > MaxSourceBytes {
		return UploadPreview{}, fmt.Errorf("upload exceeds %d bytes", MaxSourceBytes)
	}
	body, err := os.ReadFile(tempName)
	if err != nil {
		return UploadPreview{}, fmt.Errorf("read upload temp file: %w", err)
	}
	prepared, err := PrepareSourceContent(body, kind)
	if err != nil {
		return UploadPreview{}, err
	}
	return UploadPreview{
		Filename:              displayName,
		PreparedSourceContent: prepared,
		Parse:                 prepared.ParseResult,
	}, nil
}

// PreviewMultipart accepts the multipart/form-data boundary used by the API
// layer without binding this package to an HTTP server implementation.
func (s *UploadService) PreviewMultipart(ctx context.Context, contentType string, body io.Reader, kind string) (UploadPreview, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return UploadPreview{}, errors.New("upload request must be multipart/form-data")
	}
	if body == nil {
		return UploadPreview{}, errors.New("upload body is nil")
	}
	rawBody, err := io.ReadAll(io.LimitReader(body, maxMultipartBytes+1))
	if err != nil {
		return UploadPreview{}, fmt.Errorf("read multipart upload body: %w", err)
	}
	if len(rawBody) > maxMultipartBytes {
		return UploadPreview{}, fmt.Errorf("multipart upload exceeds %d bytes", maxMultipartBytes)
	}
	reader := multipart.NewReader(bytes.NewReader(rawBody), params["boundary"])
	var preview UploadPreview
	found := false
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return UploadPreview{}, fmt.Errorf("read multipart upload: %w", err)
		}
		filename := part.FileName()
		if filename == "" {
			continue
		}
		if found {
			return UploadPreview{}, errors.New("upload contains more than one file")
		}
		found = true
		preview, err = s.Preview(ctx, filename, part, kind)
		if err != nil {
			return UploadPreview{}, err
		}
	}
	if !found {
		return UploadPreview{}, errors.New("upload request does not contain a file")
	}
	return preview, nil
}

func sanitizeUploadFilename(filename string) (string, error) {
	filename = filepath.Base(strings.ReplaceAll(filename, "\\", "/"))
	filename = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, filename)
	if filename == "." || filename == ".." || strings.TrimSpace(filename) == "" {
		return "", errors.New("upload filename is empty")
	}
	extension := strings.ToLower(filepath.Ext(filename))
	if extension != ".yaml" && extension != ".yml" && extension != ".txt" && extension != ".list" {
		return "", errors.New("upload filename must end in .yaml, .yml, .txt, or .list")
	}
	return filename, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(p)
	}
}
