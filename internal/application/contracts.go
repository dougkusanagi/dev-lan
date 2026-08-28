// Package application contains transport-neutral commands and queries.
// Concrete adapters are supplied by the composition root through small ports;
// the services keep those dependencies private so callers cannot reach around
// the use-case boundary.
package application

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/metrics"
)

var ErrUnavailable = errors.New("serviço de aplicação não configurado")

// ApplyResult is the transport-neutral result of a persisted mutation.
// app.App aliases this type while the application package owns the contract.
type ApplyResult struct {
	Warnings []string
	Status   string `json:"status,omitempty"`
	Revision uint64 `json:"revision,omitempty"`
}

// The types below are application read models. They intentionally contain
// values, not adapter instances, so HTTP, CLI and Wails can share the same
// use-case boundary without importing config, platform or app internals.
type EndpointFiles struct {
	Endpoint string
	Token    string
}

type ManagedPaths struct {
	Dir          string
	LogsDir      string
	Caddy        string
	WindowsCaddy string
	WSLCaddy     string
	SecurityLog  string
	CARootExport string
}

type WSLStats struct {
	TotalCalls    uint64
	TotalFailures uint64
	TotalCanceled uint64
	TotalDuration time.Duration
}

type CaddyStatus struct {
	Available    bool   `json:"available"`
	Running      bool   `json:"running"`
	Systemd      bool   `json:"systemd"`
	Live         bool   `json:"live"`
	AdminAddress string `json:"adminAddress"`
	ConfigPath   string `json:"configPath,omitempty"`
	Detail       string `json:"detail,omitempty"`
}

type TopologyStatus struct {
	Topology       string `json:"topology"`
	UnifiedConfig  bool   `json:"unifiedConfig"`
	WindowsConfig  bool   `json:"windowsConfig"`
	WSLConfig      bool   `json:"wslConfig"`
	WindowsRunning bool   `json:"windowsRunning"`
	WSLRunning     bool   `json:"wslRunning"`
}

type CompatibilityCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type PortConflict struct {
	Port   int    `json:"port"`
	Detail string `json:"detail"`
}

type WSLCompatibilityReport struct {
	Supported             bool                 `json:"supported"`
	WindowsVersion        string               `json:"windowsVersion,omitempty"`
	WindowsBuild          int                  `json:"windowsBuild,omitempty"`
	WSLVersion            string               `json:"wslVersion,omitempty"`
	WSL2                  bool                 `json:"wsl2"`
	MirroredConfigured    bool                 `json:"mirroredConfigured"`
	MirroredNetworking    bool                 `json:"mirroredNetworking"`
	Systemd               bool                 `json:"systemd"`
	LoopbackBidirectional bool                 `json:"loopbackBidirectional"`
	LANReachable          bool                 `json:"lanReachable"`
	PortConflicts         []PortConflict       `json:"portConflicts"`
	Checks                []CompatibilityCheck `json:"checks"`
}

type PortRange struct {
	From int `json:"from"`
	To   int `json:"to"`
}

type FirewallSpec struct {
	Ports       []int       `json:"ports"`
	Ranges      []PortRange `json:"ranges"`
	Direction   string      `json:"direction"`
	Action      string      `json:"action"`
	Protocol    string      `json:"protocol"`
	Profile     string      `json:"profile"`
	RemoteIP    string      `json:"remote_ip"`
	RuleName    string      `json:"rule_name"`
	RuleGroup   string      `json:"rule_group"`
	Description string      `json:"description"`
}

type HyperVFirewallSpec struct {
	RuleName             string      `json:"ruleName"`
	DisplayName          string      `json:"displayName"`
	Ports                []int       `json:"ports"`
	Ranges               []PortRange `json:"ranges"`
	Profile              string      `json:"profile"`
	RemoteAddresses      string      `json:"remoteAddresses"`
	VMCreatorID          string      `json:"vmCreatorId"`
	DefaultInboundAction string      `json:"defaultInboundAction"`
	LoopbackEnabled      bool        `json:"loopbackEnabled"`
	AllowHostPolicyMerge bool        `json:"allowHostPolicyMerge"`
}

type HyperVFirewallStatus struct {
	Supported bool                    `json:"supported"`
	Healthy   bool                    `json:"healthy"`
	Detail    string                  `json:"detail,omitempty"`
	Rule      HyperVFirewallRuleState `json:"rule,omitempty"`
	Setting   HyperVVMSettingState    `json:"setting,omitempty"`
	Spec      HyperVFirewallSpec      `json:"spec"`
}

type HyperVFirewallRuleState struct {
	Name          string
	DisplayName   string
	Enabled       bool
	Direction     string
	Action        string
	Protocol      string
	LocalPorts    string
	Profile       string
	RemoteAddress string
	VMCreatorID   string
}

