package api

type ProjectView struct {
	Name              string `json:"name"`
	Path              string `json:"path"`
	Kind              string `json:"kind"` // "linked" or "parked"
	Mode              string `json:"mode"`
	EffectiveMode     string `json:"effectiveMode"`
	Framework         string `json:"framework"`
	URL               string `json:"url"`
	LANURL            string `json:"lanUrl"`
	LocalDevURL       string `json:"localDevUrl"`
	LocalDevState     string `json:"localDevState"`   // "active", "starting", "stopped", "available"
	LANPreviewState   string `json:"lanPreviewState"` // "ready" or "paused"
	TLSEnabled        bool   `json:"tlsEnabled"`
	Port              int    `json:"port,omitempty"`
	RoutePortOverride int    `json:"routePortOverride,omitempty"`
	Status            string `json:"status"` // "ready", "starting", "stopped", "degraded", "error"
	StatusDetail      string `json:"statusDetail,omitempty"`
	PHPVersion        string `json:"phpVersion,omitempty"`
	PackageManager    string `json:"packageManager,omitempty"`
	StaticDir         string `json:"staticDir,omitempty"`
	DevRunning        bool   `json:"devRunning"`
	DevPid            int    `json:"devPid,omitempty"`
	DevPort           int    `json:"devPort,omitempty"`
	Revision          uint64 `json:"revision,omitempty"`
}

type SystemStatusView struct {
	LANIP               string `json:"lanIp"`
	WindowsPort         int    `json:"windowsPort"`
	HTTPSPort           int    `json:"httpsPort"`
	RouteBasePort       int    `json:"routeBasePort"`
	RoutePortCount      int    `json:"routePortCount"`
	UIPort              int    `json:"uiPort"`
	TLSEnabled          bool   `json:"tlsEnabled"`
	DefaultMode         string `json:"defaultMode"`
	PHPDefaultVersion   string `json:"phpDefaultVersion"`
	WindowsCaddyRunning bool   `json:"windowsCaddyRunning"`
	WSLCaddyRunning     bool   `json:"wslCaddyRunning"`
	// M8 fields are additive so older browser clients can still decode the
	// status response while new clients distinguish the single edge from
	// mirrored networking and Hyper-V policy state.
	CaddyRunning       bool     `json:"caddyRunning,omitempty"`
	CaddyTopology      string   `json:"caddyTopology,omitempty"`
	CaddySystemd       bool     `json:"caddySystemd,omitempty"`
	CaddyLive          bool     `json:"caddyLive,omitempty"`
	MirroredConfigured bool     `json:"mirroredConfigured,omitempty"`
	MirroredNetworking bool     `json:"mirroredNetworking,omitempty"`
	HyperVFirewallOk   bool     `json:"hypervFirewallOk,omitempty"`
	CARootValid        bool     `json:"caRootValid,omitempty"`
	CARootTrusted      bool     `json:"caRootTrusted,omitempty"`
	WSLAvailable       bool     `json:"wslAvailable"`
	FirewallOk         bool     `json:"firewallOk"`
	PHPVersions        []string `json:"phpVersions"`
	TotalProjects      int      `json:"totalProjects"`
	ProtocolVersion    int      `json:"protocolVersion"`
	Revision           uint64   `json:"revision,omitempty"`
	ObservedAt         string   `json:"observedAt,omitempty"`
}

// OverviewView is the single read model used by the browser polling loop. It
// keeps one materialized WSL snapshot consistent across projects, status and
// PHP panels.
type OverviewView struct {
	Projects    []ProjectView    `json:"projects"`
	Status      SystemStatusView `json:"status"`
	PHPVersions []PHPVersionView `json:"phpVersions"`
	Revision    uint64           `json:"revision,omitempty"`
	ObservedAt  string           `json:"observedAt,omitempty"`
	Meta        *OverviewMeta    `json:"meta,omitempty"`
}

// OverviewMeta is operational read-model metadata. It contains no command
// arguments or paths and makes stale-while-revalidate behavior measurable.
type OverviewMeta struct {
	Cache              string `json:"cache"`
	HotAgeMs           int64  `json:"hotAgeMs"`
	ColdAgeMs          int64  `json:"coldAgeMs"`
	DurationMs         int64  `json:"durationMs"`
	WSLCalls           uint64 `json:"wslCalls"`
	WSLCallsDelta      uint64 `json:"wslCallsDelta"`
	WSLDurationMs      int64  `json:"wslDurationMs"`
	WSLDurationDeltaMs int64  `json:"wslDurationDeltaMs"`
}

// MutationResult is shared by HTTP and Wails. An accepted asynchronous
// operation can be followed using OperationID until a terminal Phase arrives.
type MutationResult struct {
	OperationID  string           `json:"operationId"`
	Operation    string           `json:"operation"`
	Phase        string           `json:"phase"`
	Status       string           `json:"status"`
	Revision     uint64           `json:"revision,omitempty"`
	ProjectState *ProjectView     `json:"projectState,omitempty"`
	Warnings     []string         `json:"warnings,omitempty"`
	Error        string           `json:"error,omitempty"`
	ObservedAt   string           `json:"observedAt,omitempty"`
	StartedAt    string           `json:"startedAt,omitempty"`
	UpdatedAt    string           `json:"updatedAt,omitempty"`
	DurationMs   int64            `json:"durationMs,omitempty"`
	PhaseMs      map[string]int64 `json:"phaseMs,omitempty"`
}

type PHPVersionView struct {
	Version    string   `json:"version"`
	Installed  bool     `json:"installed"`
	Configured bool     `json:"configured"`
	Extensions []string `json:"extensions,omitempty"`
}

type DoctorCheckView struct {
	Name      string `json:"name"`
	Status    string `json:"status"` // "OK", "WARN", "FAIL"
	Detail    string `json:"detail"`
	Fixable   bool   `json:"fixable"`
	FixAction string `json:"fixAction,omitempty"`
}

type GlobalConfigView struct {
	DefaultMode       string   `json:"defaultMode"`
	WindowsPort       int      `json:"windowsPort"`
	HTTPSPort         int      `json:"httpsPort"`
	TLSEnabled        bool     `json:"tlsEnabled"`
	PHPDefaultVersion string   `json:"phpDefaultVersion"`
	Allowlist         []string `json:"allowlist"`
}

type ProjectConfigUpdate struct {
	Name          string `json:"name"`
	OperationID   string `json:"operationId,omitempty"`
	Mode          string `json:"mode,omitempty"`
	PHPVersion    string `json:"phpVersion,omitempty"`
	PHPPreset     string `json:"phpPreset,omitempty"`
	TLSEnabled    *bool  `json:"tlsEnabled,omitempty"`
	RoutePort     *int   `json:"routePort,omitempty"`
	RoutePortAuto bool   `json:"routePortAuto,omitempty"`
	StaticDir     string `json:"staticDir,omitempty"`
	DevCommand    string `json:"devCommand,omitempty"`
	DevPort       int    `json:"devPort,omitempty"`
}
