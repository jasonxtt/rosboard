package update

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	"rosboard/internal/buildinfo"
	"rosboard/internal/config"
)

func cleanChildEnv() []string {
	env := []string{}
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "ROSBOARD_SUPERVISED=") {
			env = append(env, e)
		}
	}
	return env
}

// AwaitActivation is called after startup checks and net.Listen, before serving
// requests or starting workers. The inherited pipe is private to the supervisor.
func AwaitActivation() error {
	if os.Getenv("ROSBOARD_SUPERVISED") != "1" {
		return nil
	}
	ready, control := os.NewFile(3, "ready"), os.NewFile(4, "activate")
	if ready == nil || control == nil {
		return errors.New("supervisor pipes unavailable")
	}
	defer ready.Close()
	defer control.Close()
	if e := json.NewEncoder(ready).Encode(buildinfo.Current()); e != nil {
		return e
	}
	line, e := bufio.NewReader(control).ReadString('\n')
	if e != nil {
		return e
	}
	if line != "activate\n" {
		return errors.New("supervisor did not authorize activation")
	}
	return nil
}

type child struct {
	cmd       *exec.Cmd
	done      chan error
	ready     chan readiness
	control   *os.File
	readyFile *os.File
}
type readiness struct {
	info buildinfo.Info
	err  error
}