type HyperVVMSettingState struct {
	Name                 string
	DefaultInboundAction string
	LoopbackEnabled      bool
	AllowHostPolicyMerge bool
}

type FirewallSnapshot struct {
	Healthy bool         `json:"healthy"`
	Spec    FirewallSpec `json:"spec"`
	Detail  string       `json:"detail,omitempty"`
}

type TopologySnapshot struct {
	Topology      TopologyStatus         `json:"topology"`
	Caddy         CaddyStatus            `json:"caddy"`
	Compatibility WSLCompatibilityReport `json:"compatibility"`
	Firewall      FirewallSnapshot       `json:"firewall"`
	HyperV        *HyperVFirewallStatus  `json:"hyperv,omitempty"`
	CA            map[string]string      `json:"ca,omitempty"`
	Error         string                 `json:"error,omitempty"`
}

type PHPVersionStatus struct {
	Version    string
	Installed  bool
	Configured bool
	Extensions []string
}

type RouteAllocation struct {
	Path   string `json:"path"`
	Port   int    `json:"port"`
	Orphan bool   `json:"orphan"`
}

type DevProcessStatus struct {
	ProjectName string
	Port        int
	State       string
	PID         int
	Output      string
}

const (
	DevStateStopped  = "stopped"
	DevStateStarting = "starting"
	DevStateRunning  = "running"
	DevStateError    = "error"
)

type DoctorCheck struct {
	Name   string
	Status string
	Detail string
}

type NetworkProfile struct {
	Public bool
	Detail string
}

type MigrationStep string

type MigrationResult struct {
	Topology          string          `json:"topology"`
	BackupDir         string          `json:"backupDir,omitempty"`
	UnifiedBackupPath string          `json:"unifiedBackupPath,omitempty"`
	Steps             []MigrationStep `json:"steps"`
	RolledBack        bool            `json:"rolledBack"`
}

type TelemetryStatus struct {
	Enabled  bool
	Endpoint string
	Queued   int
}

type ProjectRuntimeSnapshot struct {
	Config      domain.Config
	Effective   domain.Config
	EdgeReady   bool
	WSLReady    bool
	Caddy       CaddyStatus
	Host        string
	Sockets     map[string]bool
	SocketError string
	DevStatuses map[string]DevProcessStatus
}

type SystemHealthSnapshot struct {
	Topology           TopologyStatus
	WSLAvailable       bool
	FirewallOK         bool
	HyperVOK           bool
	CAValid            bool
	CATrusted          bool
	MirroredConfigured bool
	MirroredNetworking bool
	PHPVersions        []PHPVersionStatus
}

// OperationState is the shared progress record for asynchronous mutations.
// ProjectState stays JSON to keep the application layer independent of the
// HTTP read model.
type OperationState struct {
	OperationID  string
	Operation    string
	ProjectName  string
	Transport    string
	Phase        string
	Status       string
	Revision     uint64
	ProjectState json.RawMessage
	Warnings     []string
	Error        string
	StartedAt    time.Time
	UpdatedAt    time.Time
	FinishedAt   time.Time
	PhaseMs      map[string]int64
}

type UninstallOptions struct {
	DryRun           bool
	KeepData         bool
	KeepDependencies bool
	Purge            bool
	Yes              bool
}

type UninstallAction string

const (
	UninstallRemove   UninstallAction = "remove"
	UninstallRestore  UninstallAction = "restore"
	UninstallPreserve UninstallAction = "preserve"
	UninstallConflict UninstallAction = "conflict"
	UninstallPending  UninstallAction = "pending"
	UninstallFailed   UninstallAction = "failed"
)

type UninstallItem struct {
	ID           string          `json:"id"`
	Scope        string          `json:"scope"`
	Kind         string          `json:"kind"`
	Target       string          `json:"target"`
	Action       UninstallAction `json:"action"`
	Detail       string          `json:"detail,omitempty"`
	Distribution string          `json:"distribution,omitempty"`
}

type UninstallPlan struct {
	Version          int             `json:"version"`
	DataDir          string          `json:"dataDir"`
	Manifest         bool            `json:"manifest"`
	Legacy           bool            `json:"legacy"`
	ProjectCount     int             `json:"projectCount"`
	KeepData         bool            `json:"keepData"`
	KeepDependencies bool            `json:"keepDependencies"`
	Purge            bool            `json:"purge"`
	Pending          bool            `json:"pending"`
	Items            []UninstallItem `json:"items"`
	Warnings         []string        `json:"warnings,omitempty"`
}

