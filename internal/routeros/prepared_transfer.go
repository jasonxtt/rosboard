package routeros

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/pkg/sftp"
)

var ErrTransferUnknown = errors.New("镜像传输状态尚未确认；保留已记录路径，请恢复检查，不要重复上传")

type PreparedTransfer struct {
	Path             string
	Bytes            int64
	SHA256           string
	NamespaceCreated bool
	PartialCreated   bool
	Verified         bool
	Published        bool
}

func preparedPath(p string) bool {
	clean, e := FilePath(p)
	if e != nil || clean != p {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	if len(parts) != 6 || parts[1] != "rosboard" || parts[2] != "images" || parts[3] != "jobs" || parts[5] != "image.tar" {
		return false
	}
	_, e = uuid.Parse(parts[4])
	return e == nil
}
func verifyRemote(files *sftp.Client, p string, size int64, digest string) error {
	stat, e := remoteInfo(files, p)
	if e != nil {
		return e
	}
	if !stat.Mode().IsRegular() || stat.Size() != size {
		return errors.New("remote image metadata mismatch")
	}
	f, e := files.Open(p)
	if e != nil {
		return e
	}
	defer f.Close()
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(f, size+1))
	if e != nil || n != size || hex.EncodeToString(h.Sum(nil)) != digest {
		return errors.New("remote image checksum mismatch")
	}
	return nil
}

// TransferPrepared is called only for an explicit journaled image job holding
// the device gate. checkpoint must persist every state before the next mutation.
func TransferPrepared(ctx context.Context, baseURL, user, password, key string, port int, state *PreparedTransfer, source io.ReadSeeker, checkpoint func() error, phase func(string)) error {
	if !preparedPath(state.Path) || state.Bytes <= 0 || state.Bytes > 1<<30 || len(state.SHA256) != 64 {
		return errors.New("invalid prepared archive")
	}
	files, closeSession, e := containerTransferSession(ctx, baseURL, user, password, key, port)
	if e != nil {
		return e
	}
	defer closeSession()
	final := strings.TrimPrefix(state.Path, "/")
	partial := final + ".partial"
	dir := path.Dir(final)
	if state.Published {
		phase("回读已上传镜像")
		return verifyRemote(files, final, state.Bytes, state.SHA256)
	}
	if _, e = remoteInfo(files, final); e == nil {
		if !state.Verified {
			return ErrTransferUnknown
		}
		if e = verifyRemote(files, final, state.Bytes, state.SHA256); e != nil {
			return ErrTransferUnknown
		}
		state.Published = true
		return checkpoint()
	} else if !os.IsNotExist(e) {
		return e
	}
	if !state.NamespaceCreated {
		if _, e = remoteInfo(files, dir); e == nil {
			return ErrTransferUnknown
		} else if !os.IsNotExist(e) {
			return e
		}
		if e = ensureRemoteParents(files, path.Dir(dir)); e != nil {
			return ErrTransferUnknown
		}
		if e = files.Mkdir(dir); e != nil {
			return ErrTransferUnknown
		}
		state.NamespaceCreated = true
		if e = checkpoint(); e != nil {
			return e
		}
	}
	if _, e = remoteInfo(files, partial); e == nil {
		if !state.PartialCreated {
			return ErrTransferUnknown
		}
		phase("回读上次镜像传输结果")
		if verifyRemote(files, partial, state.Bytes, state.SHA256) == nil {
			state.Verified = true
			if e = checkpoint(); e != nil {
				return e
			}
		} else {
			// Only a previously confirmed exclusive creation may be discarded.
			if e = checkpoint(); e != nil {
				return e
			}
			if e = files.Remove(partial); e != nil {
				return ErrTransferUnknown
			}
			state.PartialCreated = false
			state.Verified = false
			if e = checkpoint(); e != nil {
				return e
			}
		}
	} else if !os.IsNotExist(e) {
		return e
	} else {
		state.PartialCreated = false
		state.Verified = false
		if e = checkpoint(); e != nil {
			return e
		}
	}
	if !state.Verified {
		phase("上传镜像到 RouterOS")
		if _, e = source.Seek(0, io.SeekStart); e != nil {
			return e
		}
		f, err := files.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
		if err != nil {
			return ErrTransferUnknown
		}
		state.PartialCreated = true
		if e = checkpoint(); e != nil {
			f.Close()
			return e
		}
		n, err := io.Copy(f, io.LimitReader(source, state.Bytes+1))
		ce := f.Close()
		if err != nil || ce != nil || n != state.Bytes {
			return ErrTransferUnknown
		}
		phase("回读校验镜像大小与 SHA-256")
		if e = verifyRemote(files, partial, state.Bytes, state.SHA256); e != nil {
			return ErrTransferUnknown
		}
		state.Verified = true
		if e = checkpoint(); e != nil {
			return e
		}
	}
	if e = files.Rename(partial, final); e != nil {
		return ErrTransferUnknown
	}
	state.Published = true
	return checkpoint()
}

