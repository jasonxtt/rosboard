package policyv2

import (
	"context"
	"errors"
	"time"
)

var (
	ErrEgressNotFound = errors.New("policy egress not found")
	ErrSourceNotFound = errors.New("policy source not found")
	ErrRevisionStale  = errors.New("policy object revision is stale")
	ErrEgressInUse    = errors.New("policy egress still has assigned sources")
	ErrJobNotFound    = errors.New("policy apply job not found")
)

type RuleQuery struct {
	AfterType   string
	AfterDomain string
	Query       string
	RuleType    string
	Limit       int
}

type Repository interface {
	DeviceID() string
	ManagerInstanceID(context.Context) (string, error)

	ListEgresses(context.Context) ([]Egress, error)
	GetEgress(context.Context, string) (Egress, error)
	SaveEgress(context.Context, Egress) (Egress, error)
	DeleteEgress(context.Context, string, int64) error

	ListSources(context.Context, string) ([]Source, error)
	GetSource(context.Context, string) (Source, error)
	SaveSource(context.Context, Source) (Source, error)
	DeleteSource(context.Context, string, int64) error
	SavePendingSourceVersion(context.Context, SourceVersion, []SourceRule) error
	SaveSourceRefresh(context.Context, Source, SourceRefresh, time.Time) error
	ListSourceVersions(context.Context, string) ([]SourceVersion, error)
	ListSourceRules(context.Context, string, RuleQuery) ([]SourceRule, bool, error)

	GetDeviceState(context.Context) (DeviceState, error)
	SaveTrafficIngress(context.Context, []byte) (DeviceState, error)
	SaveApplyJob(context.Context, ApplyJob) error
	GetApplyJob(context.Context, string) (ApplyJob, error)
	CommitApply(context.Context, int64, string, ApplyJob) error
}
