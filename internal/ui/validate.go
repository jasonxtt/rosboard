package ui

import (
	"errors"
	"io/fs"
	"regexp"
	"strings"
)

// Validate checks the packaged entry and all emitted JS/CSS files before a
// supervised candidate is allowed to serve or start RouterOS workers.
var entryReference = regexp.MustCompile(`(?:src|href)=["']([^"']+)["']`)

func Validate(assets fs.FS) error {
	index, e := fs.ReadFile(assets, "index.html")
	if e != nil {
		return e
	}
	if !strings.Contains(string(index), "/assets/") {
		return errors.New("missing frontend entry")
	}
	for _, match := range entryReference.FindAllSubmatch(index, -1) {
		path := strings.TrimPrefix(strings.TrimPrefix(string(match[1]), "./"), "/")
		if !strings.HasPrefix(path, "assets/") {
			continue
		}
		data, err := fs.ReadFile(assets, path)
		if err != nil {
			return err
		}
		if len(data) == 0 {
			return errors.New("empty frontend entry asset")
		}
	}
	count := 0
	e = fs.WalkDir(assets, "assets", func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".css") {
			b, e := fs.ReadFile(assets, path)
			if e != nil {
				return e
			}
			if len(b) == 0 {
				return errors.New("empty frontend asset")
			}
			count++
		}
		return nil
	})
	if e != nil {
		return e
	}
	if count < 2 {
		return errors.New("incomplete frontend bundle")
	}
	return nil
}
