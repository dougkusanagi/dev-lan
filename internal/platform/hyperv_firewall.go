package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

var ErrHyperVFirewallNotFound = errors.New("regra do Hyper-V Firewall DevLAN não encontrada")

// WSL's registered Hyper-V firewall creator is stable in the Windows 11
// mirrored-networking contract. Keeping it explicit prevents the rule from
// accidentally applying to unrelated Hyper-V guests.
const WSLHyperVMCreatorID = "{40E0AC32-46A5-438A-A0B2-2B479E8F2E90}"

// HyperVFirewallSpec describes the policy applied to traffic entering the
// mirrored WSL interface. The default inbound action is explicitly Block;
// DevLAN opens only the assigned TCP listeners through a managed rule.
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

func DefaultHyperVFirewallSpec() HyperVFirewallSpec {
	return HyperVFirewallSpec{
		RuleName:             "DevLAN-HyperV",
		DisplayName:          "DevLAN mirrored WSL LAN",
		Ports:                []int{80, 443},
		Ranges:               []PortRange{{From: 8080, To: 8179}},
		Profile:              "Private",
		RemoteAddresses:      "LocalSubnet",
		VMCreatorID:          WSLHyperVMCreatorID,
		DefaultInboundAction: "Block",
		LoopbackEnabled:      true,
		AllowHostPolicyMerge: false,
	}
}

func HyperVFirewallSpecForConfig(cfg domain.Config) HyperVFirewallSpec {
	base := DefaultHyperVFirewallSpec()
	windows := FirewallSpecForConfig(cfg)
	base.Ports = append([]int(nil), windows.Ports...)
	base.Ranges = append([]PortRange(nil), windows.Ranges...)
	return normalizeHyperVSpec(base)
}

// NormalizeHyperVFirewallSpec is exported so the desired state can be shown
// in diagnostics without exposing the PowerShell implementation.
func NormalizeHyperVFirewallSpec(spec HyperVFirewallSpec) HyperVFirewallSpec {
	return normalizeHyperVSpec(spec)
}

func normalizeHyperVSpec(spec HyperVFirewallSpec) HyperVFirewallSpec {
	defaults := DefaultHyperVFirewallSpec()
	if strings.TrimSpace(spec.RuleName) == "" {
		spec.RuleName = defaults.RuleName
	}
	if strings.TrimSpace(spec.DisplayName) == "" {
		spec.DisplayName = defaults.DisplayName
	}
	if strings.TrimSpace(spec.Profile) == "" {
		spec.Profile = defaults.Profile
	}
	if strings.TrimSpace(spec.RemoteAddresses) == "" {
		spec.RemoteAddresses = defaults.RemoteAddresses
	}
	if strings.TrimSpace(spec.VMCreatorID) == "" {
		spec.VMCreatorID = defaults.VMCreatorID
	}
	spec.DefaultInboundAction = "Block"
	// A mirrored interface needs host loopback to reach the Windows API, but
	// the setting must never turn inbound traffic into an allow-all policy.
	spec.LoopbackEnabled = true
	spec.AllowHostPolicyMerge = false
	ports := uniqueFirewallPorts(spec.Ports)
	spec.Ports = ports
	spec.Ranges = mergeFirewallRanges(spec.Ranges)
	return spec
}

func uniqueFirewallPorts(values []int) []int {
	seen := make(map[int]bool, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value < 1 || value > 65535 || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}

func mergeFirewallRanges(values []PortRange) []PortRange {
	filtered := make([]PortRange, 0, len(values))
	for _, value := range values {
		if value.From < 1 || value.From > value.To || value.To > 65535 {
			continue
		}
		filtered = append(filtered, value)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].From == filtered[j].From {
			return filtered[i].To < filtered[j].To
		}
		return filtered[i].From < filtered[j].From
	})
	result := make([]PortRange, 0, len(filtered))
	for _, current := range filtered {
		if len(result) == 0 || current.From > result[len(result)-1].To+1 {
			result = append(result, current)
			continue
		}
		if current.To > result[len(result)-1].To {
			result[len(result)-1].To = current.To
		}
	}
	return result
}

