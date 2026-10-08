// Package containers owns native Container read models and configuration resolution.
// Container lifecycle writes remain disabled; directory operations are separate.
package containers

type Network struct {
	VETH     string `json:"veth"`
	Bridge   string `json:"bridge"`
	Address  string `json:"address"`
	Gateway  string `json:"gateway"`
	Address6 string `json:"address6"`
	Gateway6 string `json:"gateway6"`
	MAC      string `json:"mac"`
}
type Environment struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
type Mount struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"readOnly"`
	Mode     string `json:"mode"`
}
type Health struct {
	Mode        string `json:"mode"`
	Command     string `json:"command"`
	Interval    string `json:"interval"`
	Timeout     string `json:"timeout"`
	Retries     string `json:"retries"`
	StartPeriod string `json:"startPeriod"`
}
type ImageArchive struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Reference    string `json:"reference"`
	Architecture string `json:"architecture"`
	Bytes        int64  `json:"bytes"`
	SHA256       string `json:"sha256"`
	RemotePath   string `json:"remotePath"`
}
type DirectoryEntry struct {
	ID        string `json:"id"`
	Protected string `json:"protected"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Directory bool   `json:"directory"`
	Bytes     int64  `json:"bytes"`
}
type DirectoryListing struct {
	ID        string             `json:"id"`
	CanCreate bool               `json:"canCreate"`
	Pending   *DirectoryMutation `json:"pending,omitempty"`
	Path      string             `json:"path"`
	Entries   []DirectoryEntry   `json:"entries"`
}
type Draft struct {
	DraftID          string        `json:"draftId"`
	ExistingID       string        `json:"existingId"`
	Name             string        `json:"name"`
	Image            string        `json:"image"`
	ImageSource      string        `json:"imageSource"`
	ArchiveID        string        `json:"archiveId"`
	ArchiveFile      string        `json:"archiveFile"`
	Network          Network       `json:"network"`
	RootDir          string        `json:"rootDir"`
	Command          string        `json:"command"`
	Entrypoint       string        `json:"entrypoint"`
	User             string        `json:"user"`
	Workdir          string        `json:"workdir"`
	Env              []Environment `json:"env"`
	Mounts           []Mount       `json:"mounts"`
	MemoryHigh       string        `json:"memoryHigh"`
	MemoryMax        string        `json:"memoryMax"`
	CPUList          string        `json:"cpuList"`
	StartAfterCreate bool          `json:"startAfterCreate"`
	StartOnBoot      bool          `json:"startOnBoot"`
	Logging          bool          `json:"logging"`
	RestartPolicy    string        `json:"restartPolicy"`
	Health           Health        `json:"health"`
}
type Disk struct {
	Name      string `json:"name"`
	FreeBytes int64  `json:"freeBytes"`
	Writable  bool   `json:"writable"`
}
type Options struct {
	Architecture string         `json:"architecture"`
	Archives     []ImageArchive `json:"archives"`
	Bridges      []string       `json:"bridges"`
	Interfaces   []string       `json:"interfaces"`
	UsedIPs      []string       `json:"usedIPs"`
	Disks        []Disk         `json:"disks"`
	MemoryHigh   string         `json:"memoryHigh"`
	MemoryMax    string         `json:"memoryMax"`
}
type Capabilities struct {
	DirectoryWrites bool     `json:"directoryWrites"`
	Supported       bool     `json:"supported"`
	Writes          bool     `json:"writes"`
	Mode            string   `json:"mode"`
	Version         string   `json:"version"`
	Logs            bool     `json:"logs"`
	Fields          []string `json:"fields"`
	Warnings        []string `json:"warnings"`
}
type Item struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Status        string            `json:"status"`
	Image         string            `json:"image"`
	Network       Network           `json:"network"`
	CPU           string            `json:"cpu"`
	Memory        string            `json:"memory"`
	StartOnBoot   bool              `json:"startOnBoot"`
	Ownership     string            `json:"ownership"`
	SharedVETH    []string          `json:"sharedVeth"`
	EnvLists      []string          `json:"envLists"`
	MountLists    []string          `json:"mountLists"`
	Config        Draft             `json:"config"`
	ImageDefaults map[string]string `json:"imageDefaults"`
}
type Snapshot struct {
	ActiveJob    *Job         `json:"activeJob"`
	Items        []Item       `json:"items"`
	Options      Options      `json:"options"`
	Capabilities Capabilities `json:"capabilities"`
}
type Resolution struct {
	Effective       Draft             `json:"effective"`
	Errors          map[string]string `json:"errors"`
	Defaults        []string          `json:"defaults"`
	ContainerFields map[string]string `json:"containerFields"`
}
type Log struct {
	ID      string `json:"id"`
	Time    string `json:"time"`
	Message string `json:"message"`
}

// Job is shared with the test-only simulator and reserved for future real writes.
type Job struct {
	ID       string   `json:"id"`
	DeviceID string   `json:"deviceId"`
	Action   string   `json:"action"`
	TargetID string   `json:"targetId"`
	State    string   `json:"state"`
	Phase    string   `json:"phase"`
	Progress int      `json:"progress"`
	Error    string   `json:"error"`
	Retained []string `json:"retained"`
}
