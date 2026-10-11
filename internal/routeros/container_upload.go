package routeros

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// UploadContainerArchive pins the configured host key and writes only an
// exclusive server-generated archive under the dedicated image staging folder.
// It never overwrites a user path. Cancellation closes the SSH connection.
func UploadContainerArchive(ctx context.Context, baseURL, user, password, fingerprint, target string, source io.Reader, size int64, digest string) error {
	p, err := FilePath(target)
	if err != nil || !strings.Contains(p, "/rosboard/images/") || !strings.HasSuffix(p, ".tar") || size <= 0 || size > 1<<30 || !strings.HasPrefix(fingerprint, "SHA256:") {
		return errors.New("invalid archive transfer configuration")
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Hostname() == "" {
		return errors.New("invalid archive device")
	}
	conn, err := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), "22"))
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if err = conn.SetDeadline(time.Now().Add(5 * time.Minute)); err != nil {
		return err
	}
	clientConn, chans, reqs, err := ssh.NewClientConn(conn, net.JoinHostPort(u.Hostname(), "22"), &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{ssh.Password(password)}, HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
		if ssh.FingerprintSHA256(key) != fingerprint {
			return errors.New("RouterOS SSH host key changed")
		}
		return nil
	}})
	if err != nil {
		return err
	}
	client := ssh.NewClient(clientConn, chans, reqs)
	defer client.Close()
	files, err := sftp.NewClient(client)
	if err != nil {
		return err
	}
	defer files.Close()
	target = strings.TrimPrefix(p, "/")
	if err = files.MkdirAll(path.Dir(target)); err != nil {
		return err
	}
	file, err := files.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return err
	}
	count, copyErr := io.Copy(file, io.LimitReader(source, size+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if count != size {
		return errors.New("archive size changed during transfer")
	}
	remote, err := files.Open(target)
	if err != nil {
		return err
	}
	defer remote.Close()
	hash := sha256.New()
	count, err = io.Copy(hash, io.LimitReader(remote, size+1))
	if err != nil {
		return err
	}
	if count != size || hex.EncodeToString(hash.Sum(nil)) != digest {
		return errors.New("archive transfer checksum mismatch")
	}
	return nil
}