func hyperVPortExpression(spec HyperVFirewallSpec) string {
	spec = normalizeHyperVSpec(spec)
	parts := make([]string, 0, len(spec.Ports)+len(spec.Ranges))
	for _, port := range spec.Ports {
		parts = append(parts, strconv.Itoa(port))
	}
	for _, portRange := range spec.Ranges {
		parts = append(parts, fmt.Sprintf("%d-%d", portRange.From, portRange.To))
	}
	return strings.Join(parts, ",")
}

// hyperVPortArgument renders the string[] expected by the NetSecurity
// Hyper-V cmdlets. Passing the comma-joined expression as one quoted string
// makes PowerShell treat the complete value as a single port name and reject
// the rule (for example, "80,443,8080-8179").
func hyperVPortArgument(spec HyperVFirewallSpec) string {
	spec = normalizeHyperVSpec(spec)
	parts := make([]string, 0, len(spec.Ports)+len(spec.Ranges))
	for _, port := range spec.Ports {
		parts = append(parts, fmt.Sprintf("\"%d\"", port))
	}
	for _, portRange := range spec.Ranges {
		parts = append(parts, fmt.Sprintf("\"%d-%d\"", portRange.From, portRange.To))
	}
	return "@(" + strings.Join(parts, ",") + ")"
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

type HyperVFirewallStatus struct {
	Supported bool                    `json:"supported"`
	Healthy   bool                    `json:"healthy"`
	Detail    string                  `json:"detail,omitempty"`
	Rule      HyperVFirewallRuleState `json:"rule,omitempty"`
	Setting   HyperVVMSettingState    `json:"setting,omitempty"`
	Spec      HyperVFirewallSpec      `json:"spec"`
}

type HyperVFirewall struct {
	// Runner is normally a PowerShell runner. Keeping it injectable makes the
	// exact command contract testable on non-Windows CI.
	Runner Runner
}

func (f HyperVFirewall) runner() Runner {
	if f.Runner != nil {
		return f.Runner
	}
	return NewExecRunner("powershell.exe")
}

func (f HyperVFirewall) Inspect(ctx context.Context, spec HyperVFirewallSpec) (HyperVFirewallRuleState, error) {
	if runtime.GOOS != "windows" && f.Runner == nil {
		return HyperVFirewallRuleState{}, fmt.Errorf("Hyper-V Firewall só é suportado no Windows")
	}
	spec = normalizeHyperVSpec(spec)
	// Name and VMCreatorId belong to different parameter sets on the Get
	// cmdlet. Name is unique within a policy store, so retrieve by Name and
	// verify VMCreatorId as part of hyperVRuleMatches instead. A missing CDXML
	// instance exits powershell.exe with status 1 even with SilentlyContinue;
	// handle its stable error ID in PowerShell so this remains locale-neutral.
	command := "try { Get-NetFirewallHyperVRule -Name '" + escapePowerShellSingleQuoted(spec.RuleName) + "' -ErrorAction Stop | Select-Object Name,DisplayName,Enabled,Direction,Action,Protocol,LocalPorts,Profiles,RemoteAddresses,VMCreatorId | ConvertTo-Json -Compress } catch { if ($_.FullyQualifiedErrorId -like 'CmdletizationQuery_NotFound_InstanceID*') { exit 0 }; throw }"
	out, err := f.runner().Run(ctx, "-NoProfile", "-NonInteractive", "-Command", command)
	if err != nil {
		if hyperVObjectMissing(out, err) {
			return HyperVFirewallRuleState{}, ErrHyperVFirewallNotFound
		}
		return HyperVFirewallRuleState{}, err
	}
	state := parseHyperVRuleState(out)
	if state.Name == "" {
		return HyperVFirewallRuleState{}, ErrHyperVFirewallNotFound
	}
	return state, nil
}

func (f HyperVFirewall) inspectVMSetting(ctx context.Context, spec HyperVFirewallSpec) (HyperVVMSettingState, error) {
	command := "Get-NetFirewallHyperVVMSetting -Name '" + escapePowerShellSingleQuoted(spec.VMCreatorID) + "' -PolicyStore ActiveStore | Select-Object Name,DefaultInboundAction,LoopbackEnabled,AllowHostPolicyMerge | ConvertTo-Json -Compress"
	out, err := f.runner().Run(ctx, "-NoProfile", "-NonInteractive", "-Command", command)
	if err != nil {
		return HyperVVMSettingState{}, err
	}
	var object map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &object) == nil {
		state := HyperVVMSettingState{}
		for rawKey, rawValue := range object {
			key := strings.ToLower(rawKey)
			value := jsonScalarString(rawValue)
			switch key {
			case "name":
				state.Name = value
			case "defaultinboundaction":
				state.DefaultInboundAction = hyperVActionName(value)
			case "loopbackenabled":
				state.LoopbackEnabled = hyperVBoolValue(value)
			case "allowhostpolicymerge":
				state.AllowHostPolicyMerge = hyperVBoolValue(value)
			}
		}
		if state.Name == "" {
			state.Name = spec.VMCreatorID
		}
		return state, nil
	}
	return HyperVVMSettingState{}, fmt.Errorf("resposta de configuração Hyper-V inválida")
}

