package routeros

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type transferReadHandlers struct {
	mutations atomic.Int32
	reads     atomic.Int32
	deny      bool
}

func (h *transferReadHandlers) Fileread(*sftp.Request) (io.ReaderAt, error) {
	h.reads.Add(1)
	return nil, os.ErrPermission
}
func (h *transferReadHandlers) Filewrite(*sftp.Request) (io.WriterAt, error) {
	h.mutations.Add(1)
	return nil, os.ErrPermission
}
func (h *transferReadHandlers) Filecmd(*sftp.Request) error {
	h.mutations.Add(1)
	return os.ErrPermission
}
func (h *transferReadHandlers) Filelist(*sftp.Request) (sftp.ListerAt, error) {
	h.reads.Add(1)
	if h.deny {
		return nil, os.ErrPermission
	}
	return emptyTransferListing{}, nil
}

type emptyTransferListing struct{}

func (emptyTransferListing) ListAt([]os.FileInfo, int64) (int, error) { return 0, io.EOF }

func TestTransferDetectionNeverWrites(t *testing.T) {
	for _, scenario := range []string{"success", "permission-denied", "auth-denied", "key-changed", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			_, key, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			signer, err := ssh.NewSignerFromKey(key)
			if err != nil {
				t.Fatal(err)
			}
			cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
				if scenario == "auth-denied" || c.User() != "transfer" || string(p) != "test-only" {
					return nil, errors.New("denied")
				}
				return nil, nil
			}}
			cfg.AddHostKey(signer)
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			h := &transferReadHandlers{deny: scenario == "permission-denied"}
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(3 * time.Second))
				server, channels, requests, err := ssh.NewServerConn(conn, cfg)
				if err != nil {
					return
				}
				defer server.Close()
				go ssh.DiscardRequests(requests)
				for ch := range channels {
					if ch.ChannelType() != "session" {
						h.mutations.Add(1)
						ch.Reject(ssh.Prohibited, "session only")
						continue
					}
					channel, reqs, err := ch.Accept()
					if err != nil {
						return
					}
					for req := range reqs {
						if req.Type != "subsystem" {
							h.mutations.Add(1)
							req.Reply(false, nil)
							continue
						}
						req.Reply(true, nil)
						fs := sftp.NewRequestServer(channel, sftp.Handlers{FileGet: h, FilePut: h, FileCmd: h, FileList: h})
						fs.Serve()
						fs.Close()
						break
					}
				}
			}()
			fingerprint := ssh.FingerprintSHA256(signer.PublicKey())
			if scenario == "key-changed" {
				fingerprint = "SHA256:wrong"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			err = CheckContainerTransfer(ctx, "http://127.0.0.1", "transfer", "test-only", fingerprint, ln.Addr().(*net.TCPAddr).Port)
			ln.Close()
			select {
			case <-done:
			case <-time.After(4 * time.Second):
				t.Fatal("session leaked")
			}
			if (err == nil) != (scenario == "success") {
				t.Fatalf("error=%v", err)
			}
			if h.mutations.Load() != 0 {
				t.Fatalf("detection sent %d mutation/shell requests", h.mutations.Load())
			}
			if scenario == "success" && h.reads.Load() == 0 {
				t.Fatal("no read connectivity evidence")
			}
		})
	}
}

func TestTransferSSHDisabledAndInvalidConfig(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	if err := CheckContainerTransfer(context.Background(), "http://127.0.0.1", "test", "test", "SHA256:test", port); err == nil {
		t.Fatal("disabled service accepted")
	}
	for _, p := range []int{-1, 0, 65536} {
		if err := CheckContainerTransfer(context.Background(), "http://127.0.0.1", "test", "test", "SHA256:test", p); err == nil {
			t.Fatal("invalid port accepted")
		}
	}
}
