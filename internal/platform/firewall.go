package platform

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

var (
	ErrFirewallNotFound = errors.New("regra de firewall DevLAN não encontrada")
	ErrFirewallConflict = errors.New("regra de firewall existente não pertence ao DevLAN")
)

// FirewallManager is kept as the small compatibility boundary used by older
// callers. New application paths use FirewallReconciler below.
type FirewallManager interface {
	Ensure(context.Context, ...int) error
	Remove(context.Context) error
}

// FirewallReconciler is the range-aware firewall boundary. The adapter must
// inspect before mutating so a third-party rule with the same display name is
// never overwritten.
type FirewallReconciler interface {
	Reconcile(context.Context, FirewallSpec) error
	Inspect(context.Context) (FirewallRuleState, error)
	Remove(context.Context) error
}

type PortRange struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// FirewallSpec is a pure desired-state description. Ports outside the route
// pool (for example an explicit route override) are represented in Ports;
// regular route capacity is represented compactly in Ranges.
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

type FirewallRuleState struct {
	Name        string
	Group       string
	Description string
	Enabled     string
	Direction   string
	Action      string
	Protocol    string
	LocalPorts  string
	Profile     string
	RemoteIP    string
}

const (
	FirewallRuleName        = "DevLAN"
	FirewallRuleGroup       = "DevLAN Managed"
	FirewallRuleDescription = "Managed by DevLAN; do not edit."
)

// DefaultFirewallSpec describes the standard LAN policy independently from a
// Config. It is useful for install/bootstrap and for contract tests.
func DefaultFirewallSpec() FirewallSpec {
	return normalizeFirewallSpec(FirewallSpec{
		Ports:       []int{80, 443},
		Ranges:      []PortRange{{From: 8080, To: 8179}},
		Direction:   "in",
		Action:      "allow",
		Protocol:    "tcp",
		Profile:     "private",
		RemoteIP:    "localsubnet",
		RuleName:    FirewallRuleName,
		RuleGroup:   FirewallRuleGroup,
		Description: FirewallRuleDescription,
	})
}

// FirewallSpecForConfig is the single source of truth used by install, route,
// TLS, repair, doctor and the UI. ui_port is intentionally absent: the admin
// server is loopback-only and must never be opened on the LAN.
func FirewallSpecForConfig(cfg domain.Config) FirewallSpec {
	base, count := cfg.RouteBasePort, cfg.RoutePortCount
	if base == 0 {
		base = 8080
	}
	if count == 0 {
		count = 100
	}
	// M8 removes the Windows edge and therefore removes the configurable host
	// listener from the policy. The unified Caddy binds these two ports in WSL;
	// legacy windows_port/https_port values remain readable for migration only.
	windowsPort, httpsPort := 80, 443
	spec := DefaultFirewallSpec()
	spec.Ports = []int{windowsPort, httpsPort}
	spec.Ranges = []PortRange{{From: base, To: base + count - 1}}
	uiPort := cfg.UIPort
	activePaths := make(map[string]struct{}, len(cfg.Projects))
	for _, project := range cfg.Projects {
		activePaths[project.Path] = struct{}{}
		if project.RoutePort != nil && *project.RoutePort > 0 && !portInRanges(*project.RoutePort, spec.Ranges) {
			if *project.RoutePort != uiPort {
				spec.Ports = append(spec.Ports, *project.RoutePort)
			}
		}
	}
	for path, port := range cfg.RoutePortAllocations {
		// Allocations are intentionally retained as orphan-prune state. They
		// must not become firewall openings after their project disappears. An
		// explicit project override is already covered above; automatic active
		// projects may use the pool range, which is covered compactly there.
		if _, active := activePaths[path]; !active || port == uiPort {
			continue
		}
		if !portInRanges(port, spec.Ranges) {
			spec.Ports = append(spec.Ports, port)
		}
	}
	return normalizeFirewallSpec(spec)
}

func portInRanges(port int, ranges []PortRange) bool {
	for _, portRange := range ranges {
		if port >= portRange.From && port <= portRange.To {
			return true
		}
	}
	return false
}

