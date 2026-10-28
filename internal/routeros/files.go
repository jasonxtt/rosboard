package routeros

import (
	"context"
	"errors"
	"net/http"
	"path"
	"strings"
	"unicode"
)

// FilePath is a RouterOS Files path, never a host filesystem path.
func FilePath(value string) (string, error) {
	if len(value) > 1024 || strings.Contains(value, "\\") || strings.ContainsFunc(value, unicode.IsControl) {
		return "", errors.New("invalid directory path")
	}
	for _, part := range strings.Split(value, "/") {
		if part == "." || part == ".." {
			return "", errors.New("invalid directory path")
		}
	}
	return path.Clean("/" + strings.Trim(value, "/")), nil
}

func DirectoryName(name string) error {
	if strings.TrimSpace(name) == "" || name == "." || name == ".." || len(name) > 128 || strings.ContainsAny(name, "/\\") || strings.ContainsFunc(name, unicode.IsControl) {
		return errors.New("invalid directory name")
	}
	return nil
}

// These commands deliberately cannot upload, overwrite or remove ordinary files.
// The service re-reads the target's type, path and opaque ID before every write.
func (c *MutationClient) CreateDirectory(ctx context.Context, name string) error {
	p, err := FilePath(name)
	if err != nil || p == "/" || path.Dir(p) == "/" {
		return errors.New("invalid directory path")
	}
	return c.directoryCommand(ctx, "add", RouterOSFields{"name": strings.TrimPrefix(p, "/"), "type": "directory"})
}

func (c *MutationClient) RenameDirectory(ctx context.Context, id, name string) error {
	if err := validateFileID(id); err != nil {
		return err
	}
	p, err := FilePath(name)
	if err != nil || p == "/" || path.Dir(p) == "/" {
		return errors.New("invalid directory path")
	}
	return c.directoryCommand(ctx, "set", RouterOSFields{"numbers": id, "name": strings.TrimPrefix(p, "/")})
}

func (c *MutationClient) RemoveDirectory(ctx context.Context, id string) error {
	if err := validateFileID(id); err != nil {
		return err
	}
	return c.directoryCommand(ctx, "remove", RouterOSFields{"numbers": id})
}

// Files can use long **opaque IDs, unlike configuration menus' *hex IDs.
// IDs go only into the JSON numbers field and must match fresh server metadata.
func validateFileID(id string) error {
	if len(id) > 256 {
		return errors.New("invalid RouterOS file ID")
	}
	if !strings.HasPrefix(id, "**") {
		return validateMutationID(id)
	}
	if len(id) < 3 {
		return errors.New("invalid RouterOS file ID")
	}
	for _, c := range id[2:] {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("-_+=", c)) {
			return errors.New("invalid RouterOS file ID")
		}
	}
	return nil
}

func (c *MutationClient) directoryCommand(ctx context.Context, command string, fields RouterOSFields) error {
	endpoint := c.baseURL + "/rest/file/" + command
	body, err := c.execute(ctx, http.MethodPost, endpoint, fields, maxMutationJSONBytes, mutationNoRetryMutation)
	if err != nil {
		return err
	}
	if _, err := decodeMutationResponse(body); err != nil {
		return mutationUnknownFor(http.MethodPost, endpoint, 0)
	}
	return nil
}
