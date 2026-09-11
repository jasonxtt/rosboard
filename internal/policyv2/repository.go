package policyv2

import (
	"context"
	"errors"
	"time"

	"rosboard/internal/accesscontrol"
)

var (
	ErrEgressNotFound      = errors.New("出口不存在")
	ErrSourceNotFound      = errors.New("目标列表不存在")
	ErrRevisionStale       = errors.New("内容已被其他修改更新，请刷新后重试")
	ErrEgressInUse         = errors.New("出口仍被来源引用")
	ErrSourceInUse         = errors.New("目标列表仍被访问控制使用")
	ErrRoutingRuleRequired = errors.New("来源的路由关联必须由策略路由规则管理")
	ErrJobNotFound         = errors.New("应用任务不存在")

	ErrTargetListNotFound      = errors.New("目标列表不存在")
	ErrTargetListInUse         = ErrSourceInUse
	ErrTargetListKindImmutable = errors.New("目标列表类型不可修改")
	ErrTargetListTypeImmutable = errors.New("目标列表来源类型不可修改")
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

// DomainApplyJobStateRepository is the follow-up-aware commit contract. A
// non-terminal domain commit persists its applied state and the same job in a
// non-terminal state atomically, allowing another domain apply to continue
// without exposing a transient committed job.
type DomainApplyJobStateRepository interface {
	CommitRoutingApplyWithJobState(context.Context, int64, string, ApplyJob, []TargetVersionPromotion, bool) error
	CommitAccessApplyWithJobState(context.Context, int64, string, ApplyJob, []accesscontrol.MemberResolution, []TargetVersionPromotion, bool) error
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
