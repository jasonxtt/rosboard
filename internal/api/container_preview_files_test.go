package api

import (
	"archive/tar"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"testing"

	"rosboard/internal/containers"
	"rosboard/internal/routeros"
)

func simulationFiles(device string) []routeros.RouterOSObject {
	rows := []routeros.RouterOSObject{{"name": "sata1", "type": "disk"}, {"name": "usb1", "type": "disk"}, {"name": "sata1/rosboard", "type": "directory"}, {"name": "sata1/rosboard/containers", "type": "directory"}, {"name": "sata1/shared-config", "type": "directory"}, {"name": "sata1/shared-config/config.yaml", "type": ".yaml file", "size": "1024"}}
	for _, item := range simulationFixture(device).Items {
		rows = append(rows, routeros.RouterOSObject{"name": strings.TrimPrefix(item.Config.RootDir, "/"), "type": "directory"})
	}
	return rows
}
func (m *simulation) mkdir(w http.ResponseWriter, r *http.Request, device string, s containers.Snapshot) {
	var req struct {
		Parent string `json:"parent"`
		Name   string `json:"name"`
	}
	if decodeJSONBody(w, r, &req) != nil {
		return
	}
	listing, err := containers.DirectoryView(m.files[device], s.Options.Disks, req.Parent)
	if err != nil || listing.Path == "/" || req.Name == "" || req.Name == "." || req.Name == ".." || len(req.Name) > 128 || strings.ContainsAny(req.Name, "/\\\x00\r\n") {
		writeAPIError(w, 400, "invalid_directory", "请在磁盘目录中创建文件夹，名称不能包含路径分隔符")
		return
	}
	for _, entry := range listing.Entries {
		if entry.Name == req.Name {
			writeAPIError(w, 409, "directory_exists", "同名文件或目录已存在")
			return
		}
	}
	target := path.Join(listing.Path, req.Name)
	if _, err := containers.DirectoryPath(target); err != nil {
		writeAPIError(w, 400, "invalid_directory", "文件夹名称无效")
		return
	}
	m.files[device] = append(m.files[device], routeros.RouterOSObject{"name": strings.TrimPrefix(target, "/"), "type": "directory"})
	writeJSON(w, 201, map[string]string{"path": target})
}
func (m *simulation) uploadImage(w http.ResponseWriter, r *http.Request, device string, s containers.Snapshot) {
	if len(s.Options.Archives) >= 8 {
		writeAPIError(w, 409, "archive_limit", "最多暂存 8 个镜像，请重置模拟设备后重试")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, containers.MaxArchiveBytes+(1<<20))
	multipart, err := r.MultipartReader()
	if err != nil {
		writeAPIError(w, 400, "invalid_upload", "请选择镜像文件")
		return
	}
	part, err := multipart.NextPart()
	if err != nil || part.FormName() != "file" || part.FileName() == "" {
		writeAPIError(w, 400, "invalid_upload", "缺少镜像文件")
		return
	}
	file, err := os.CreateTemp("", "rosboard-container-image-*.tar")
	if err != nil {
		writeAPIError(w, 500, "staging_failed", "创建镜像暂存文件失败")
		return
	}
	defer os.Remove(file.Name())
	defer file.Close()
	digest := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, digest), io.LimitReader(part, containers.MaxArchiveBytes+1))
	if err != nil || size > containers.MaxArchiveBytes {
		writeAPIError(w, 413, "archive_too_large", "镜像归档不能超过 1 GiB")
		return
	}
	if _, err = multipart.NextPart(); err != io.EOF {
		writeAPIError(w, 400, "invalid_upload", "每次只上传一个镜像文件")
		return
	}
	archive, err := containers.InspectArchive(file, part.FileName(), size)
	if err != nil {
		writeAPIError(w, 400, "invalid_archive", err.Error())
		return
	}
	if archive.Architecture != containers.OCIArchitecture(s.Options.Architecture) {
		writeAPIError(w, 400, "architecture_mismatch", "镜像架构与当前设备不匹配")
		return
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		writeAPIError(w, 500, "staging_failed", "生成镜像标识失败")
		return
	}
	archive.ID = hex.EncodeToString(token[:])
	archive.SHA256 = hex.EncodeToString(digest.Sum(nil))
	var disk *containers.Disk
	for i := range s.Options.Disks {
		d := &s.Options.Disks[i]
		if d.Writable && d.FreeBytes > size && (disk == nil || d.FreeBytes > disk.FreeBytes) {
			disk = d
		}
	}
	if disk == nil {
		writeAPIError(w, 400, "insufficient_storage", "没有足够空闲空间的磁盘")
		return
	}
	archive.RemotePath = path.Join("/", disk.Name, "rosboard", "images", archive.ID+".tar")
	s.Options.Archives = append(s.Options.Archives, archive)
	m.devices[device] = s
	writeJSON(w, 201, archive)
}