// RemovePrepared reconciles each exact recorded object before removing it.
// No recursive directory deletion or user-supplied paths are accepted.
func RemovePrepared(ctx context.Context, baseURL, user, password, key string, port int, state PreparedTransfer) error {
	if !preparedPath(state.Path) || !state.NamespaceCreated {
		return errors.New("unowned image cleanup refused")
	}
	files, closeSession, e := containerTransferSession(ctx, baseURL, user, password, key, port)
	if e != nil {
		return e
	}
	defer closeSession()
	final := strings.TrimPrefix(state.Path, "/")
	for _, p := range []string{final, final + ".partial"} {
		stat, err := remoteInfo(files, p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !stat.Mode().IsRegular() {
			return errors.New("image path changed")
		}
		if p == final {
			if !state.Verified {
				return ErrTransferUnknown
			}
			if e = verifyRemote(files, p, state.Bytes, state.SHA256); e != nil {
				return e
			}
		} else if !state.PartialCreated {
			return ErrTransferUnknown
		}
		if e = files.Remove(p); e != nil {
			return ErrTransferUnknown
		}
	}
	if _, err := remoteInfo(files, path.Dir(final)); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	entries, e := files.ReadDir(path.Dir(final))
	if e == nil && len(entries) == 0 {
		if e = files.RemoveDirectory(path.Dir(final)); e != nil {
			return ErrTransferUnknown
		}
	}
	return e
}

// RouterOS can return vendor status codes for a missing LSTAT target. Infer
// absence only from a successful parent listing, never from an unknown error.
func remoteInfo(files *sftp.Client, p string) (os.FileInfo, error) {
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	current := "."
	for i, part := range parts {
		entries, e := files.ReadDir(current)
		if e != nil {
			return nil, e
		}
		var found os.FileInfo
		for _, entry := range entries {
			if entry.Name() == part {
				found = entry
				break
			}
		}
		if found == nil {
			return nil, os.ErrNotExist
		}
		if found.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("remote symlink refused")
		}
		if i == len(parts)-1 {
			return found, nil
		}
		if !found.IsDir() {
			return nil, errors.New("remote parent is not directory")
		}
		current = path.Join(current, part)
	}
	return nil, os.ErrNotExist
}
func ensureRemoteParents(files *sftp.Client, p string) error {
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	current := ""
	for i, part := range parts {
		current = path.Join(current, part)
		info, e := remoteInfo(files, current)
		if e == nil {
			if !info.IsDir() {
				return errors.New("remote parent changed")
			}
			continue
		}
		if !os.IsNotExist(e) || i == 0 {
			return e
		}
		if e = files.Mkdir(current); e != nil {
			return ErrTransferUnknown
		}
	}
	return nil
}

func PreparedAbsent(ctx context.Context, baseURL, user, password, key string, port int, state PreparedTransfer) error {
	if !preparedPath(state.Path) {
		return ErrTransferUnknown
	}
	files, closeSession, e := containerTransferSession(ctx, baseURL, user, password, key, port)
	if e != nil {
		return e
	}
	defer closeSession()
	_, e = remoteInfo(files, path.Dir(strings.TrimPrefix(state.Path, "/")))
	if os.IsNotExist(e) {
		return nil
	}
	return ErrTransferUnknown
}
