package containers

import (
	"context"
	"path"
	"sort"

	"rosboard/internal/config"
	"rosboard/internal/routeros"
)

// DirectoryPath normalizes RouterOS relative file names to UI absolute paths.
// Reject traversal rather than silently cleaning it into a different location.
func DirectoryPath(value string) (string, error) {
	return routeros.FilePath(value)
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
		p, err := DirectoryPath(row["name"])
		if err != nil || p == "/" {
			continue
		}
		entry := entries[p]
		entry.ID = row[".id"]
		entries[p] = entry
	}
	for _, disk := range disks {
		if disk.Writable {
			p, _ := DirectoryPath(disk.Name)
			if _, exists := entries[p]; !exists {
				add(disk.Name, true, 0)
			}
		}
	}
	if parent != "/" {
		entry, ok := entries[parent]
		if !ok || !entry.Directory {
			return DirectoryListing{}, directoryError(404, "directory_not_found", "目录已不存在，请返回上级目录")
		}
	}
	result := DirectoryListing{Path: parent, Entries: []DirectoryEntry{}}
	result.ID = entries[parent].ID
	result.CanCreate = writableDirectory(parent, disks)
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
	listing, err := DirectoryView(rows, disks, parent)
	if err != nil {
		return listing, err
	}
	refs, err := s.directoryReferences(ctx, d)
	if err != nil {
		return listing, err
	}
	for i := range listing.Entries {
		entry := &listing.Entries[i]
		entry.Protected = protectedDirectory(entry.Path, rows, disks, refs)
		if entry.ID == "" && entry.Protected == "" {
			entry.Protected = "目录没有可操作的 RouterOS ID"
		}
	}
	s.directoryMu.Lock()
	for _, record := range s.directoryRecords {
		if record.deviceKey == directoryDeviceKey(d) && (record.result.State == "unknown" || record.processing) && record.result.Path != "" {
			pending := record.result
			listing.Pending = &pending
			break
		}
	}
	s.directoryMu.Unlock()
	return listing, nil
}
