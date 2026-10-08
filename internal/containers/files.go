package containers

import (
	"context"
	"errors"
	"path"
	"sort"
	"strings"
	"unicode"

	"rosboard/internal/config"
	"rosboard/internal/routeros"
)

// DirectoryPath normalizes RouterOS relative file names to UI absolute paths.
// Reject traversal rather than silently cleaning it into a different location.
func DirectoryPath(value string) (string, error) {
	if value == "" {
		return "/", nil
	}
	if strings.Contains(value, "\\") || strings.ContainsFunc(value, unicode.IsControl) {
		return "", errors.New("invalid directory path")
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." || part == "." {
			return "", errors.New("invalid directory path")
		}
	}
	return path.Clean("/" + strings.Trim(value, "/")), nil
}

func DirectoryView(rows []routeros.RouterOSObject, disks []Disk, parent string) (DirectoryListing, error) {
	parent, err := DirectoryPath(parent)
	if err != nil {
		return DirectoryListing{}, err
	}
	entries := map[string]DirectoryEntry{}
	add := func(value string, directory bool, size int64) {
		p, err := DirectoryPath(value)
		if err != nil || p == "/" {
			return
		}
		entries[p] = DirectoryEntry{Name: path.Base(p), Path: p, Directory: directory, Bytes: size}
		for ancestor := path.Dir(p); ancestor != "/"; ancestor = path.Dir(ancestor) {
			if _, exists := entries[ancestor]; !exists {
				entries[ancestor] = DirectoryEntry{Name: path.Base(ancestor), Path: ancestor, Directory: true}
			}
		}
	}
	for _, row := range rows {
		add(row["name"], row["type"] == "directory" || row["type"] == "disk", byteCount(row["size"]))
	}
	for _, disk := range disks {
		if disk.Writable {
			add(disk.Name, true, 0)
		}
	}
	if parent != "/" {
		entry, ok := entries[parent]
		if !ok || !entry.Directory {
			return DirectoryListing{}, errors.New("directory not found")
		}
	}
	result := DirectoryListing{Path: parent, Entries: []DirectoryEntry{}}
	for _, entry := range entries {
		if path.Dir(entry.Path) == parent {
			result.Entries = append(result.Entries, entry)
		}
	}
	sort.Slice(result.Entries, func(i, j int) bool {
		a, b := result.Entries[i], result.Entries[j]
		if a.Directory != b.Directory {
			return a.Directory
		}
		return a.Name < b.Name
	})
	return result, nil
}
func (s *Service) Directories(ctx context.Context, d config.DeviceConfig, disks []Disk, parent string) (DirectoryListing, error) {
	if _, err := DirectoryPath(parent); err != nil {
		return DirectoryListing{}, err
	}
	rows, err := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerFiles)
	if err != nil {
		return DirectoryListing{}, err
	}
	return DirectoryView(rows, disks, parent)
}
