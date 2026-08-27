package policyv2

import "time"

const (
	ListModeShared    = "shared"
	ListModeDedicated = "dedicated"

	FamilyIPv4 AddressFamily = "ipv4"
	FamilyIPv6 AddressFamily = "ipv6"
)

type AddressFamily string

type EgressFamily struct {
	Family       AddressFamily `json:"family"`
	Enabled      bool          `json:"enabled"`
	WANInterface string        `json:"wanInterface"`
	Gateway      string        `json:"gateway"`
	RouteTable   string        `json:"routeTable"`
	RouteMode    string        `json:"routeMode"`
	NATMode      string        `json:"natMode"`
	WANSource    string        `json:"wanSource"`
}

type Egress struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Priority        int            `json:"priority"`
	ListMode        string         `json:"listMode"`
	ListName        string         `json:"listName"`
	DNSUpstream     string         `json:"dnsUpstream"`
	FakeAlias       string         `json:"fakeAlias"`
	FailureMode     string         `json:"failureMode"`
	RouterOutput    bool           `json:"routerOutput"`
	Enabled         bool           `json:"enabled"`
	Revision        int64          `json:"revision"`
	PendingDeletion bool           `json:"pendingDeletion"`
	Applied         bool           `json:"applied"`
	Families        []EgressFamily `json:"families"`
	CreatedAt       time.Time      `json:"-"`
	UpdatedAt       time.Time      `json:"-"`
}

type Source struct {
	ID                string          `json:"id"`
	EgressID          string          `json:"egressId"`
	Type              string          `json:"type"`
	Name              string          `json:"name"`
	URL               string          `json:"url,omitempty"`
	Schedule          string          `json:"schedule"`
	Enabled           bool            `json:"enabled"`
	ActiveVersionID   string          `json:"activeVersionId"`
	PendingVersionID  string          `json:"-"`
	LastGoodVersionID string          `json:"lastGoodVersionId"`
	ETag              string          `json:"etag,omitempty"`
	LastModified      string          `json:"lastModified,omitempty"`
	NextRunAt         time.Time       `json:"nextRunAt,omitempty"`
	Revision          int64           `json:"revision"`
	PendingDeletion   bool            `json:"pendingDeletion"`
	Applied           bool            `json:"-"`
	Versions          []SourceVersion `json:"versions"`
	Counts            map[string]int  `json:"counts"`
	CreatedAt         time.Time       `json:"-"`
	UpdatedAt         time.Time       `json:"-"`
}

type SourceVersion struct {
	ID             string         `json:"id"`
	SourceID       string         `json:"sourceId"`
	SHA256         string         `json:"sha256"`
	CompressedYAML []byte         `json:"-"`
	State          string         `json:"state"`
	Error          string         `json:"error,omitempty"`
	HTTPStatus     int            `json:"httpStatus,omitempty"`
	Counts         map[string]int `json:"counts"`
	Diff           map[string]any `json:"diff"`
	CreatedAt      time.Time      `json:"createdAt"`
}

type SourceRule struct {
	VersionID string `json:"-"`
	RuleType  string `json:"type"`
	Domain    string `json:"domain"`
}

type SourceRefresh struct {
	NotModified  bool
	ETag         string
	LastModified string
	Version      *SourceVersion
	Rules        []SourceRule
}

type DeviceState struct {
	DeviceID        string
	TrafficIngress  []byte
	DesiredRevision int64
	AppliedRevision int64
	AppliedHash     string
	AppliedAt       time.Time
	Job             ApplyJob
}

type ApplyJob struct {
	ID         string    `json:"id"`
	PlanID     string    `json:"planId"`
	State      string    `json:"state"`
	Phase      string    `json:"phase"`
	Progress   int       `json:"progress"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	StartedAt  time.Time `json:"startedAt,omitempty"`
	FinishedAt time.Time `json:"finishedAt,omitempty"`
}

func (j ApplyJob) Terminal() bool {
	switch j.State {
	case "committed", "failed":
		return true
	default:
		return false
	}
}

func (s DeviceState) Applied() bool {
	return s.DesiredRevision > 0 && s.DesiredRevision == s.AppliedRevision
}