type UninstallResult struct {
	ApplyResult
	Plan      UninstallPlan `json:"plan"`
	Completed bool          `json:"completed"`
}

var ErrWSLShutdownConfirmation = errors.New("confirmação obrigatória: wsl --shutdown encerra todas as distribuições")

func (o UninstallOptions) Validate() error {
	if o.Purge && !o.Yes {
		return errors.New("uninstall --purge exige confirmação explícita com --yes")
	}
	return nil
}

// GlobalSettings is the validated intent used by the global-settings command.
// It deliberately contains no persistence or transport fields.
type GlobalSettings struct {
	DefaultMode       string
	WindowsPort       int
	HTTPSPort         int
	TLSEnabled        bool
	PHPDefaultVersion string
	Allowlist         []string
}

type LinkProjectCommand struct {
	Name string
	Path string
}

type UnlinkProjectCommand struct {
	Name string
}

type ParkDirectoryCommand struct {
	Path string
}

type UnparkDirectoryCommand struct {
	Path string
}

type IgnoreProjectCommand struct {
	Selector string
}

type UnignoreProjectCommand struct {
	Path string
}

type SetDefaultModeCommand struct {
	Mode domain.Mode
}

type SetProjectModeCommand struct {
	Name string
	Mode *domain.Mode
}

// ProjectCommandPort is the minimum mutation surface required by project
// commands. app.App satisfies it without the application package depending on
// the concrete app implementation.
type ProjectCommandPort interface {
	Link(context.Context, string, string) (domain.Project, ApplyResult, error)
	Unlink(context.Context, string) (domain.Project, ApplyResult, error)
	Park(context.Context, string) (domain.Park, ApplyResult, error)
	Unpark(context.Context, string) (domain.Park, ApplyResult, error)
	IgnoreProject(context.Context, string) (ApplyResult, error)
	UnignoreProject(context.Context, string) (ApplyResult, error)
	SetDefaultMode(context.Context, domain.Mode) (ApplyResult, error)
	SetProjectMode(context.Context, string, *domain.Mode) (ApplyResult, error)
}

type SettingsCommandPort interface {
	SaveGlobalSettings(context.Context, GlobalSettings) (ApplyResult, error)
}

// ExtendedCommandPort contains the application operations that used to be
// called directly on app.App by one of the transports. It is intentionally
// kept as one private-to-the-composition port for this migration; R-06 will
// split it into smaller adapter-facing ports without changing callers.
type ExtendedCommandPort interface {
	EnsureState() error
	Audit(string, string)
	CloseDevProxies() error
	InstallWithPort(context.Context, bool, int) (ApplyResult, error)
	UninstallWithOptions(context.Context, UninstallOptions) (UninstallResult, error)
	Reload(context.Context) (ApplyResult, error)
	Trust(context.Context) error
	SetTLS(context.Context, bool) (ApplyResult, error)
	SetProjectTLS(context.Context, string, bool) (ApplyResult, string, error)
	SetProjectStaticDir(context.Context, string, string) (ApplyResult, error)
	SetProjectDevPort(context.Context, string, int) (ApplyResult, error)
	SetProjectDevCommand(context.Context, string, string) (ApplyResult, error)
	SetProjectPackageManager(context.Context, string, string) (ApplyResult, error)
	SetRoutePort(context.Context, string, *int) (ApplyResult, error)
	PruneRouteAllocations(context.Context, bool) ([]string, ApplyResult, error)
	ExposeProject(context.Context, string, time.Duration) (ApplyResult, string, error)
	UnexposeProject(context.Context, string) (ApplyResult, string, error)
	SetAllowlist(context.Context, string, []string) (ApplyResult, error)
	AddAllowlist(context.Context, string, []string) (ApplyResult, error)
	RemoveAllowlist(context.Context, string, []string) (ApplyResult, error)
	ClearAllowlist(context.Context, string) (ApplyResult, error)
	SetAuth(context.Context, string, bool, string, string) (ApplyResult, error)
	DisableAuth(context.Context, string) (ApplyResult, error)
	ExportCA(context.Context, string) (string, error)
	RotateCA(context.Context) (ApplyResult, error)
	PHPInstall(context.Context, string, []string) (ApplyResult, error)
	PHPRemove(context.Context, string) (ApplyResult, error)
	SetDefaultPHPVersion(context.Context, string) (ApplyResult, error)
	SetPHPVersionExtensions(context.Context, string, []string) (ApplyResult, error)
	SetProjectPHPVersion(context.Context, string, string) (ApplyResult, error)
	SetProjectPHPPreset(context.Context, string, string) (ApplyResult, error)
	SetProjectPHPIsolated(context.Context, string, bool) (ApplyResult, error)
	SetPHPGlobalPool(context.Context, domain.PHPFPMPoolConfig) (ApplyResult, error)
	SetPHPVersionPool(context.Context, string, domain.PHPFPMPoolConfig) (ApplyResult, error)
	RunComposer(context.Context, string, string, []string) (string, error)
	SetComposerEnvironment(context.Context, string, string) (ApplyResult, error)
	StartDev(context.Context, string) error
	StopDev(context.Context, string) error
	RestartDev(context.Context, string) error
	BuildProject(context.Context, string) (string, error)
	InstallDeps(context.Context, string) (string, error)
	Open(context.Context, string) (string, error)
	ImportConfig(context.Context, []byte) (ApplyResult, error)
	DiagnosticBundle(context.Context, string) (string, error)
	ReconcileFirewall(context.Context) error
	RepairM8(context.Context) (ApplyResult, error)
	MigrateTopology(context.Context, bool) (MigrationResult, error)
	SetTelemetryConsent(bool, string) error
	SendTelemetry(context.Context) (int, error)
	BeginOperation(string, string, string) (OperationState, bool, error)
	SetOperationTransport(string, string)
	UpdateOperation(string, string, string, uint64, json.RawMessage, []string, error) OperationState
}