func hyperVObjectMissing(output string, err error) bool {
	text := strings.ToLower(output)
	if err != nil {
		text += " " + strings.ToLower(err.Error())
	}
	for _, marker := range []string{
		"not found",
		"cannot find",
		"does not exist",
		"cmdletizationquery_notfound_instanceid",
		"no msft_netfirewallhyperv",
		"no msft_netfirewallhypervvmsetting",
		"not recognized",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func hyperVBoolValue(value string) bool {
	return strings.EqualFold(value, "true") || value == "1" || strings.EqualFold(value, "enabled")
}

func hyperVActionName(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "4", "block":
		return "Block"
	case "2", "allow":
		return "Allow"
	default:
		return value
	}
}

func hyperVProfileName(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "2", "private":
		return "Private"
	case "4", "public":
		return "Public"
	case "1", "domain":
		return "Domain"
	default:
		return value
	}
}

func escapePowerShellSingleQuoted(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func parseHyperVRuleState(output string) HyperVFirewallRuleState {
	// PowerShell emits compact JSON in production; the line parser below keeps
	// the adapter convenient for simple test doubles and localized output.
	state := HyperVFirewallRuleState{}
	var object map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(output)), &object) == nil {
		for rawKey, rawValue := range object {
			key := strings.ToLower(rawKey)
			value := jsonScalarString(rawValue)
			switch key {
			case "name":
				state.Name = value
			case "displayname":
				state.DisplayName = value
			case "enabled":
				state.Enabled = hyperVBoolValue(value)
			case "direction":
				state.Direction = value
			case "action":
				state.Action = hyperVActionName(value)
			case "protocol":
				state.Protocol = value
			case "localports":
				state.LocalPorts = value
			case "profile", "profiles":
				state.Profile = hyperVProfileName(value)
			case "remoteaddresses", "remoteaddress":
				state.RemoteAddress = value
			case "vmcreatorid":
				state.VMCreatorID = value
			}
		}
		return state
	}
	for _, raw := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		line := strings.Trim(strings.TrimSpace(raw), "{},\"")
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			parts = strings.SplitN(line, "=", 2)
		}
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.Trim(strings.TrimSpace(parts[0]), "\""))
		value := strings.Trim(strings.TrimSpace(parts[1]), "\",")
		switch key {
		case "name":
			state.Name = value
		case "displayname":
			state.DisplayName = value
		case "enabled":
			state.Enabled = hyperVBoolValue(value)
		case "direction":
			state.Direction = value
		case "action":
			state.Action = hyperVActionName(value)
		case "protocol":
			state.Protocol = value
		case "localports":
			state.LocalPorts = value
		case "profile", "profiles":
			state.Profile = hyperVProfileName(value)
		case "remoteaddresses", "remoteaddress":
			state.RemoteAddress = value
		case "vmcreatorid":
			state.VMCreatorID = value
		}
	}
	return state
}

func jsonScalarString(value any) string {
	switch item := value.(type) {
	case string:
		return item
	case bool:
		return strconv.FormatBool(item)
	case float64:
		return strconv.FormatFloat(item, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(item))
		for _, entry := range item {
			parts = append(parts, jsonScalarString(entry))
		}
		return strings.Join(parts, ",")
	default:
		return fmt.Sprint(item)
	}
}

