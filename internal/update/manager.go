package update

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"rosboard/internal/buildinfo"
)

type Job struct {
	ID         string     `json:"id"`
	From       string     `json:"from"`
	To         string     `json:"to"`
	Stage      string     `json:"stage"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Message    string     `json:"message"`
	Downloaded int64      `json:"downloaded"`
	Total      int64      `json:"total"`
}

func (j *Job) Active() bool {
	return j != nil && j.Stage != "succeeded" && j.Stage != "failed" && j.Stage != "rolled_back"
}

type Status struct {
	Current    buildinfo.Info `json:"current"`
	Latest     *Release       `json:"latest"`
	CheckedAt  *time.Time     `json:"checkedAt"`
	CheckError string         `json:"checkError"`
	CanInstall bool           `json:"canInstall"`
	Reason     string         `json:"reason"`
	Job        *Job           `json:"job"`
}
type Manager struct {
	mu          sync.Mutex
	paths       Paths
	info        buildinfo.Info
	client      *releaseClient
	latest      *Release
	checkedAt   *time.Time
	attemptedAt time.Time
	checkError  string
	checking    bool
	job         *Job
	stop        func()
	ctx         context.Context
	logger      *log.Logger
	supported   bool
}

func NewManager(ctx context.Context, p Paths, info buildinfo.Info, stop func(), logger *log.Logger) *Manager {
	return &Manager{paths: p, info: info, client: newReleaseClient(), ctx: ctx, stop: stop, logger: logger, supported: os.Getenv("ROSBOARD_SUPERVISED") == "1" && info.OS == "linux"}
}
func (m *Manager) statusLocked() Status {
	j := m.job
	if disk, e := readJob(m.paths); e != nil {
		return Status{Current: m.info, Latest: m.latest, CheckedAt: m.checkedAt, CheckError: m.checkError, Reason: "更新状态无法读取，请检查服务日志和更新目录", Job: &Job{ID: "unreadable", Stage: "recovery_required", Message: "更新状态无法读取"}}
	} else if disk != nil {
		if j == nil || j.ID != disk.ID || j.Active() {
			j = disk
		}
	}
	if j != nil {
		copy := *j
		j = &copy
	}
	s := Status{Current: m.info, Latest: m.latest, CheckedAt: m.checkedAt, CheckError: m.checkError, Job: j}
	switch {
	case os.Getenv("ROSBOARD_UPDATE_DISABLED") != "":
		s.Reason = "此安装已禁用在线更新，请由管理员按部署流程更新"
	case !m.supported:
		s.Reason = "在线更新需要 Linux systemd 守护安装，请参阅部署说明"
	case !stableVersion.MatchString(m.info.Version):
		s.Reason = "开发构建不支持在线更新，请先安装正式版本"
	case m.latest == nil:
		s.Reason = "请先检查更新"
	case m.checkError != "":
		s.Reason = "检查失败，请重新检查更新"
	case j.Active():
		s.Reason = "更新正在进行"
	default:
		c, e := CompareStable(m.latest.Version, m.info.Version)
		switch {
		case e != nil:
			s.Reason = "版本号无法比较"
		case c <= 0:
			s.Reason = "当前已是最新版本"
		case m.latest.Asset.Name == "" || m.latest.Checksums.Name == "":
			s.Reason = "新版本暂未提供当前架构的完整安装包"
		default:
			s.CanInstall = true
		}
	}
	return s
}
func (m *Manager) Status() Status { m.mu.Lock(); defer m.mu.Unlock(); return m.statusLocked() }
func (m *Manager) Active() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked().Job.Active()
}
func (m *Manager) Check(ctx context.Context) (Status, error) {
	m.mu.Lock()
	if m.checking || time.Since(m.attemptedAt) < 30*time.Second || m.statusLocked().Job.Active() {
		s := m.statusLocked()
		m.mu.Unlock()
		return s, errors.New("请稍后再检查更新")
	}
	m.checking = true
	m.attemptedAt = time.Now()
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	rel, e := m.client.discover(ctx, m.info.Arch)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checking = false
	if e != nil {
		m.checkError = e.Error()
	} else {
		now := time.Now().UTC()
		m.latest = rel
		m.checkedAt = &now
		m.checkError = ""
	}
	return m.statusLocked(), e
}
func (m *Manager) Install(version string) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.statusLocked()
	if !s.CanInstall {
		return s, errors.New(s.Reason)
	}
	if version != m.latest.Version {
		return s, errors.New("目标版本已变化，请重新检查")
	}
	if m.checkedAt == nil || time.Since(*m.checkedAt) > time.Hour {
		return s, errors.New("版本信息已过期，请重新检查")
	}
	if e := m.paths.preflight(); e != nil {
		m.logger.Printf("update preflight failed: %v", e)
		return s, errors.New("更新预检查失败，请检查目录权限、备份目录及磁盘空间；详情见服务日志")
	}
	j := &Job{ID: uuid.NewString(), From: m.info.Version, To: version, Stage: "downloading", StartedAt: time.Now().UTC(), Total: m.latest.Asset.Size}
	if e := saveJob(m.paths, j); e != nil {
		return s, errors.New("无法保存更新任务")
	}
	m.job = j
	rel := *m.latest
	go m.download(rel, j)
	return m.statusLocked(), nil
}
func finishJob(p Paths, j *Job, stage, message string) error {
	now := time.Now().UTC()
	j.Stage = stage
	j.Message = message
	j.FinishedAt = &now
	return saveJob(p, j)
}
func (m *Manager) download(rel Release, initial *Job) {
	j := *initial
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Minute)
	defer cancel()
	e := m.client.download(ctx, m.paths, rel, &j)
	if e != nil {
		m.logger.Printf("update download failed: %v", e)
		if se := finishJob(m.paths, &j, "failed", "下载或校验失败，当前版本未改变；请重试，详情见服务日志"); se != nil {
			m.logger.Printf("update state failed: %v", se)
		}
		m.mu.Lock()
		m.job = &j
		m.mu.Unlock()
		return
	}
	j.Stage = "pending"
	j.Message = "正在准备重启"
	if e = saveJob(m.paths, &j); e != nil {
		m.logger.Printf("update pending state failed: %v", e)
		m.mu.Lock()
		j.Stage = "failed"
		j.Message = "无法保存更新任务，当前版本未改变"
		m.job = &j
		m.mu.Unlock()
		return
	}
	// The supervisor, not this child, takes the backup after all processes stop.
	m.stop()
}
func cleanupDownload(p Paths) {
	os.Remove(filepath.Join(p.State, "download.tar.gz"))
	os.Remove(filepath.Join(p.State, "candidate"))
}

// ClearRecovery is part of a confirmed full reset: retained snapshots contain
// the same private accounts/configuration as the live data being erased.
func (m *Manager) ClearRecovery() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.statusLocked().Job.Active() {
		return errors.New("update is active")
	}
	for _, path := range []string{filepath.Join(m.paths.Backup, "previous"), filepath.Join(m.paths.Backup, "preparing"), filepath.Join(m.paths.State, "candidate"), filepath.Join(m.paths.State, "download.tar.gz"), filepath.Join(m.paths.State, "job.json")} {
		if _, e := os.Lstat(path); errors.Is(e, os.ErrNotExist) {
			continue
		} else if e != nil {
			return e
		}
		if e := noLinks(path); e != nil {
			return e
		}
		if e := os.RemoveAll(path); e != nil {
			return e
		}
	}
	m.job = nil
	return nil
}

// WaitForCommit gates external policy writes while this candidate is under
// observation. Reads and local startup run normally; cancellation sends no write.
func (m *Manager) WaitForCommit(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		j, err := readJob(m.paths)
		if err != nil {
			return fmt.Errorf("read update commit: %w", err)
		}
		if j == nil || j.To != m.info.Version || j.Stage != "verifying_startup" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