func normalizeFirewallSpec(spec FirewallSpec) FirewallSpec {
	spec = withFirewallDefaults(spec)

	ports := make([]int, 0, len(spec.Ports))
	seen := map[int]struct{}{}
	for _, port := range spec.Ports {
		if port >= 1 && port <= 65535 {
			if _, exists := seen[port]; !exists {
				seen[port] = struct{}{}
				ports = append(ports, port)
			}
		}
	}
	sort.Ints(ports)
	ranges := make([]PortRange, 0, len(spec.Ranges))
	for _, portRange := range spec.Ranges {
		if portRange.From < 1 || portRange.To > 65535 || portRange.From > portRange.To {
			continue
		}
		ranges = append(ranges, portRange)
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].From == ranges[j].From {
			return ranges[i].To < ranges[j].To
		}
		return ranges[i].From < ranges[j].From
	})
	merged := make([]PortRange, 0, len(ranges))
	for _, current := range ranges {
		if len(merged) == 0 || current.From > merged[len(merged)-1].To+1 {
			merged = append(merged, current)
			continue
		}
		if current.To > merged[len(merged)-1].To {
			merged[len(merged)-1].To = current.To
		}
	}
	filteredPorts := ports[:0]
	for _, port := range ports {
		if !portInRanges(port, merged) {
			filteredPorts = append(filteredPorts, port)
		}
	}
	spec.Ports = filteredPorts
	spec.Ranges = merged
	return spec
}

func withFirewallDefaults(spec FirewallSpec) FirewallSpec {
	if spec.Direction == "" {
		spec.Direction = "in"
	}
	if spec.Action == "" {
		spec.Action = "allow"
	}
	if spec.Protocol == "" {
		spec.Protocol = "tcp"
	}
	if spec.Profile == "" {
		spec.Profile = "private"
	}
	if spec.RemoteIP == "" {
		spec.RemoteIP = "localsubnet"
	}
	if spec.RuleName == "" {
		spec.RuleName = FirewallRuleName
	}
	if spec.RuleGroup == "" {
		spec.RuleGroup = FirewallRuleGroup
	}
	if spec.Description == "" {
		spec.Description = FirewallRuleDescription
	}
	return spec
}

func (spec FirewallSpec) validate() error {
	if strings.TrimSpace(spec.RuleName) == "" || strings.TrimSpace(spec.RuleGroup) == "" || strings.TrimSpace(spec.Description) == "" {
		return errors.New("identidade da regra de firewall incompleta")
	}
	for _, port := range spec.Ports {
		if port < 1 || port > 65535 {
			return fmt.Errorf("porta do firewall inválida: %d", port)
		}
	}
	for _, portRange := range spec.Ranges {
		if portRange.From < 1 || portRange.To > 65535 || portRange.From > portRange.To {
			return fmt.Errorf("faixa do firewall inválida: %d-%d", portRange.From, portRange.To)
		}
	}
	if len(spec.Ports) == 0 && len(spec.Ranges) == 0 {
		return errors.New("ao menos uma porta do firewall é obrigatória")
	}
	return nil
}

func (spec FirewallSpec) localPortExpression() string {
	spec = normalizeFirewallSpec(spec)
	parts := make([]string, 0, len(spec.Ports)+len(spec.Ranges))
	for _, port := range spec.Ports {
		parts = append(parts, strconv.Itoa(port))
	}
	for _, portRange := range spec.Ranges {
		parts = append(parts, fmt.Sprintf("%d-%d", portRange.From, portRange.To))
	}
	return strings.Join(parts, ",")
}

func (rule FirewallRuleState) managed(spec FirewallSpec) bool {
	spec = normalizeFirewallSpec(spec)
	return strings.EqualFold(strings.TrimSpace(rule.Name), spec.RuleName) &&
		(strings.TrimSpace(rule.Group) == "" || strings.EqualFold(strings.TrimSpace(rule.Group), spec.RuleGroup)) &&
		strings.EqualFold(strings.TrimSpace(rule.Description), spec.Description)
}

// legacyDevLANRule recognizes the narrowly-scoped rule created by DevLAN
// releases before managed group/description metadata existed. Keeping this
// predicate strict lets upgrades adopt that rule without taking ownership of
// an unrelated rule that merely happens to use the same display name.
func (rule FirewallRuleState) legacyDevLANRule(spec FirewallSpec) bool {
	spec = normalizeFirewallSpec(spec)
	return equalsFold(rule.Name, spec.RuleName) &&
		strings.TrimSpace(rule.Group) == "" &&
		strings.TrimSpace(rule.Description) == "" &&
		equalsAnyFold(rule.Enabled, "yes", "sim") &&
		equalsAnyFold(rule.Direction, "in", "entrada") &&
		equalsAnyFold(rule.Action, "allow", "permitir") &&
		equalsFold(rule.Protocol, "tcp") &&
		equalsPorts(rule.LocalPorts, "80,443") &&
		equalsAnyFold(rule.Profile, "private", "privado", "particular") &&
		equalsAnyFold(rule.RemoteIP, "localsubnet", "sub-rede local", "rede local")
}