func TestSimulationDirectoryMkdirScopeAndCollisions(t *testing.T) {
	m := newSimulation()
	simulationCall(t, m, "POST", "/api/containers/directories?device=demo-router", map[string]string{"parent": "/sata1/rosboard/containers", "name": "app"}, 201)
	raw := simulationCall(t, m, "GET", "/api/containers/directories?device=demo-router&path=/sata1/rosboard/containers", nil, 200)
	if !strings.Contains(string(raw), `"path":"/sata1/rosboard/containers/app"`) {
		t.Fatal(string(raw))
	}
	simulationCall(t, m, "GET", "/api/containers/directories?device=demo-edge&path=/sata1/rosboard/containers/app", nil, 400)
	simulationCall(t, m, "POST", "/api/containers/directories?device=demo-router", map[string]string{"parent": "/sata1/rosboard/containers", "name": "app"}, 409)
	for _, name := range []string{"../escape", "app/nested", ".", ".."} {
		simulationCall(t, m, "POST", "/api/containers/directories?device=demo-router", map[string]string{"parent": "/sata1", "name": name}, 400)
	}
	simulationCall(t, m, "POST", "/api/containers/_reset?device=demo-router", nil, 200)
	simulationCall(t, m, "GET", "/api/containers/directories?device=demo-router&path=/sata1/rosboard/containers/app", nil, 400)
}
func TestSimulationImageUploadAndDeviceScopedCreation(t *testing.T) {
	m := newSimulation()
	var image bytes.Buffer
	archive := tar.NewWriter(&image)
	for name, data := range map[string]string{"manifest.json": `[{"Config":"config.json","RepoTags":["local/app:stable"],"Layers":[]}]`, "config.json": `{"architecture":"amd64","os":"linux"}`} {
		if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		archive.Write([]byte(data))
	}
	archive.Close()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, _ := form.CreateFormFile("file", "image.tar")
	part.Write(image.Bytes())
	form.Close()
	r := httptest.NewRequest("POST", "/api/containers/images/upload?device=demo-router", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var a containers.ImageArchive
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if len(a.SHA256) != 64 || a.Architecture != "amd64" || len(m.devices["demo-edge"].Options.Archives) != 0 {
		t.Fatal(a)
	}
	d := containers.Draft{DraftID: "upload1", ImageSource: "archive", ArchiveID: a.ID, Network: containers.Network{VETH: "local-app", Bridge: "br-containers", Address: "172.20.0.40/24", Gateway: "172.20.0.1"}, StartAfterCreate: true, StartOnBoot: true, Logging: true, RestartPolicy: "no", Health: containers.Health{Mode: "inherit"}}
	simulationCall(t, m, "POST", "/api/containers/actions?device=demo-edge", simulatedRequest{Action: "create", RequestID: "wrong-device", Draft: d}, 400)
	job := simulateFinished(t, m, simulatedRequest{Action: "create", Draft: d})
	items := m.devices["demo-router"].Items
	item := items[len(items)-1]
	if job.State != "succeeded" || item.Config.ArchiveFile != a.RemotePath || item.Image != "local/app:stable" {
		t.Fatal(job, item)
	}
}
