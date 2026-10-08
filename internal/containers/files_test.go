package containers

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"rosboard/internal/routeros"
)

func archiveFixture(t *testing.T, architecture string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	files := map[string]string{"manifest.json": `[{"Config":"config.json","RepoTags":["example/app:v2"],"Layers":["layer.tar"]}]`, "config.json": `{"architecture":"` + architecture + `","os":"linux"}`, "layer.tar": "test opaque layer"}
	for _, name := range []string{"config.json", "layer.tar", "manifest.json"} {
		data := files[name]
		if err := w.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestInspectArchiveAndImageProjection(t *testing.T) {
	raw := archiveFixture(t, "amd64")
	a, err := InspectArchive(bytes.NewReader(raw), "app.tar", int64(len(raw)))
	if err != nil || a.Reference != "example/app:v2" || a.Architecture != "amd64" {
		t.Fatal(a, err)
	}
	s := optionsFixture()
	s.Options.Architecture = "x86_64"
	a.ID = "upload-a"
	a.RemotePath = "/sata1/rosboard/images/upload-a.tar"
	s.Options.Archives = []ImageArchive{a}
	d := draftFixture()
	d.ImageSource = "archive"
	d.ArchiveID = a.ID
	d.Image = "untrusted:client"
	d.ArchiveFile = "/unsafe.tar"
	r := Resolve(d, s)
	if len(r.Errors) > 0 || r.Effective.Image != a.Reference || r.ContainerFields["file"] != a.RemotePath || r.ContainerFields["remote-image"] != "" {
		t.Fatal(r)
	}
	other := optionsFixture()
	other.Options.Architecture = "x86_64"
	if Resolve(d, other).Errors["archiveId"] == "" {
		t.Fatal("cross-device archive accepted")
	}
	s.Options.Architecture = "arm64"
	if Resolve(d, s).Errors["archiveId"] == "" {
		t.Fatal("incompatible image accepted")
	}
	d.ImageSource = "registry"
	r = Resolve(d, s)
	if r.ContainerFields["file"] != "" || r.Effective.ArchiveID != "" {
		t.Fatal("image sources mixed")
	}
	for _, tc := range []struct {
		name string
		data []byte
		size int64
	}{{"export.tar", []byte("not a save image"), 16}, {"app.zip", raw, int64(len(raw))}, {"app.tar", raw, MaxArchiveBytes + 1}} {
		if _, err := InspectArchive(bytes.NewReader(tc.data), tc.name, tc.size); err == nil {
			t.Fatal("invalid archive accepted", tc.name)
		}
	}
}
func TestArchiveRejectsTraversalMissingLayersAndMultipleImages(t *testing.T) {
	for _, manifest := range []string{`[{"Config":"config.json","Layers":["missing.tar"]}]`, `[{"Config":"config.json"},{"Config":"config.json"}]`} {
		var b bytes.Buffer
		w := tar.NewWriter(&b)
		for name, data := range map[string]string{"manifest.json": manifest, "config.json": `{"architecture":"amd64","os":"linux"}`} {
			w.WriteHeader(&tar.Header{Name: name, Size: int64(len(data))})
			w.Write([]byte(data))
		}
		w.Close()
		if _, err := InspectArchive(bytes.NewReader(b.Bytes()), "app.tar", int64(b.Len())); err == nil {
			t.Fatal("invalid manifest accepted")
		}
	}
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	w.WriteHeader(&tar.Header{Name: "../escape", Size: 1})
	w.Write([]byte("a"))
	w.Close()
	if _, err := InspectArchive(bytes.NewReader(b.Bytes()), "app.tar", int64(b.Len())); err == nil {
		t.Fatal("traversal accepted")
	}
}
func TestDirectoryViewNavigatesActualMetadata(t *testing.T) {
	rows := []routeros.RouterOSObject{{"name": "sata1/docker/config", "type": "directory"}, {"name": "sata1/docker/config/app.yaml", "type": ".yaml file", "size": "32"}, {"name": "sata1/docker/rootfs", "type": "directory"}}
	listing, err := DirectoryView(rows, []Disk{{Name: "sata1", Writable: true}}, "/sata1/docker")
	if err != nil || len(listing.Entries) != 2 || listing.Entries[0].Path != "/sata1/docker/config" {
		t.Fatal(listing, err)
	}
	listing, err = DirectoryView(rows, nil, "sata1/docker/config")
	if err != nil || listing.Entries[0].Directory || listing.Entries[0].Bytes != 32 {
		t.Fatal(listing, err)
	}
	for _, p := range []string{"/sata1/../x", "/sata1/./x", "/sata1/\\x", "/sata1/\x00x", "/missing", "/sata1/docker/config/app.yaml"} {
		if _, err := DirectoryView(rows, nil, p); err == nil {
			t.Fatal("invalid directory accepted", p)
		}
	}
}
func TestExistingArchiveConfigurationRemainsIntact(t *testing.T) {
	d := draftFixture()
	d.ExistingID = "*a"
	d.ImageSource = "archive"
	d.ArchiveFile = "sata1/image.tar"
	d.Image = "example:original"
	d.RootDir = "/sata1/existing"
	d.Name = "existing"
	s := optionsFixture()
	s.Items = []Item{{ID: d.ExistingID, Name: d.Name, Image: d.Image, Network: d.Network, Config: d}}
	r := Resolve(d, s)
	if len(r.Errors) > 0 || !reflect.DeepEqual(r.Effective, d) || r.ContainerFields["file"] != d.ArchiveFile {
		out, _ := json.Marshal(r)
		t.Fatal(string(out))
	}
}

func TestStorageDirectoryBoundariesAndExistingRelativePaths(t *testing.T) {
	d, s := draftFixture(), optionsFixture()
	for _, root := range []string{"/sata1", "/", "/sata1/../escape"} {
		d.RootDir = root
		if Resolve(d, s).Errors["rootDir"] == "" {
			t.Fatal("unsafe root accepted", root)
		}
	}
	d.RootDir = "/sata1/app/rootfs"
	d.Mounts = []Mount{{Source: "/sata1/app/rootfs/config", Target: "/etc/app"}}
	if Resolve(d, s).Errors["mounts.0"] == "" {
		t.Fatal("persistent mount inside rootfs accepted")
	}
	d.Mounts[0].Source = "/sata1/app/volumes/config"
	if len(Resolve(d, s).Errors) > 0 {
		t.Fatal(Resolve(d, s).Errors)
	}
	d.ExistingID = "*relative"
	d.Name = "existing"
	d.RootDir = "sata1/docker/rootdir/app"
	d.Mounts[0].Source = "sata1/docker/appdata/app"
	s.Items = []Item{{ID: d.ExistingID, Name: d.Name, Network: d.Network, Config: d}}
	r := Resolve(d, s)
	if len(r.Errors) > 0 || r.Effective.RootDir != d.RootDir || r.Effective.Mounts[0].Source != d.Mounts[0].Source {
		t.Fatal("existing relative paths changed", r)
	}
}