func launch(p Paths) (*child, error) {
	r, w, e := os.Pipe()
	if e != nil {
		return nil, e
	}
	cr, cw, e := os.Pipe()
	if e != nil {
		r.Close()
		w.Close()
		return nil, e
	}
	cmd := exec.Command(p.Binary, "-config", p.Config)
	cmd.Env = append(cleanChildEnv(), "ROSBOARD_SUPERVISED=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = []*os.File{w, cr}
	if e = cmd.Start(); e != nil {
		r.Close()
		w.Close()
		cr.Close()
		cw.Close()
		return nil, e
	}
	w.Close()
	cr.Close()
	c := &child{cmd: cmd, done: make(chan error, 1), ready: make(chan readiness, 1), control: cw, readyFile: r}
	go func() { c.done <- cmd.Wait() }()
	go func() {
		var info buildinfo.Info
		e := json.NewDecoder(io.LimitReader(r, 4096)).Decode(&info)
		c.ready <- readiness{info, e}
		r.Close()
	}()
	return c, nil
}
func (c *child) stop() {
	c.control.Close()
	c.readyFile.Close()
	_ = c.cmd.Process.Signal(unix.SIGTERM)
	select {
	case <-c.done:
	case <-time.After(15 * time.Second):
		_ = c.cmd.Process.Kill()
		<-c.done
	}
}
func (c *child) activate() error {
	_, e := c.control.Write([]byte("activate\n"))
	c.control.Close()
	return e
}
func (c *child) waitReady(ctx context.Context, expected string) error {
	select {
	case r := <-c.ready:
		if r.err != nil {
			return r.err
		}
		if expected != "" && (r.info.Version != expected || r.info.OS != "linux") {
			return errors.New("candidate readiness version mismatch")
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(45 * time.Second):
		return errors.New("candidate readiness timeout")
	}
}

// Supervise is deliberately hosted by a separate, stable executable copy. It
// must remain startable even when the candidate cannot execute after a reboot.
func Supervise(ctx context.Context, p Paths, logger *log.Logger) error {
	if e := p.validate(); e != nil {
		return e
	}
	if e := os.MkdirAll(p.State, 0700); e != nil {
		return e
	}
	lock, e := os.OpenFile(filepath.Join(p.State, "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	if e = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		return errors.New("another rosboard supervisor is running")
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	j, e := readJob(p)
	if e != nil {
		return fmt.Errorf("read recovery journal: %w", e)
	}
	if j.Active() {
		switch j.Stage {
		case "installing", "verifying_startup":
			if e = p.restore(); e != nil {
				return fmt.Errorf("restore interrupted update: %w", e)
			}
			e = finishJob(p, j, "rolled_back", "更新被中断，已恢复原版本和数据")
		case "downloading", "verifying", "pending", "backing_up":
			e = finishJob(p, j, "failed", "更新被中断，原版本未改变")
		default:
			return errors.New("unknown recovery stage; manual inspection required")
		}
		if e != nil {
			return e
		}
	}
	var c *child
	for ctx.Err() == nil {
		if c == nil {
			// Full reset or an operator config edit may change the data path.
			cfg, err := config.Load(p.Config)
			if err != nil {
				return err
			}
			p.Data = cfg.DataDir
			if err = p.validate(); err != nil {
				return err
			}
			c, e = launch(p)
			if e != nil {
				return e
			}
			if e = c.waitReady(ctx, ""); e != nil {
				c.stop()
				return fmt.Errorf("panel startup: %w", e)
			}
			if e = c.activate(); e != nil {
				c.stop()
				return e
			}
		}
		select {
		case <-ctx.Done():
			c.stop()
			return nil
		case e = <-c.done:
			logger.Printf("panel stopped: %v", e)
		}
		c.control.Close()
		c.readyFile.Close()
		c = nil
		j, e = readJob(p)
		if e != nil {
			return e
		}
		if j != nil && j.Stage == "pending" {
			c, e = apply(ctx, p, j, logger)
			if e != nil {
				return e
			}
		} else if j.Active() {
			if e = finishJob(p, j, "failed", "更新被中断，原版本未改变"); e != nil {
				return e
			}
		}
		if c == nil {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Second):
			}
		}
	}
	if c != nil {
		c.stop()
	}
	return nil
}

func apply(ctx context.Context, p Paths, j *Job, logger *log.Logger) (*child, error) {
	j.Stage = "backing_up"
	if e := saveJob(p, j); e != nil {
		return nil, e
	}
	if e := p.snapshot(); e != nil {
		logger.Printf("update backup failed: %v", e)
		return nil, finishJob(p, j, "failed", "备份失败，原版本未改变；详情见服务日志")
	}
	// Write-ahead journal: recovery is mandatory even if the rename is interrupted.
	j.Stage = "installing"
	if e := saveJob(p, j); e != nil {
		return nil, e
	}
	recoverOld := func(cause error) (*child, error) {
		logger.Printf("update activation failed: %v", cause)
		if e := p.restore(); e != nil {
			return nil, fmt.Errorf("update recovery failed: %w", e)
		}
		return nil, finishJob(p, j, "rolled_back", "新版本启动验证失败，已恢复原版本和数据")
	}
	if e := os.Rename(filepath.Join(p.State, "candidate"), p.Binary); e != nil {
		return recoverOld(e)
	}
	if e := syncDir(filepath.Dir(p.Binary)); e != nil {
		return recoverOld(e)
	}
	j.Stage = "verifying_startup"
	if e := saveJob(p, j); e != nil {
		return recoverOld(e)
	}
	c, e := launch(p)
	if e != nil {
		return recoverOld(e)
	}
	if e = c.waitReady(ctx, j.To); e != nil {
		c.stop()
		return recoverOld(e)
	}
	if e = c.activate(); e != nil {
		c.stop()
		return recoverOld(e)
	}
	cfg, e := config.Load(p.Config)
	if e == nil {
		var host, port string
		host, port, e = net.SplitHostPort(cfg.ListenAddress)
		if host == "" || host == "0.0.0.0" {
			host = "127.0.0.1"
		}
		if host == "::" {
			host = "::1"
		}
		if e == nil {
			e = c.observe(ctx, "http://"+net.JoinHostPort(host, port)+"/api/health", j.To, 5*time.Second, 20*time.Second)
		}
	}
	if e != nil {
		c.stop()
		return recoverOld(e)
	}
	// Workers run during observation, but external writes remain gated until
	// this durable commit. Local changes can still be restored from snapshot.
	if e = finishJob(p, j, "succeeded", "更新成功"); e != nil {
		c.stop()
		return recoverOld(e)
	}
	cleanupDownload(p)
	return c, nil
}

// observe requires continuous healthy responses from this exact child, not an
// unrelated listener or a stale version. Any exit before commit triggers recovery.
func (c *child) observe(ctx context.Context, endpoint, version string, stable, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var healthySince time.Time
	for {
		select {
		case err := <-c.done:
			c.done <- err // stop still owns reaping the child.
			return fmt.Errorf("candidate exited after activation: %v", err)
		case <-ctx.Done():
			return fmt.Errorf("candidate health observation: %w", ctx.Err())
		case <-ticker.C:
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		response, err := client.Do(req)
		healthy := false
		if err == nil {
			var health struct {
				OK      bool   `json:"ok"`
				Version string `json:"version"`
				PID     int    `json:"pid"`
			}
			decodeErr := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&health)
			response.Body.Close()
			healthy = response.StatusCode == http.StatusOK && decodeErr == nil && health.OK && health.Version == version && health.PID == c.cmd.Process.Pid
		}
		if !healthy {
			healthySince = time.Time{}
			continue
		}
		if healthySince.IsZero() {
			healthySince = time.Now()
		}
		if time.Since(healthySince) >= stable {
			select {
			case err := <-c.done:
				c.done <- err
				return fmt.Errorf("candidate exited after activation: %v", err)
			default:
				return ctx.Err()
			}
		}
	}
}