func equalsAnyFold(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if equalsFold(value, candidate) {
			return true
		}
	}
	return false
}

func (rule FirewallRuleState) Matches(spec FirewallSpec) bool {
	spec = normalizeFirewallSpec(spec)
	return rule.managed(spec) &&
		equalsFold(rule.Enabled, "yes") &&
		equalsFold(rule.Direction, spec.Direction) &&
		equalsFold(rule.Action, spec.Action) &&
		equalsFold(rule.Protocol, spec.Protocol) &&
		equalsPorts(rule.LocalPorts, spec.localPortExpression()) &&
		equalsFold(rule.Profile, spec.Profile) &&
		equalsRemoteIP(rule.RemoteIP, spec.RemoteIP)
}

func equalsFold(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func equalsPorts(left, right string) bool {
	return strings.EqualFold(strings.ReplaceAll(strings.TrimSpace(left), " ", ""), strings.ReplaceAll(strings.TrimSpace(right), " ", ""))
}

func equalsRemoteIP(left, right string) bool {
	return strings.EqualFold(strings.ReplaceAll(strings.TrimSpace(left), " ", ""), strings.ReplaceAll(strings.TrimSpace(right), " ", ""))
}

type SystemFirewall struct {
	// Runner is injectable for contract tests and diagnostics. A nil runner
	// uses netsh in production.
	Runner Runner
}

func (s SystemFirewall) runner() Runner {
	if s.Runner != nil {
		return s.Runner
	}
	return NewExecRunner("netsh")
}

func (s SystemFirewall) Ensure(ctx context.Context, ports ...int) error {
	return s.Reconcile(ctx, FirewallSpec{Ports: append([]int(nil), ports...)})
}

func (s SystemFirewall) Inspect(ctx context.Context) (FirewallRuleState, error) {
	if runtime.GOOS != "windows" && s.Runner == nil {
		return FirewallRuleState{}, fmt.Errorf("firewall DevLAN só é suportado no Windows")
	}
	out, err := s.runner().Run(ctx, "advfirewall", "firewall", "show", "rule", "name="+FirewallRuleName, "verbose")
	if err != nil {
		// ExecRunner preserves native command diagnostics in the returned error
		// when netsh exits non-zero. Localized "no matching rule" messages are
		// therefore commonly present in err rather than out.
		if firewallOutputHasNoRules(out) || firewallOutputHasNoRules(err.Error()) {
			return FirewallRuleState{}, ErrFirewallNotFound
		}
		return FirewallRuleState{}, err
	}
	rules := parseNetshRules(out)
	if len(rules) == 0 || firewallOutputHasNoRules(out) {
		return FirewallRuleState{}, ErrFirewallNotFound
	}
	managedSpec := DefaultFirewallSpec()
	for _, rule := range rules {
		if rule.managed(managedSpec) {
			return rule, nil
		}
	}
	// Prefer reporting any non-legacy collision. Reconcile may replace legacy
	// rules by name, so it must never do that when a third-party rule is mixed
	// into the same display-name set.
	for _, rule := range rules {
		if !rule.legacyDevLANRule(managedSpec) {
			return rule, nil
		}
	}
	// Return a real matching-name rule so Reconcile can report a conflict and
	// never mistake a third-party rule for an absent managed rule.
	return rules[0], nil
}

func firewallOutputHasNoRules(output string) bool {
	lower := strings.ToLower(output)
	for _, marker := range []string{
		"no rules",
		"no rules match",
		"nenhuma regra",
		"não há regras",
		"nao ha regras",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func parseNetshRule(output string) FirewallRuleState {
	rules := parseNetshRules(output)
	if len(rules) == 0 {
		return FirewallRuleState{}
	}
	return rules[0]
}

func parseNetshRules(output string) []FirewallRuleState {
	rules := make([]FirewallRuleState, 0, 1)
	var rule FirewallRuleState
	for _, raw := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		separator := strings.IndexByte(line, ':')
		if separator < 1 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:separator]))
		value := strings.TrimSpace(line[separator+1:])
		if key == "rule name" || key == "nome da regra" {
			if rule.Name != "" {
				rules = append(rules, rule)
				rule = FirewallRuleState{}
			}
			rule.Name = value
			continue
		}
		switch key {
		case "group", "grouping", "grupo":
			rule.Group = value
		case "description", "descrição", "descricao":
			rule.Description = value
		case "enabled", "habilitado":
			rule.Enabled = value
		case "direction", "direção", "direcao":
			rule.Direction = value
		case "action", "ação", "acao":
			rule.Action = value
		case "protocol", "protocolo":
			rule.Protocol = value
		case "localport", "local port", "porta local":
			rule.LocalPorts = value
		case "profiles", "profile", "perfis", "perfil":
			rule.Profile = value
		case "remoteip", "remote ip", "ip remoto":
			rule.RemoteIP = value
		}
	}
	if rule.Name != "" {
		rules = append(rules, rule)
	}
	return rules
}

