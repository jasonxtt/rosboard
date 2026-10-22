package routeros

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type mutableTransferFS struct {
	base   sftp.Handlers
	mu     sync.Mutex
	events []string
	drop   string
	close  func()
}

func (f *mutableTransferFS) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	return f.base.FileGet.Fileread(r)
}
func (f *mutableTransferFS) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	f.mu.Lock()
	f.events = append(f.events, r.Method+":"+r.Filepath)
	f.mu.Unlock()
	return f.base.FilePut.Filewrite(r)
}
func (f *mutableTransferFS) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	return f.base.FileList.Filelist(r)
}
func (f *mutableTransferFS) Filecmd(r *sftp.Request) error {
	f.mu.Lock()
	f.events = append(f.events, r.Method+":"+r.Filepath)
	drop := f.drop == r.Method
	if drop {
		f.drop = ""
	}
	close := f.close
	f.mu.Unlock()
	e := f.base.FileCmd.Filecmd(r)
	if e == nil && drop && close != nil {
		close()
	}
	return e
}
func transferFixture(t *testing.T) (*mutableTransferFS, string, int, string) {
	t.Helper()
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(key)
	cfg := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
		if string(p) != "test-only" {
			return nil, os.ErrPermission
		}
		return nil, nil
	}}
	cfg.AddHostKey(signer)
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	fs := &mutableTransferFS{base: sftp.InMemHandler()}
	if e := fs.base.FileCmd.Filecmd(sftp.NewRequest("Mkdir", "/disk")); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	go func() {
		for {
			conn, e := ln.Accept()
			if e != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(10 * time.Second))
				server, channels, requests, e := ssh.NewServerConn(conn, cfg)
				if e != nil {
					return
				}
				defer server.Close()
				go ssh.DiscardRequests(requests)
				for ch := range channels {
					channel, reqs, e := ch.Accept()
					if e != nil {
						return
					}
					for req := range reqs {
						if req.Type != "subsystem" {
							req.Reply(false, nil)
							continue
						}
						req.Reply(true, nil)
						fs.mu.Lock()
						fs.close = func() { channel.Close() }
						fs.mu.Unlock()
						s := sftp.NewRequestServer(channel, sftp.Handlers{FileGet: fs, FilePut: fs, FileCmd: fs, FileList: fs})
						s.Serve()
						s.Close()
						break
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { ln.Close(); wg.Wait() })
	return fs, "http://127.0.0.1", ln.Addr().(*net.TCPAddr).Port, ssh.FingerprintSHA256(signer.PublicKey())
}
func TestPreparedTransferRecoveryAndExactCleanup(t *testing.T) {
	for _, fault := range []string{"none", "checkpoint", "rename", "remove"} {
		t.Run(fault, func(t *testing.T) {
			fs, base, port, key := transferFixture(t)
			payload := []byte("archive test bytes")
			hash := sha256.Sum256(payload)
			state := PreparedTransfer{Path: "/disk/rosboard/images/jobs/" + uuid.NewString() + "/image.tar", Bytes: int64(len(payload)), SHA256: hex.EncodeToString(hash[:])}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			checkpointFault := fault == "checkpoint"
			checkpoint := func() error {
				if checkpointFault && state.PartialCreated && !state.Verified {
					checkpointFault = false
					return errors.New("checkpoint fault")
				}
				return nil
			}
			if fault == "rename" {
				fs.drop = "Rename"
			}
			e := TransferPrepared(ctx, base, "transfer", "test-only", key, port, &state, bytes.NewReader(payload), checkpoint, func(string) {})
			if fault == "checkpoint" || fault == "rename" {
				if e == nil {
					t.Fatal("fault not exercised")
				}
				e = TransferPrepared(ctx, base, "transfer", "test-only", key, port, &state, bytes.NewReader(payload), checkpoint, func(string) {})
			}
			if e != nil || !state.Published {
				t.Fatalf("transfer not reconciled: %v", e)
			}
			if fault == "remove" {
				fs.mu.Lock()
				fs.drop = "Remove"
				fs.mu.Unlock()
			}
			e = RemovePrepared(ctx, base, "transfer", "test-only", key, port, state)
			if fault == "remove" {
				if e == nil {
					t.Fatal("remove fault not exercised")
				}
				e = RemovePrepared(ctx, base, "transfer", "test-only", key, port, state)
			}
			if e != nil {
				t.Fatal(e)
			}
			// Repeated cleanup must succeed without repeating file removal.
			if e = RemovePrepared(ctx, base, "transfer", "test-only", key, port, state); e != nil {
				t.Fatal(e)
			}
			fs.mu.Lock()
			events := append([]string{}, fs.events...)
			fs.mu.Unlock()
			finalRemoves := 0
			for _, event := range events {
				if event == "Remove:"+state.Path {
					finalRemoves++
				}
				if strings.Contains(event, "Mkdir:/disk/") && !strings.HasPrefix(event, "Mkdir:/disk/rosboard") {
					t.Fatal("foreign path mutation")
				}
			}
			if finalRemoves != 1 {
				t.Fatalf("final remove repeated %d times", finalRemoves)
			}
		})
	}
}
func TestPreparedTransferRejectsForeignPathsAndCollisions(t *testing.T) {
	fs, base, port, key := transferFixture(t)
	payload := []byte("same content")
	h := sha256.Sum256(payload)
	state := PreparedTransfer{Path: "/disk/rosboard/images/jobs/" + uuid.NewString() + "/image.tar", Bytes: int64(len(payload)), SHA256: hex.EncodeToString(h[:])}
	ctx := context.Background()
	if e := TransferPrepared(ctx, base, "transfer", "test-only", key, port, &state, bytes.NewReader(payload), func() error { return nil }, func(string) {}); e != nil {
		t.Fatal(e)
	}
	fs.mu.Lock()
	before := len(fs.events)
	fs.mu.Unlock()
	unowned := state
	unowned.Verified = false
	unowned.Published = false
	unowned.NamespaceCreated = false
	unowned.PartialCreated = false
	if e := TransferPrepared(ctx, base, "transfer", "test-only", key, port, &unowned, bytes.NewReader(payload), func() error { return nil }, func(string) {}); e == nil {
		t.Fatal("foreign collision adopted")
	}
	for _, p := range []string{"/disk/user.tar", "/disk/rosboard/images/jobs/../image.tar", "/disk/rosboard/images/jobs/not-uuid/image.tar"} {
		bad := state
		bad.Path = p
		if RemovePrepared(ctx, base, "transfer", "test-only", key, port, bad) == nil {
			t.Fatal("foreign cleanup accepted")
		}
	}
	fs.mu.Lock()
	after := len(fs.events)
	fs.mu.Unlock()
	if before != after {
		t.Fatal("denied paths caused mutations")
	}
}

func TestAutomaticTransferKeyDiscoveryAndPinnedRead(t *testing.T) {
	_, base, port, key := transferFixture(t)
	observed, err := DiscoverContainerTransferKey(context.Background(), base, port)
	if err != nil || observed != key {
		t.Fatalf("discovery failed: %v", err)
	}
	if err = CheckContainerTransfer(context.Background(), base, "fixture", "test-only", observed, port); err != nil {
		t.Fatal(err)
	}
	if err = CheckContainerTransfer(context.Background(), base, "fixture", "wrong-password", observed, port); err == nil {
		t.Fatal("authentication failure accepted")
	}
	if err = CheckContainerTransfer(context.Background(), base, "fixture", "test-only", "SHA256:changed", port); err == nil {
		t.Fatal("changed host identity accepted")
	}
}