// Commands exposes validated command DTOs while keeping the concrete
// mutation ports private to the application service.
type Commands struct {
	projects ProjectCommandPort
	settings SettingsCommandPort
	extended ExtendedCommandPort
}

func NewCommands(projects ProjectCommandPort, settings SettingsCommandPort) *Commands {
	commands := &Commands{projects: projects, settings: settings}
	if port, ok := projects.(ExtendedCommandPort); ok {
		commands.extended = port
	} else if port, ok := settings.(ExtendedCommandPort); ok {
		commands.extended = port
	}
	return commands
}

func (c *Commands) extendedPort() ExtendedCommandPort {
	if c == nil {
		return nil
	}
	return c.extended
}

func (c *Commands) EnsureState() error {
	if port := c.extendedPort(); port != nil {
		return port.EnsureState()
	}
	return ErrUnavailable
}

func (c *Commands) Audit(event, details string) {
	if port := c.extendedPort(); port != nil {
		port.Audit(event, details)
	}
}

func (c *Commands) CloseDevProxies() error {
	if port := c.extendedPort(); port != nil {
		return port.CloseDevProxies()
	}
	return ErrUnavailable
}

func (c *Commands) InstallWithPort(ctx context.Context, configureFirewall bool, windowsPort int) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.InstallWithPort(ctx, configureFirewall, windowsPort)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) UninstallWithOptions(ctx context.Context, options UninstallOptions) (UninstallResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.UninstallWithOptions(ctx, options)
	}
	return UninstallResult{}, ErrUnavailable
}

func (c *Commands) Reload(ctx context.Context) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.Reload(ctx)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) Trust(ctx context.Context) error {
	if port := c.extendedPort(); port != nil {
		return port.Trust(ctx)
	}
	return ErrUnavailable
}

func (c *Commands) SetTLS(ctx context.Context, enabled bool) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.SetTLS(ctx, enabled)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) SetProjectTLS(ctx context.Context, selector string, enabled bool) (ApplyResult, string, error) {
	if port := c.extendedPort(); port != nil {
		return port.SetProjectTLS(ctx, selector, enabled)
	}
	return ApplyResult{}, "", ErrUnavailable
}

func (c *Commands) SetProjectStaticDir(ctx context.Context, selector, value string) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.SetProjectStaticDir(ctx, selector, value)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) SetProjectDevPort(ctx context.Context, selector string, portValue int) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.SetProjectDevPort(ctx, selector, portValue)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) SetProjectDevCommand(ctx context.Context, selector, value string) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.SetProjectDevCommand(ctx, selector, value)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) SetProjectPackageManager(ctx context.Context, selector, value string) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.SetProjectPackageManager(ctx, selector, value)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) SetRoutePort(ctx context.Context, selector string, portValue *int) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.SetRoutePort(ctx, selector, portValue)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) PruneRouteAllocations(ctx context.Context, dryRun bool) ([]string, ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.PruneRouteAllocations(ctx, dryRun)
	}
	return nil, ApplyResult{}, ErrUnavailable
}

func (c *Commands) ExposeProject(ctx context.Context, selector string, duration time.Duration) (ApplyResult, string, error) {
	if port := c.extendedPort(); port != nil {
		return port.ExposeProject(ctx, selector, duration)
	}
	return ApplyResult{}, "", ErrUnavailable
}