func (f HyperVFirewall) Reconcile(ctx context.Context, spec HyperVFirewallSpec) error {
	if runtime.GOOS != "windows" && f.Runner == nil {
		return nil
	}
	spec = normalizeHyperVSpec(spec)
	state, err := f.Inspect(ctx, spec)
	if errors.Is(err, ErrHyperVFirewallNotFound) {
		_, err = f.runner().Run(ctx, "-NoProfile", "-NonInteractive", "-Command", hyperVCreateCommand(spec))
		if err != nil {
			return fmt.Errorf("criar regra Hyper-V Firewall: %w", err)
		}
	} else if err != nil {
		return err
	} else if !hyperVRuleMatches(state, spec) {
		_, err = f.runner().Run(ctx, "-NoProfile", "-NonInteractive", "-Command", hyperVSetCommand(spec))
		if err != nil {
			return fmt.Errorf("reconciliar regra Hyper-V Firewall: %w", err)
		}
	}
	// This is deliberately a separate command from the rule reconciliation. A
	// malformed rule must not cause the global Hyper-V inbound action to become
	// permissive as a side effect. Inspect first so repeated reload/repair calls
	// are true no-ops when the VM policy already matches.
	setting, settingErr := f.inspectVMSetting(ctx, spec)
	if settingErr != nil || !hyperVVMSettingMatches(setting, spec) {
		_, err = f.runner().Run(ctx, "-NoProfile", "-NonInteractive", "-Command", hyperVVMSettingCommand(spec))
		if err != nil {
			return fmt.Errorf("reconciliar política padrão Hyper-V Firewall: %w", err)
		}
	}
	return nil
}

func hyperVRuleMatches(state HyperVFirewallRuleState, spec HyperVFirewallSpec) bool {
	spec = normalizeHyperVSpec(spec)
	return strings.EqualFold(state.Name, spec.RuleName) &&
		strings.EqualFold(state.DisplayName, spec.DisplayName) && state.Enabled &&
		strings.EqualFold(state.Direction, "Inbound") && strings.EqualFold(state.Action, "Allow") &&
		strings.EqualFold(state.Protocol, "TCP") && strings.ReplaceAll(state.LocalPorts, " ", "") == hyperVPortExpression(spec) &&
		strings.EqualFold(state.Profile, spec.Profile) && strings.EqualFold(state.RemoteAddress, spec.RemoteAddresses) &&
		(strings.TrimSpace(state.VMCreatorID) == "" || strings.EqualFold(state.VMCreatorID, spec.VMCreatorID))
}

func hyperVCreateCommand(spec HyperVFirewallSpec) string {
	spec = normalizeHyperVSpec(spec)
	return fmt.Sprintf("New-NetFirewallHyperVRule -Name '%s' -DisplayName '%s' -Direction Inbound -Action Allow -Enabled True -Protocol TCP -LocalPorts %s -Profiles %s -RemoteAddresses %s -VMCreatorId '%s'", escapePowerShellSingleQuoted(spec.RuleName), escapePowerShellSingleQuoted(spec.DisplayName), hyperVPortArgument(spec), spec.Profile, spec.RemoteAddresses, escapePowerShellSingleQuoted(spec.VMCreatorID))
}

func hyperVSetCommand(spec HyperVFirewallSpec) string {
	spec = normalizeHyperVSpec(spec)
	return fmt.Sprintf("Set-NetFirewallHyperVRule -Name '%s' -NewDisplayName '%s' -Direction Inbound -Action Allow -Enabled True -Protocol TCP -LocalPorts %s -Profiles %s -RemoteAddresses %s -VMCreatorId '%s'", escapePowerShellSingleQuoted(spec.RuleName), escapePowerShellSingleQuoted(spec.DisplayName), hyperVPortArgument(spec), spec.Profile, spec.RemoteAddresses, escapePowerShellSingleQuoted(spec.VMCreatorID))
}

func hyperVVMSettingCommand(spec HyperVFirewallSpec) string {
	return fmt.Sprintf("Set-NetFirewallHyperVVMSetting -Name '%s' -DefaultInboundAction Block -LoopbackEnabled %t -AllowHostPolicyMerge %t", escapePowerShellSingleQuoted(spec.VMCreatorID), spec.LoopbackEnabled, spec.AllowHostPolicyMerge)
}

