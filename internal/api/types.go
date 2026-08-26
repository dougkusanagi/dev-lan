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
}

type SystemStatusView struct {
	LANIP               string   `json:"lanIp"`
	WindowsPort         int      `json:"windowsPort"`
	HTTPSPort           int      `json:"httpsPort"`
	RouteBasePort       int      `json:"routeBasePort"`
	RoutePortCount      int      `json:"routePortCount"`
	UIPort              int      `json:"uiPort"`
	TLSEnabled          bool     `json:"tlsEnabled"`
	DefaultMode         string   `json:"defaultMode"`
	PHPDefaultVersion   string   `json:"phpDefaultVersion"`
	WindowsCaddyRunning bool     `json:"windowsCaddyRunning"`
	WSLCaddyRunning     bool     `json:"wslCaddyRunning"`
	WSLAvailable        bool     `json:"wslAvailable"`
	FirewallOk          bool     `json:"firewallOk"`
	PHPVersions         []string `json:"phpVersions"`
	TotalProjects       int      `json:"totalProjects"`
	ProtocolVersion     int      `json:"protocolVersion"`
}

// OverviewView is the single read model used by the browser polling loop. It
// keeps one materialized WSL snapshot consistent across projects, status and
// PHP panels.
type OverviewView struct {
	Projects    []ProjectView    `json:"projects"`
	Status      SystemStatusView `json:"status"`
	PHPVersions []PHPVersionView `json:"phpVersions"`
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