func (c *Commands) UnexposeProject(ctx context.Context, selector string) (ApplyResult, string, error) {
	if port := c.extendedPort(); port != nil {
		return port.UnexposeProject(ctx, selector)
	}
	return ApplyResult{}, "", ErrUnavailable
}

func (c *Commands) SetAllowlist(ctx context.Context, selector string, cidrs []string) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.SetAllowlist(ctx, selector, cidrs)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) AddAllowlist(ctx context.Context, selector string, cidrs []string) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.AddAllowlist(ctx, selector, cidrs)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) RemoveAllowlist(ctx context.Context, selector string, cidrs []string) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.RemoveAllowlist(ctx, selector, cidrs)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) ClearAllowlist(ctx context.Context, selector string) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.ClearAllowlist(ctx, selector)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) SetAuth(ctx context.Context, selector string, enabled bool, username, password string) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.SetAuth(ctx, selector, enabled, username, password)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) DisableAuth(ctx context.Context, selector string) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.DisableAuth(ctx, selector)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) ExportCA(ctx context.Context, target string) (string, error) {
	if port := c.extendedPort(); port != nil {
		return port.ExportCA(ctx, target)
	}
	return "", ErrUnavailable
}

func (c *Commands) RotateCA(ctx context.Context) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.RotateCA(ctx)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) PHPInstall(ctx context.Context, version string, extensions []string) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.PHPInstall(ctx, version, extensions)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) PHPRemove(ctx context.Context, version string) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.PHPRemove(ctx, version)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) SetDefaultPHPVersion(ctx context.Context, version string) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.SetDefaultPHPVersion(ctx, version)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) SetPHPVersionExtensions(ctx context.Context, version string, extensions []string) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.SetPHPVersionExtensions(ctx, version, extensions)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) SetProjectPHPVersion(ctx context.Context, selector, version string) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.SetProjectPHPVersion(ctx, selector, version)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) SetProjectPHPPreset(ctx context.Context, selector, preset string) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.SetProjectPHPPreset(ctx, selector, preset)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) SetProjectPHPIsolated(ctx context.Context, selector string, isolated bool) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.SetProjectPHPIsolated(ctx, selector, isolated)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) SetPHPGlobalPool(ctx context.Context, pool domain.PHPFPMPoolConfig) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.SetPHPGlobalPool(ctx, pool)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) SetPHPVersionPool(ctx context.Context, version string, pool domain.PHPFPMPoolConfig) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.SetPHPVersionPool(ctx, version, pool)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) RunComposer(ctx context.Context, selector, environment string, args []string) (string, error) {
	if port := c.extendedPort(); port != nil {
		return port.RunComposer(ctx, selector, environment, args)
	}
	return "", ErrUnavailable
}

func (c *Commands) SetComposerEnvironment(ctx context.Context, selector, value string) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.SetComposerEnvironment(ctx, selector, value)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) StartDev(ctx context.Context, selector string) error {
	if port := c.extendedPort(); port != nil {
		return port.StartDev(ctx, selector)
	}
	return ErrUnavailable
}

func (c *Commands) StopDev(ctx context.Context, selector string) error {
	if port := c.extendedPort(); port != nil {
		return port.StopDev(ctx, selector)
	}
	return ErrUnavailable
}

func (c *Commands) RestartDev(ctx context.Context, selector string) error {
	if port := c.extendedPort(); port != nil {
		return port.RestartDev(ctx, selector)
	}
	return ErrUnavailable
}

func (c *Commands) BuildProject(ctx context.Context, selector string) (string, error) {
	if port := c.extendedPort(); port != nil {
		return port.BuildProject(ctx, selector)
	}
	return "", ErrUnavailable
}

func (c *Commands) InstallDeps(ctx context.Context, selector string) (string, error) {
	if port := c.extendedPort(); port != nil {
		return port.InstallDeps(ctx, selector)
	}
	return "", ErrUnavailable
}

func (c *Commands) Open(ctx context.Context, selector string) (string, error) {
	if port := c.extendedPort(); port != nil {
		return port.Open(ctx, selector)
	}
	return "", ErrUnavailable
}

func (c *Commands) ImportConfig(ctx context.Context, data []byte) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.ImportConfig(ctx, data)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) DiagnosticBundle(ctx context.Context, target string) (string, error) {
	if port := c.extendedPort(); port != nil {
		return port.DiagnosticBundle(ctx, target)
	}
	return "", ErrUnavailable
}

func (c *Commands) ReconcileFirewall(ctx context.Context) error {
	if port := c.extendedPort(); port != nil {
		return port.ReconcileFirewall(ctx)
	}
	return ErrUnavailable
}

func (c *Commands) RepairM8(ctx context.Context) (ApplyResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.RepairM8(ctx)
	}
	return ApplyResult{}, ErrUnavailable
}

