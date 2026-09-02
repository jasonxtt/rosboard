package policyv2

import (
	"context"
	"errors"
	"time"

	"rosboard/internal/accesscontrol"
)

var (
	ErrEgressNotFound      = errors.New("policy egress not found")
	ErrSourceNotFound      = errors.New("policy source not found")
	ErrRevisionStale       = errors.New("policy object revision is stale")
	ErrEgressInUse         = errors.New("policy egress still has assigned sources")
	ErrSourceInUse         = errors.New("policy source is used by access control")
	ErrRoutingRuleRequired = errors.New("source routing association must be managed by a routing rule")
	ErrJobNotFound         = errors.New("policy apply job not found")

	ErrTargetListNotFound      = errors.New("policy target list not found")
	ErrTargetListInUse         = ErrSourceInUse
	ErrTargetListKindImmutable = errors.New("target list kind cannot be changed")
	ErrTargetListTypeImmutable = errors.New("target list source type cannot be changed")
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
	CommitApply(context.Context, int64, int64, string, ApplyJob, []accesscontrol.MemberResolution, bool) error
}

// DomainApplyRepository is the explicit apply contract used by the manager.
// CommitApply remains on Repository as a compatibility path for older
// integrations and narrow test fakes.
type DomainApplyRepository interface {
	CommitRoutingApply(context.Context, int64, string, ApplyJob, []TargetVersionPromotion) error
	CommitAccessApply(context.Context, int64, string, ApplyJob, []accesscontrol.MemberResolution, []TargetVersionPromotion) error
}

type TargetConsumerDomainRepository interface {
	TargetConsumerDomains(context.Context, string) (TargetConsumerDomains, error)
}

// TargetListRepository is the canonical Target Library contract. The legacy
// Repository above remains Source-shaped for policy-routing compatibility in
// Slice 1; both contracts are backed by the same policy-v2 store.
type TargetListRepository interface {
	DeviceID() string

	ListTargetLists(context.Context) ([]TargetList, error)
	GetTargetList(context.Context, string) (TargetList, error)
	SaveTargetList(context.Context, TargetList) (TargetList, error)
	DeleteTargetList(context.Context, string, int64) error
	SavePendingTargetListVersion(context.Context, TargetListVersion, []TargetListRule) error
	SaveTargetListRefresh(context.Context, TargetList, TargetListRefresh, time.Time) error
	ListTargetListVersions(context.Context, string) ([]TargetListVersion, error)
	ListTargetListRules(context.Context, string, RuleQuery) ([]TargetListRule, bool, error)
}
