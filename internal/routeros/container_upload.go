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
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// UploadContainerArchive pins the configured host key and writes only an
// exclusive server-generated archive under the dedicated image staging folder.
// It never overwrites a user path. Cancellation closes the SSH connection.
func UploadContainerArchive(ctx context.Context, baseURL, user, password, fingerprint, target string, source io.Reader, size int64, digest string) error {
	return UploadContainerArchivePort(ctx, baseURL, user, password, fingerprint, 22, target, source, size, digest)
}

func UploadContainerArchivePort(ctx context.Context, baseURL, user, password, fingerprint string, port int, target string, source io.Reader, size int64, digest string) error {
	p, err := FilePath(target)
	if err != nil || !strings.Contains(p, "/rosboard/images/") || !strings.HasSuffix(p, ".tar") || size <= 0 || size > 1<<30 || !strings.HasPrefix(fingerprint, "SHA256:") {
		return errors.New("invalid archive transfer configuration")
	}
	files, closeSession, err := containerTransferSession(ctx, baseURL, user, password, fingerprint, port)
	if err != nil {
		return err
	}
	defer closeSession()
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

// CheckContainerTransfer performs authentication and metadata reads only. It
// intentionally offers no write-probe switch or shell execution path.
func CheckContainerTransfer(ctx context.Context, baseURL, user, password, fingerprint string, port int) error {
	files, closeSession, err := containerTransferSession(ctx, baseURL, user, password, fingerprint, port)
	if err != nil {
		return err
	}
	defer closeSession()
	_, err = files.ReadDir(".")
	return err
}

func containerTransferSession(ctx context.Context, baseURL, user, password, fingerprint string, port int) (*sftp.Client, func(), error) {
	if user == "" || password == "" || port < 1 || port > 65535 || !strings.HasPrefix(fingerprint, "SHA256:") {
		return nil, nil, errors.New("invalid transfer credentials or key")
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Hostname() == "" {
		return nil, nil, errors.New("invalid archive device")
	}
	rawConn, err := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), strconv.Itoa(port)))
	if err != nil {
		return nil, nil, err
	}

	conn := &transferIdleConn{Conn: rawConn}
	stop := context.AfterFunc(ctx, func() { conn.Close() })

	clientConn, chans, reqs, err := ssh.NewClientConn(conn, net.JoinHostPort(u.Hostname(), strconv.Itoa(port)), &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{ssh.Password(password)}, HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
		if ssh.FingerprintSHA256(key) != fingerprint {
			return errors.New("RouterOS SSH host key changed")
		}
		return nil
	}})
	if err != nil {
		stop()
		conn.Close()
		return nil, nil, err
	}
	client := ssh.NewClient(clientConn, chans, reqs)

	files, err := sftp.NewClient(client)
	if err != nil {
		stop()
		client.Close()
		return nil, nil, err
	}

	return files, func() { stop(); files.Close(); client.Close(); conn.Close() }, nil
}

// Idle-progress limits apply independently to every read/write, while the
// caller's stage context bounds total duration. Active transfers may exceed 5m.
type transferIdleConn struct{ net.Conn }

func (c *transferIdleConn) Read(p []byte) (int, error) {
	c.SetReadDeadline(time.Now().Add(60 * time.Second))
	return c.Conn.Read(p)
}
func (c *transferIdleConn) Write(p []byte) (int, error) {
	c.SetWriteDeadline(time.Now().Add(60 * time.Second))
	return c.Conn.Write(p)
}

// DiscoverContainerTransferKey observes the key at the REST device's SSH host.
// It aborts before authentication; the caller must authenticate and read SFTP
// with this exact key before persisting first-use trust. Existing pins never use it.
func DiscoverContainerTransferKey(ctx context.Context, baseURL string, port int) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Hostname() == "" || port < 1 || port > 65535 {
		return "", errors.New("invalid transfer endpoint")
	}
	address := net.JoinHostPort(u.Hostname(), strconv.Itoa(port))
	raw, err := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return "", err
	}
	defer raw.Close()
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	defer stop()
	raw.SetDeadline(time.Now().Add(15 * time.Second))
	observed := ""
	sentinel := errors.New("host key observed before authentication")
	_, _, _, err = ssh.NewClientConn(raw, address, &ssh.ClientConfig{User: "rosboard-key-discovery", HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
		observed = ssh.FingerprintSHA256(key)
		return sentinel
	}})
	if observed == "" {
		return "", err
	}
	return observed, nil
}