func (c *Commands) MigrateTopology(ctx context.Context, confirmed bool) (MigrationResult, error) {
	if port := c.extendedPort(); port != nil {
		return port.MigrateTopology(ctx, confirmed)
	}
	return MigrationResult{}, ErrUnavailable
}

func (c *Commands) SetTelemetryConsent(enabled bool, endpoint string) error {
	if port := c.extendedPort(); port != nil {
		return port.SetTelemetryConsent(enabled, endpoint)
	}
	return ErrUnavailable
}

func (c *Commands) SendTelemetry(ctx context.Context) (int, error) {
	if port := c.extendedPort(); port != nil {
		return port.SendTelemetry(ctx)
	}
	return 0, ErrUnavailable
}

func (c *Commands) BeginOperation(id, operation, project string) (OperationState, bool, error) {
	if port := c.extendedPort(); port != nil {
		return port.BeginOperation(id, operation, project)
	}
	return OperationState{}, false, ErrUnavailable
}

func (c *Commands) SetOperationTransport(id, transport string) {
	if port := c.extendedPort(); port != nil {
		port.SetOperationTransport(id, transport)
	}
}

func (c *Commands) UpdateOperation(id, phase, status string, revision uint64, projectState json.RawMessage, warnings []string, operationErr error) OperationState {
	if port := c.extendedPort(); port != nil {
		return port.UpdateOperation(id, phase, status, revision, projectState, warnings, operationErr)
	}
	return OperationState{}
}

func (c *Commands) LinkProject(ctx context.Context, command LinkProjectCommand) (domain.Project, ApplyResult, error) {
	if c == nil || c.projects == nil {
		return domain.Project{}, ApplyResult{}, ErrUnavailable
	}
	return c.projects.Link(ctx, command.Name, command.Path)
}

func (c *Commands) UnlinkProject(ctx context.Context, command UnlinkProjectCommand) (domain.Project, ApplyResult, error) {
	if c == nil || c.projects == nil {
		return domain.Project{}, ApplyResult{}, ErrUnavailable
	}
	return c.projects.Unlink(ctx, command.Name)
}

func (c *Commands) ParkDirectory(ctx context.Context, command ParkDirectoryCommand) (domain.Park, ApplyResult, error) {
	if c == nil || c.projects == nil {
		return domain.Park{}, ApplyResult{}, ErrUnavailable
	}
	return c.projects.Park(ctx, command.Path)
}

func (c *Commands) UnparkDirectory(ctx context.Context, command UnparkDirectoryCommand) (domain.Park, ApplyResult, error) {
	if c == nil || c.projects == nil {
		return domain.Park{}, ApplyResult{}, ErrUnavailable
	}
	return c.projects.Unpark(ctx, command.Path)
}

func (c *Commands) IgnoreProject(ctx context.Context, command IgnoreProjectCommand) (ApplyResult, error) {
	if c == nil || c.projects == nil {
		return ApplyResult{}, ErrUnavailable
	}
	return c.projects.IgnoreProject(ctx, command.Selector)
}

func (c *Commands) UnignoreProject(ctx context.Context, command UnignoreProjectCommand) (ApplyResult, error) {
	if c == nil || c.projects == nil {
		return ApplyResult{}, ErrUnavailable
	}
	return c.projects.UnignoreProject(ctx, command.Path)
}

func (c *Commands) SetDefaultMode(ctx context.Context, command SetDefaultModeCommand) (ApplyResult, error) {
	if c == nil || c.projects == nil {
		return ApplyResult{}, ErrUnavailable
	}
	return c.projects.SetDefaultMode(ctx, command.Mode)
}

func (c *Commands) SetProjectMode(ctx context.Context, command SetProjectModeCommand) (ApplyResult, error) {
	if c == nil || c.projects == nil {
		return ApplyResult{}, ErrUnavailable
	}
	return c.projects.SetProjectMode(ctx, command.Name, command.Mode)
}

func (c *Commands) SaveGlobalSettings(ctx context.Context, settings GlobalSettings) (ApplyResult, error) {
	if c == nil || c.settings == nil {
		return ApplyResult{}, ErrUnavailable
	}
	return c.settings.SaveGlobalSettings(ctx, settings)
}

// QueryPort is the read surface needed by the critical configuration and
// effective-project queries. It is intentionally narrower than app.App.
type QueryPort interface {
	Config() (domain.Config, error)
	EffectiveConfig(context.Context, domain.Config) (domain.Config, error)
	Revision() uint64
}

