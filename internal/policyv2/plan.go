package policyv2

import (
	"time"

	"rosboard/internal/accesscontrol"
)

type DesiredObject struct {
	LogicalID string
	Menu      string
	Fields    map[string]string
	Order     int
	Phase     string
}

type ActualObject struct {
	LogicalID string
	Menu      string
	RouterID  string
	Position  int
	Fields    map[string]string
	Ownership string
}

type PlanIssue struct {
	Code      string `json:"code"`
	Status    string `json:"status"`
	Family    string `json:"family,omitempty"`
	EgressID  string `json:"egressID,omitempty"`
	LogicalID string `json:"logicalID,omitempty"`
	Reason    string `json:"reason"`
}

type PlanAcknowledgement struct {
	Code     string `json:"code"`
	Required bool   `json:"required"`
	Accepted bool   `json:"accepted"`
}

type PlanOperation struct {
	Seq       int               `json:"seq"`
	Order     int               `json:"-"`
	Phase     string            `json:"phase"`
	Action    string            `json:"action"`
	Menu      string            `json:"menu"`
	LogicalID string            `json:"logicalID"`
	RouterID  string            `json:"routerID,omitempty"`
	Ownership string            `json:"ownership"`
	Before    map[string]string `json:"before,omitempty"`
	After     map[string]string `json:"after,omitempty"`
	Anchor    *PlanAnchor       `json:"anchor,omitempty"`
}

type PlanAnchor struct {
	LogicalID string `json:"logicalID,omitempty"`
	RouterID  string `json:"routerID,omitempty"`
	Relation  string `json:"relation"`
	Menu      string `json:"menu,omitempty"`
}

type PlanSummary struct {
	Create          int `json:"create"`
	Patch           int `json:"patch"`
	Delete          int `json:"delete"`
	Move            int `json:"move"`
	Disable         int `json:"disable"`
	Enable          int `json:"enable"`
	ReferenceAdd    int `json:"referenceAdd"`
	ReferenceRemove int `json:"referenceRemove"`
	Reuse           int `json:"reuse"`
	Adopt           int `json:"adopt"`
	Blockers        int `json:"blockers"`
	Warnings        int `json:"warnings"`
	FamilyBlockers  int `json:"familyBlockers"`
}

type Plan struct {
	PlanID                   string                                             `json:"planID"`
	DeviceID                 string                                             `json:"deviceID"`
	Kind                     string                                             `json:"kind"`
	Lifecycle                string                                             `json:"lifecycle"`
	CreatedAt                time.Time                                          `json:"createdAt"`
	ExpiresAt                time.Time                                          `json:"expiresAt"`
	DesiredRevision          int64                                              `json:"desiredRevision"`
	AccessRevision           int64                                              `json:"accessRevision,omitempty"`
	AccessResolutionCount    int                                                `json:"accessResolutionCount,omitempty"`
	InternetEgressCandidates map[string][]accesscontrol.InternetEgressCandidate `json:"internetEgressCandidates,omitempty"`
	DesiredHash              string                                             `json:"desiredHash,omitempty"`
	ActualFingerprint        string                                             `json:"actualFingerprint"`
	Blockers                 []PlanIssue                                        `json:"blockers"`
	FamilyBlockers           []PlanIssue                                        `json:"familyBlockers,omitempty"`
	Warnings                 []PlanIssue                                        `json:"warnings"`
	Acknowledgements         []PlanAcknowledgement                              `json:"acknowledgements"`
	OwnershipStrict          bool                                               `json:"ownershipStrict"`
	Summary                  PlanSummary                                        `json:"summary"`
	Operations               []PlanOperation                                    `json:"operations"`
	PlanHash                 string                                             `json:"planHash"`
	State                    string                                             `json:"state"`
}

type PlanEnvelope struct {
	Plan     Plan   `json:"plan"`
	PlanID   string `json:"planId"`
	PlanHash string `json:"planHash"`
}

type cachedPlan struct {
	Plan              Plan
	Desired           []DesiredObject
	Actual            []ActualObject
	AccessResolutions []accesscontrol.MemberResolution
	InternetEgresses  map[string][]string
}