func (s SystemFirewall) Reconcile(ctx context.Context, spec FirewallSpec) error {
	if runtime.GOOS != "windows" && s.Runner == nil {
		return fmt.Errorf("firewall DevLAN só é suportado no Windows")
	}
	spec = withFirewallDefaults(spec)
	if err := spec.validate(); err != nil {
		return err
	}
	spec = normalizeFirewallSpec(spec)
	current, err := s.Inspect(ctx)
	if err == nil {
		if !current.managed(spec) {
			if !current.legacyDevLANRule(spec) {
				return fmt.Errorf("%w: nome=%q grupo=%q descrição=%q", ErrFirewallConflict, current.Name, current.Group, current.Description)
			}
			if _, err = s.runner().Run(ctx, "advfirewall", "firewall", "delete", "rule", "name="+spec.RuleName); err != nil {
				return fmt.Errorf("remover regra legada do firewall: %w", err)
			}
			_, err = s.runner().Run(ctx, firewallAddArguments(spec)...)
			return err
		}
		if current.Matches(spec) {
			return nil
		}
		_, err = s.runner().Run(ctx, firewallSetArguments(spec)...)
		return err
	}
	if !errors.Is(err, ErrFirewallNotFound) {
		return err
	}
	_, err = s.runner().Run(ctx, firewallAddArguments(spec)...)
	return err
}

func firewallSetArguments(spec FirewallSpec) []string {
	return append([]string{"advfirewall", "firewall", "set", "rule", "name=" + spec.RuleName, "new"}, firewallProperties(spec)...)
}

func firewallAddArguments(spec FirewallSpec) []string {
	return append([]string{"advfirewall", "firewall", "add", "rule", "name=" + spec.RuleName}, firewallProperties(spec)...)
}

func firewallProperties(spec FirewallSpec) []string {
	return []string{
		"dir=" + strings.ToLower(spec.Direction),
		"action=" + strings.ToLower(spec.Action),
		"enable=yes",
		"protocol=" + strings.ToUpper(spec.Protocol),
		"localport=" + spec.localPortExpression(),
		"profile=" + strings.ToLower(spec.Profile),
		"remoteip=" + strings.ToLower(spec.RemoteIP),
		"description=" + spec.Description,
	}
}

func (s SystemFirewall) Remove(ctx context.Context) error {
	if runtime.GOOS != "windows" && s.Runner == nil {
		return nil
	}
	rule, err := s.Inspect(ctx)
	if errors.Is(err, ErrFirewallNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !rule.managed(DefaultFirewallSpec()) {
		return fmt.Errorf("%w: regra %q não é gerenciada pelo DevLAN", ErrFirewallConflict, rule.Name)
	}
	_, err = s.runner().Run(ctx, "advfirewall", "firewall", "delete", "rule", "name="+FirewallRuleName,
		"dir=in", "protocol=TCP", "localport="+rule.LocalPorts, "profile=private", "remoteip=localsubnet")
	return err
}

// EnsureFirewall remains a source-compatible helper for integrations that
// provide an explicit list. Application code uses FirewallSpecForConfig.
func EnsureFirewall(ctx context.Context, ports ...int) error {
	return (SystemFirewall{}).Ensure(ctx, ports...)
}

func RemoveFirewall(ctx context.Context) error { return (SystemFirewall{}).Remove(ctx) }
