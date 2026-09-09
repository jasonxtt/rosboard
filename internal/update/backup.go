package update

import (
	"errors"
	"os"
	"path/filepath"
)

func (p Paths) snapshot() error {
	if e := p.preflight(); e != nil {
		return e
	}
	dest := filepath.Join(p.Backup, "previous")
	if _, e := os.Stat(dest); e == nil {
		if _, e = os.Stat(filepath.Join(dest, "complete")); e != nil {
			return errors.New("unrecognized previous backup; refusing to remove it")
		}
		if e = os.RemoveAll(dest); e != nil {
			return e
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	temp := filepath.Join(p.Backup, "preparing")
	if e := os.RemoveAll(temp); e != nil {
		return e
	}
	if e := os.Mkdir(temp, 0700); e != nil {
		return e
	}
	if e := copyFile(p.Binary, filepath.Join(temp, "rosboard"), 0700); e != nil {
		return e
	}
	if _, e := os.Stat(p.Config); e == nil {
		if e = copyFile(p.Config, filepath.Join(temp, "config"), 0600); e != nil {
			return e
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	if e := copyTree(p.Data, filepath.Join(temp, "data")); e != nil {
		return e
	}
	if e := atomicWrite(filepath.Join(temp, "complete"), []byte("rosboard update backup v1\n"), 0600); e != nil {
		return e
	}
	if e := os.Rename(temp, dest); e != nil {
		return e
	}
	return syncDir(p.Backup)
}
func (p Paths) restore() error {
	if e := p.validate(); e != nil {
		return e
	}
	src := filepath.Join(p.Backup, "previous")
	if e := noLinks(src); e != nil {
		return e
	}
	if _, e := treeSize(src); e != nil {
		return e
	}
	if _, e := os.Stat(filepath.Join(src, "complete")); e != nil {
		return errors.New("complete recovery backup missing")
	}
	// Journal remains in installing/verifying until the complete restore finishes;
	// repeating this restore after a power interruption is safe.
	temp := filepath.Join(filepath.Dir(p.Binary), ".rosboard-restore")
	if e := os.Remove(temp); e != nil && !errors.Is(e, os.ErrNotExist) {
		return e
	}
	if e := copyFile(filepath.Join(src, "rosboard"), temp, 0700); e != nil {
		return e
	}
	if e := os.Rename(temp, p.Binary); e != nil {
		return e
	}
	if e := syncDir(filepath.Dir(p.Binary)); e != nil {
		return e
	}
	if e := os.RemoveAll(p.Data); e != nil {
		return e
	}
	if e := copyTree(filepath.Join(src, "data"), p.Data); e != nil {
		return e
	}
	if b, e := os.ReadFile(filepath.Join(src, "config")); e == nil {
		return atomicWrite(p.Config, b, 0600)
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	if e := os.Remove(p.Config); e != nil && !errors.Is(e, os.ErrNotExist) {
		return e
	}
	return syncDir(filepath.Dir(p.Config))
}