// ExtendedQueryPort is the read side of the same application boundary. The
// concrete app may use WSL, Caddy, the filesystem and the network to produce
// these snapshots, but transports only see stable value objects.
type ExtendedQueryPort interface {
	EndpointFilesSnapshot() EndpointFiles
	ManagedPathsSnapshot() ManagedPaths
	LANAddressSnapshot() string
	ClockNow() time.Time
	WSLStatsSnapshot() WSLStats
	ProjectRuntime(context.Context) (ProjectRuntimeSnapshot, error)
	SystemHealth(context.Context, CaddyStatus) SystemHealthSnapshot
	CaddyStatusSnapshot(context.Context) CaddyStatus
	TopologyStatusSnapshot(context.Context) TopologyStatus
	CompatibilityReport(context.Context) WSLCompatibilityReport
	TopologySnapshot(context.Context) TopologySnapshot
	NetworkProfileSnapshot(context.Context) NetworkProfile
	PHPVersions(context.Context) ([]PHPVersionStatus, error)
	RouteAllocations(context.Context) ([]RouteAllocation, error)
	DoctorSnapshot(context.Context, string) ([]DoctorCheck, error)
	URL(context.Context, string) (string, error)
	Logs(string) (string, error)
	ProjectDevLogs(context.Context, string, int) (string, error)
	DevStatusSnapshot(context.Context, string) (DevProcessStatus, error)
	CheckLANAddressDivergence() (string, string, bool)
	CAInfo(context.Context) (map[string]string, error)
	SecurityAuditLogs(context.Context, int) (string, error)
	PHPInfo(context.Context, string) (string, error)
	MetricsSnapshot(context.Context, string, string) (*metrics.Snapshot, error)
	TelemetryStatus() (TelemetryStatus, error)
	ExportConfig() ([]byte, error)
	Operation(string) (OperationState, bool)
	SubscribeOperations(context.Context) (<-chan OperationState, func())
}

type Queries struct {
	source   QueryPort
	extended ExtendedQueryPort
}

func NewQueries(source QueryPort) *Queries {
	queries := &Queries{source: source}
	if port, ok := source.(ExtendedQueryPort); ok {
		queries.extended = port
	}
	return queries
}

func (q *Queries) extendedPort() ExtendedQueryPort {
	if q == nil {
		return nil
	}
	return q.extended
}

func (q *Queries) EndpointFiles() EndpointFiles {
	if port := q.extendedPort(); port != nil {
		return port.EndpointFilesSnapshot()
	}
	return EndpointFiles{}
}

func (q *Queries) ManagedPaths() ManagedPaths {
	if port := q.extendedPort(); port != nil {
		return port.ManagedPathsSnapshot()
	}
	return ManagedPaths{}
}

func (q *Queries) LANAddress() string {
	if port := q.extendedPort(); port != nil {
		return port.LANAddressSnapshot()
	}
	return "localhost"
}

func (q *Queries) Now() time.Time {
	if port := q.extendedPort(); port != nil {
		return port.ClockNow()
	}
	return time.Now()
}

func (q *Queries) WSLStats() WSLStats {
	if port := q.extendedPort(); port != nil {
		return port.WSLStatsSnapshot()
	}
	return WSLStats{}
}

func (q *Queries) ProjectRuntime(ctx context.Context) (ProjectRuntimeSnapshot, error) {
	if port := q.extendedPort(); port != nil {
		return port.ProjectRuntime(ctx)
	}
	return ProjectRuntimeSnapshot{}, ErrUnavailable
}

func (q *Queries) SystemHealth(ctx context.Context, caddy CaddyStatus) SystemHealthSnapshot {
	if port := q.extendedPort(); port != nil {
		return port.SystemHealth(ctx, caddy)
	}
	return SystemHealthSnapshot{}
}

func (q *Queries) CaddyStatus(ctx context.Context) CaddyStatus {
	if port := q.extendedPort(); port != nil {
		return port.CaddyStatusSnapshot(ctx)
	}
	return CaddyStatus{}
}

func (q *Queries) TopologyStatus(ctx context.Context) TopologyStatus {
	if port := q.extendedPort(); port != nil {
		return port.TopologyStatusSnapshot(ctx)
	}
	return TopologyStatus{}
}

func (q *Queries) Compatibility(ctx context.Context) WSLCompatibilityReport {
	if port := q.extendedPort(); port != nil {
		return port.CompatibilityReport(ctx)
	}
	return WSLCompatibilityReport{}
}

func (q *Queries) Topology(ctx context.Context) TopologySnapshot {
	if port := q.extendedPort(); port != nil {
		return port.TopologySnapshot(ctx)
	}
	return TopologySnapshot{}
}

