package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type Paths struct{ Binary, Config, Data, State, Backup string }

func NewPaths(binary, config, data string) (Paths, error) {
	var p Paths
	var e error
	if p.Binary, e = filepath.Abs(binary); e != nil {
		return p, e
	}
	if p.Config, e = filepath.Abs(config); e != nil {
		return p, e
	}
	if p.Data, e = filepath.Abs(data); e != nil {
		return p, e
	}
	p.State = filepath.Join(filepath.Dir(p.Binary), ".rosboard-update")
	p.Backup = os.Getenv("ROSBOARD_UPDATE_BACKUP_DIR")
	if p.Backup == "" {
		p.Backup = filepath.Join(p.State, "backup")
	}
	p.Backup, e = filepath.Abs(p.Backup)
	return p, e
}
func within(path, dir string) bool {
	r, e := filepath.Rel(dir, path)
	return e == nil && (r == "." || r != ".." && !filepath.IsAbs(r) && !strings.HasPrefix(r, ".."+string(filepath.Separator)))
}
func noLinks(path string) error {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		st, e := os.Lstat(current)
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if e == nil && st.Mode()&os.ModeSymlink != 0 {
			return errors.New("update paths must not contain symbolic links")
		}
		if current == filepath.Dir(current) {
			break
		}
	}
	return nil
}
func (p Paths) validate() error {
	for _, s := range []string{p.Binary, p.Config, p.Data, p.State, p.Backup} {
		if e := noLinks(s); e != nil {
			return e
		}
	}
	if p.Data == "/" || filepath.Dir(p.Data) == "/" || within(p.Binary, p.Data) || within(p.State, p.Data) || within(p.Backup, p.Data) || within(p.Data, p.Backup) || within(p.Config, p.State) || within(p.Config, p.Backup) || within(p.Binary, p.Backup) {
		return errors.New("安装、数据和备份目录重叠或不安全")
	}
	st, e := os.Stat(p.Binary)
	if e != nil {
		return e
	}
	if !st.Mode().IsRegular() {
		return errors.New("executable is not a regular file")
	}
	return nil
}
func writeJSON(path string, value any) error {
	b, e := json.Marshal(value)
	if e != nil {
		return e
	}
	return atomicWrite(path, b, 0600)
}
func atomicWrite(path string, b []byte, mode fs.FileMode) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".update-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	if e = os.Rename(name, path); e != nil {
		return e
	}
	return syncDir(filepath.Dir(path))
}
func syncDir(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func readJob(p Paths) (*Job, error) {
	b, e := os.ReadFile(filepath.Join(p.State, "job.json"))
	if errors.Is(e, os.ErrNotExist) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var j Job
	if e = json.Unmarshal(b, &j); e != nil {
		return nil, e
	}
	if j.ID == "" {
		return nil, errors.New("invalid update journal")
	}
	return &j, nil
}
func saveJob(p Paths, j *Job) error { return writeJSON(filepath.Join(p.State, "job.json"), j) }
func copyFile(src, dst string, mode fs.FileMode) error {
	s, e := os.Open(src)
	if e != nil {
		return e
	}
	defer s.Close()
	d, e := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if e != nil {
		return e
	}
	_, e = io.Copy(d, s)
	if e == nil {
		e = d.Sync()
	}
	ce := d.Close()
	if e != nil {
		return e
	}
	return ce
}
func copyTree(src, dst string) error {
	if e := os.MkdirAll(dst, 0700); e != nil {
		return e
	}
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(src, path)
		if e != nil {
			return e
		}
		target := filepath.Join(dst, rel)
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("data backup does not follow symbolic links")
		}
		if d.IsDir() {
			if e = os.MkdirAll(target, 0700); e != nil {
				return e
			}
			return nil
		}
		st, e := d.Info()
		if e != nil {
			return e
		}
		if !st.Mode().IsRegular() {
			return errors.New("data contains a non-regular file")
		}
		return copyFile(path, target, 0600)
	})
	if err != nil {
		return err
	}
	return filepath.WalkDir(dst, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return syncDir(path)
		}
		return nil
	})
}
func treeSize(path string) (int64, error) {
	var n int64
	e := filepath.WalkDir(path, func(_ string, d fs.DirEntry, e error) error {
		if errors.Is(e, os.ErrNotExist) {
			return nil
		}
		if e != nil {
			return e
		}
		st, e := d.Info()
		if e != nil {
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 || (!st.IsDir() && !st.Mode().IsRegular()) {
			return errors.New("unsupported backup file")
		}
		if !st.IsDir() {
			n += st.Size()
		}
		return nil
	})
	return n, e
}
func freeSpace(path string, need int64) error {
	var s unix.Statfs_t
	if e := unix.Statfs(path, &s); e != nil {
		return e
	}
	if uint64(need) > s.Bavail*uint64(s.Bsize) {
		return errors.New("磁盘空间不足，无法安全更新")
	}
	return nil
}
func (p Paths) preflight() error {
	if e := p.validate(); e != nil {
		return e
	}
	for _, dir := range []string{p.State, p.Backup} {
		if e := os.MkdirAll(dir, 0700); e != nil {
			return e
		}
	}
	for _, dir := range []string{filepath.Dir(p.Binary), p.State, p.Backup, filepath.Dir(p.Config), filepath.Dir(p.Data)} {
		f, e := os.CreateTemp(dir, ".update-probe-*")
		if e != nil {
			return fmt.Errorf("更新目录不可写: %w", e)
		}
		name := f.Name()
		f.Close()
		os.Remove(name)
	}
	n, e := treeSize(p.Data)
	if e != nil {
		return e
	}
	st, e := os.Stat(p.Binary)
	if e != nil {
		return e
	}
	if e = freeSpace(filepath.Dir(p.Binary), 3*maxArchive+st.Size()+n+(32<<20)); e != nil {
		return e
	}
	return freeSpace(p.Backup, n+st.Size()+(32<<20))
}