func hyperVVMSettingMatches(state HyperVVMSettingState, spec HyperVFirewallSpec) bool {
	return strings.EqualFold(state.Name, spec.VMCreatorID) &&
		strings.EqualFold(state.DefaultInboundAction, "Block") &&
		state.LoopbackEnabled && !state.AllowHostPolicyMerge
}

func (f HyperVFirewall) Status(ctx context.Context, spec HyperVFirewallSpec) HyperVFirewallStatus {
	spec = normalizeHyperVSpec(spec)
	if runtime.GOOS != "windows" && f.Runner == nil {
		return HyperVFirewallStatus{Supported: false, Spec: spec, Detail: "não se aplica fora do Windows"}
	}
	rule, err := f.Inspect(ctx, spec)
	if err != nil {
		return HyperVFirewallStatus{Supported: true, Healthy: false, Spec: spec, Detail: err.Error()}
	}
	setting, settingErr := f.inspectVMSetting(ctx, spec)
	if settingErr != nil {
		return HyperVFirewallStatus{Supported: true, Healthy: false, Rule: rule, Spec: spec, Detail: settingErr.Error()}
	}
	healthy := hyperVRuleMatches(rule, spec) && hyperVVMSettingMatches(setting, spec)
	return HyperVFirewallStatus{Supported: true, Healthy: healthy, Rule: rule, Setting: setting, Spec: spec}
}

// CompositeFirewall performs the coordinated policy update. The Windows
// Firewall remains the host policy and Hyper-V Firewall is the mirrored-WSL
// policy; neither adapter is allowed to broaden the other's scope.
type CompositeFirewall struct {
	Windows SystemFirewall
	HyperV  HyperVFirewall
}

// Ensure preserves the legacy port while routing normal callers through the
// complete range-aware reconciliation path. Callers that need exact desired
// state should use FirewallReconciler.Reconcile.
func (f CompositeFirewall) Ensure(ctx context.Context, ports ...int) error {
	spec := DefaultFirewallSpec()
	spec.Ports = append([]int(nil), ports...)
	return f.Reconcile(ctx, spec)
}

func (f CompositeFirewall) Reconcile(ctx context.Context, spec FirewallSpec) error {
	if err := f.Windows.Reconcile(ctx, spec); err != nil && runtime.GOOS == "windows" {
		return err
	}
	return f.HyperV.Reconcile(ctx, HyperVFirewallSpec{
		RuleName:             "DevLAN-HyperV",
		DisplayName:          "DevLAN mirrored WSL LAN",
		Ports:                spec.Ports,
		Ranges:               spec.Ranges,
		Profile:              "Private",
		RemoteAddresses:      "LocalSubnet",
		VMCreatorID:          WSLHyperVMCreatorID,
		DefaultInboundAction: "Block",
		LoopbackEnabled:      true,
		AllowHostPolicyMerge: false,
	})
}

func (f CompositeFirewall) Inspect(ctx context.Context) (FirewallRuleState, error) {
	return f.Windows.Inspect(ctx)
}

func (f CompositeFirewall) Remove(ctx context.Context) error {
	if err := f.Windows.Remove(ctx); err != nil && runtime.GOOS == "windows" {
		return err
	}
	if runtime.GOOS != "windows" && f.HyperV.Runner == nil {
		return nil
	}
	// As with Get-NetFirewallHyperVRule, Name and VMCreatorId cannot be used
	// together because they select different parameter sets.
	_, err := f.HyperV.runner().Run(ctx, "-NoProfile", "-NonInteractive", "-Command", "Remove-NetFirewallHyperVRule -Name 'DevLAN-HyperV' -ErrorAction SilentlyContinue")
	return err
}

func (f CompositeFirewall) HyperVStatus(ctx context.Context, cfgSpec FirewallSpec) HyperVFirewallStatus {
	return f.HyperV.Status(ctx, HyperVFirewallSpec{
		RuleName:             "DevLAN-HyperV",
		DisplayName:          "DevLAN mirrored WSL LAN",
		Ports:                cfgSpec.Ports,
		Ranges:               cfgSpec.Ranges,
		Profile:              "Private",
		RemoteAddresses:      "LocalSubnet",
		VMCreatorID:          WSLHyperVMCreatorID,
		DefaultInboundAction: "Block",
		LoopbackEnabled:      true,
		AllowHostPolicyMerge: false,
	})
}