func (q *Queries) NetworkProfile(ctx context.Context) NetworkProfile {
	if port := q.extendedPort(); port != nil {
		return port.NetworkProfileSnapshot(ctx)
	}
	return NetworkProfile{}
}

func (q *Queries) PHPVersions(ctx context.Context) ([]PHPVersionStatus, error) {
	if port := q.extendedPort(); port != nil {
		return port.PHPVersions(ctx)
	}
	return nil, ErrUnavailable
}

func (q *Queries) RouteAllocations(ctx context.Context) ([]RouteAllocation, error) {
	if port := q.extendedPort(); port != nil {
		return port.RouteAllocations(ctx)
	}
	return nil, ErrUnavailable
}

func (q *Queries) Doctor(ctx context.Context, project string) ([]DoctorCheck, error) {
	if port := q.extendedPort(); port != nil {
		return port.DoctorSnapshot(ctx, project)
	}
	return nil, ErrUnavailable
}

func (q *Queries) URL(ctx context.Context, selector string) (string, error) {
	if port := q.extendedPort(); port != nil {
		return port.URL(ctx, selector)
	}
	return "", ErrUnavailable
}

func (q *Queries) Logs(component string) (string, error) {
	if port := q.extendedPort(); port != nil {
		return port.Logs(component)
	}
	return "", ErrUnavailable
}

func (q *Queries) ProjectDevLogs(ctx context.Context, selector string, lines int) (string, error) {
	if port := q.extendedPort(); port != nil {
		return port.ProjectDevLogs(ctx, selector, lines)
	}
	return "", ErrUnavailable
}

func (q *Queries) DevStatus(ctx context.Context, selector string) (DevProcessStatus, error) {
	if port := q.extendedPort(); port != nil {
		return port.DevStatusSnapshot(ctx, selector)
	}
	return DevProcessStatus{}, ErrUnavailable
}

func (q *Queries) CheckLANAddressDivergence() (string, string, bool) {
	if port := q.extendedPort(); port != nil {
		return port.CheckLANAddressDivergence()
	}
	return "", "", false
}

func (q *Queries) CAInfo(ctx context.Context) (map[string]string, error) {
	if port := q.extendedPort(); port != nil {
		return port.CAInfo(ctx)
	}
	return nil, ErrUnavailable
}

func (q *Queries) SecurityAuditLogs(ctx context.Context, lines int) (string, error) {
	if port := q.extendedPort(); port != nil {
		return port.SecurityAuditLogs(ctx, lines)
	}
	return "", ErrUnavailable
}

func (q *Queries) PHPInfo(ctx context.Context, selector string) (string, error) {
	if port := q.extendedPort(); port != nil {
		return port.PHPInfo(ctx, selector)
	}
	return "", ErrUnavailable
}

func (q *Queries) MetricsSnapshot(ctx context.Context, project, rawRange string) (*metrics.Snapshot, error) {
	if port := q.extendedPort(); port != nil {
		return port.MetricsSnapshot(ctx, project, rawRange)
	}
	return nil, ErrUnavailable
}

func (q *Queries) TelemetryStatus() (TelemetryStatus, error) {
	if port := q.extendedPort(); port != nil {
		return port.TelemetryStatus()
	}
	return TelemetryStatus{}, ErrUnavailable
}

func (q *Queries) ExportConfig() ([]byte, error) {
	if port := q.extendedPort(); port != nil {
		return port.ExportConfig()
	}
	return nil, ErrUnavailable
}

func (q *Queries) Operation(id string) (OperationState, bool) {
	if port := q.extendedPort(); port != nil {
		return port.Operation(id)
	}
	return OperationState{}, false
}

func (q *Queries) SubscribeOperations(ctx context.Context) (<-chan OperationState, func()) {
	if port := q.extendedPort(); port != nil {
		return port.SubscribeOperations(ctx)
	}
	channel := make(chan OperationState)
	close(channel)
	return channel, func() {}
}

func (q *Queries) Config(ctx context.Context) (domain.Config, error) {
	if q == nil || q.source == nil {
		return domain.Config{}, ErrUnavailable
	}
	if err := contextError(ctx); err != nil {
		return domain.Config{}, err
	}
	return q.source.Config()
}

func (q *Queries) EffectiveConfig(ctx context.Context, cfg domain.Config) (domain.Config, error) {
	if q == nil || q.source == nil {
		return domain.Config{}, ErrUnavailable
	}
	if err := contextError(ctx); err != nil {
		return domain.Config{}, err
	}
	return q.source.EffectiveConfig(ctx, cfg)
}

func (q *Queries) Revision(ctx context.Context) uint64 {
	if q == nil || q.source == nil || contextError(ctx) != nil {
		return 0
	}
	return q.source.Revision()
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
