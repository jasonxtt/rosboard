package containers

import (
	"archive/tar"
	"encoding/json"
	"errors"
	"io"
	"path"
	"strings"
)

const MaxArchiveBytes int64 = 1 << 30
const maxImageJSON int64 = 1 << 20

func OCIArchitecture(architecture string) string {
	switch architecture {
	case "x86_64":
		return "amd64"
	case "arm64":
		return "arm64"
	case "arm":
		return "arm"
	}
	return architecture
}

// InspectArchive reads Docker-save metadata without extracting or running it.
// Single-image tar only: OCI-only, gzip and docker-export lack this manifest.
func InspectArchive(file io.ReadSeeker, name string, bytes int64) (ImageArchive, error) {
	invalid := errors.New("请选择单个 Linux 镜像的 docker save / podman save --format docker-archive 导出的 .tar 文件")
	if bytes <= 0 || bytes > MaxArchiveBytes || !strings.HasSuffix(strings.ToLower(name), ".tar") {
		return ImageArchive{}, invalid
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return ImageArchive{}, invalid
	}
	entries := map[string]int64{}
	var manifest []struct {
		Config   string
		RepoTags []string
		Layers   []string
	}
	reader := tar.NewReader(io.LimitReader(file, MaxArchiveBytes+1))
	for count := 0; ; count++ {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil || count >= 100000 {
			return ImageArchive{}, invalid
		}
		p := strings.TrimPrefix(header.Name, "./")
		if strings.HasPrefix(p, "/") || strings.Contains(p, "\\") || path.Clean(p) != strings.TrimSuffix(p, "/") || p == ".." || strings.HasPrefix(p, "../") {
			return ImageArchive{}, invalid
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size < 0 {
			return ImageArchive{}, invalid
		}
		if _, exists := entries[p]; exists {
			return ImageArchive{}, invalid
		}
		entries[p] = header.Size
		if p == "manifest.json" {
			if header.Size > maxImageJSON {
				return ImageArchive{}, invalid
			}
			data, err := io.ReadAll(io.LimitReader(reader, maxImageJSON+1))
			if err != nil || json.Unmarshal(data, &manifest) != nil {
				return ImageArchive{}, invalid
			}
		}
	}
	if len(manifest) != 1 || manifest[0].Config == "" {
		return ImageArchive{}, invalid
	}
	entry := manifest[0]
	configSize, exists := entries[entry.Config]
	if !exists || configSize > maxImageJSON {
		return ImageArchive{}, invalid
	}
	for _, layer := range entry.Layers {
		if _, exists := entries[layer]; !exists {
			return ImageArchive{}, invalid
		}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return ImageArchive{}, invalid
	}
	reader = tar.NewReader(file)
	var config struct {
		Architecture string
		OS           string
	}
	found := false
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return ImageArchive{}, invalid
		}
		if strings.TrimPrefix(header.Name, "./") == entry.Config {
			data, err := io.ReadAll(io.LimitReader(reader, maxImageJSON+1))
			if err != nil || json.Unmarshal(data, &config) != nil {
				return ImageArchive{}, invalid
			}
			found = true
			break
		}
	}
	if !found || config.OS != "linux" || (config.Architecture != "amd64" && config.Architecture != "arm64" && config.Architecture != "arm") {
		return ImageArchive{}, invalid
	}
	reference := strings.TrimSuffix(path.Base(name), path.Ext(name))
	if len(entry.RepoTags) > 0 && entry.RepoTags[0] != "" {
		reference = entry.RepoTags[0]
	}
	if reference == "" || strings.ContainsAny(reference, " \t\r\n\x00") {
		return ImageArchive{}, invalid
	}
	return ImageArchive{Name: path.Base(name), Reference: reference, Architecture: config.Architecture, Bytes: bytes}, nil
}
