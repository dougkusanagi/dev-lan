package platform

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

type m8Runner struct {
	outputs map[string]string
	errors  map[string]error
	calls   [][]string
}

func TestNormalizeCommandOutputDecodesBOMlessUTF16(t *testing.T) {
	want := "Version WSL: 2.7.3.0\n"
	units := utf16.Encode([]rune(want))
	data := make([]byte, len(units)*2)
	for index, unit := range units {
		binary.LittleEndian.PutUint16(data[index*2:], unit)
	}
	if got := normalizeCommandOutput(data); got != want {
		t.Fatalf("saída UTF-16 não foi decodificada: %q", got)
	}
	if got := normalizeCommandOutput([]byte("plain output\n")); got != "plain output\n" {
		t.Fatalf("saída comum foi alterada: %q", got)
	}
}

func TestWSLCompatibilityDoesNotDuplicateScopedWSLArguments(t *testing.T) {
	invoker := &m8Runner{outputs: map[string]string{
		"--version":        "WSL version: 2.7.3.0\n",
		"--list --verbose": "  NAME      STATE           VERSION\n* Ubuntu    Running         2\n",
		"--distribution Ubuntu --exec /bin/sh -c " + compatibilityProbeScript: "networkingMode=mirrored\nsystemd=true\nloopback=true\n",
	}}
	probe := WSLCompatibilityProbe{
		Windows: &m8Runner{outputs: map[string]string{
			"-NoProfile -NonInteractive -Command Get-ComputerInfo | Select-Object WindowsProductName,WindowsVersion,OsBuildNumber | Format-List | Out-String": "OsBuildNumber : 22631\n",
		}},
		WSL:        WSLRunner{Distribution: "Ubuntu", Invoker: invoker},
		ConfigText: "[wsl2]\nnetworkingMode=mirrored\n",
		LANProbe:   func(context.Context) error { return nil },
	}
	report := probe.Check(context.Background(), "Ubuntu", 0)
	if !report.WSL2 || !report.MirroredNetworking || !report.Systemd || !report.LoopbackBidirectional {
		t.Fatalf("probe WSL configurado não ficou saudável: %#v", report)
	}
	for _, call := range invoker.calls {
		joined := strings.Join(call, " ")
		if strings.Contains(joined, "--exec --distribution") || strings.Contains(joined, "--exec --exec") {
			t.Fatalf("argumentos WSL duplicados: %q", call)
		}
	}
}

func (r *m8Runner) Run(_ context.Context, args ...string) (string, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	joined := strings.Join(args, " ")
	if err := r.errors[joined]; err != nil {
		return r.outputs[joined], err
	}
	return r.outputs[joined], nil
}

func TestUpdateWSLConfigPreservesUnknownContentAndComments(t *testing.T) {
	input := "# keep this\n[wsl2]\nnetworkingMode = nat # user note\n# firewall = false\nfirewall=false\n\n[experimental]\nfoo=bar\n"
	updated, err := UpdateWSLConfigText(input, DefaultWSLConfigSettings())
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"# keep this",
		"networkingMode = mirrored # user note",
		"# firewall = false",
		"firewall=true",
		"dnsTunneling = true",
		"autoProxy = true",
		"[experimental]\nfoo=bar\nhostAddressLoopback = true",
	} {
		if !strings.Contains(updated, expected) {
			t.Fatalf("conteúdo atualizado não contém %q:\n%s", expected, updated)
		}
	}
	if !WSLConfigHasMirroredNetworking(updated) {
		t.Fatal("networkingMode espelhado não foi detectado")
	}

	crlf, err := UpdateWSLConfigText("[wsl2]\r\nfoo=bar\r\n", DefaultWSLConfigSettings())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ReplaceAll(crlf, "\r\n", ""), "\n") {
		t.Fatalf("a convenção CRLF não foi preservada: %q", crlf)
	}
}

func TestUpdateWSLConfigPreservesBOMEmptyFilesAndLastDuplicate(t *testing.T) {
	settings := DefaultWSLConfigSettings()
	updated, err := UpdateWSLConfigText("\ufeff[wsl2]\nnetworkingMode=nat\nnetworkingMode=mirrored # keep\n", settings)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(updated, "\ufeff[wsl2]") || !WSLConfigHasMirroredNetworking(updated) {
		t.Fatalf("BOM ou última atribuição não foi preservada: %q", updated)
	}
	if !strings.Contains(updated, "networkingMode=nat") || !strings.Contains(updated, "networkingMode=mirrored # keep") {
		t.Fatalf("conteúdo duplicado/comentário deveria ser preservado: %q", updated)
	}

	empty, err := UpdateWSLConfigText("", settings)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(empty, "\n") || !strings.HasPrefix(empty, "[wsl2]\n") {
		t.Fatalf("arquivo vazio recebeu layout inesperado: %q", empty)
	}
}

func TestUpdateWSLConfigCreatesBackupAndPublishesCompleteSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".wslconfig")
	original := []byte("[wsl2]\nnetworkingMode=nat\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := UpdateWSLConfig(path, DefaultWSLConfigSettings())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.BackupPath == "" {
		t.Fatalf("transação não reportou mudança/backup: %#v", result)
	}
	backup, err := os.ReadFile(result.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != string(original) {
		t.Fatalf("backup incorreto: %q", backup)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !WSLConfigHasMirroredNetworking(string(updated)) || !strings.Contains(string(updated), "autoProxy = true") {
		t.Fatalf(".wslconfig publicado incompleto:\n%s", updated)
	}
}

func TestWSLCompatibilityProbeChecksEffectiveCapabilitiesAndConflicts(t *testing.T) {
	host := &m8Runner{outputs: map[string]string{
		"-NoProfile -NonInteractive -Command Get-ComputerInfo | Select-Object WindowsProductName,WindowsVersion,OsBuildNumber | Format-List | Out-String": "WindowsProductName : Windows 11\nWindowsVersion : 23H2\nOsBuildNumber : 22631\n",
	}}
	wsl := &m8Runner{outputs: map[string]string{
		"--version": "WSL version: 2.3.26.0\nKernel version: 5.15\n",
		"--distribution Ubuntu --exec /bin/sh -c " + compatibilityProbeScript: "networkingMode=mirrored\nsystemd=true\nloopback=true\n",
	}}
	report := (WSLCompatibilityProbe{
		Windows:       host,
		WSL:           wsl,
		LANProbe:      func(context.Context) error { return nil },
		PortAvailable: func(_ context.Context, port int) bool { return port != 443 },
	}).Check(context.Background(), "Ubuntu", 80, 443, 8080, 8080)
	if report.WindowsBuild != 22631 || !report.WSL2 || !report.MirroredNetworking || !report.Systemd || !report.LoopbackBidirectional || !report.LANReachable {
		t.Fatalf("capacidades efetivas inesperadas: %#v", report)
	}
	if report.Supported || !reflect.DeepEqual(report.PortConflicts, []PortConflict{{Port: 443, Detail: "porta ocupada por outro listener"}}) {
		t.Fatalf("conflito de porta não foi reportado corretamente: %#v", report)
	}
}

func TestWSLCompatibilityCanProbeWSLToWindowsWithoutBackgroundAPI(t *testing.T) {
	host := &m8Runner{outputs: map[string]string{
		"-NoProfile -NonInteractive -Command Get-ComputerInfo | Select-Object WindowsProductName,WindowsVersion,OsBuildNumber | Format-List | Out-String": "WindowsProductName : Windows 11\nOsBuildNumber : 22631\n",
	}}
	wsl := &m8Runner{outputs: map[string]string{
		"--version": "WSL version: 2.3.26.0\n",
		"--distribution Ubuntu --exec /bin/sh -c " + compatibilityProbeScript: "networkingMode=mirrored\nsystemd=true\nloopback=false\n",
	}}
	report := (WSLCompatibilityProbe{
		Windows:           host,
		WSL:               wsl,
		WSLToWindowsProbe: func(context.Context) error { return nil },
		LoopbackProbe:     func(context.Context) error { return nil },
		LANProbe:          func(context.Context) error { return nil },
	}).Check(context.Background(), "Ubuntu")
	if !report.LoopbackBidirectional {
		t.Fatalf("listener temporário não substituiu dependência da API: %#v", report)
	}
}

func TestWSLCompatibilityUsesTheSelectedDistributionVersion(t *testing.T) {
	host := &m8Runner{outputs: map[string]string{
		"-NoProfile -NonInteractive -Command Get-ComputerInfo | Select-Object WindowsProductName,WindowsVersion,OsBuildNumber | Format-List | Out-String": "WindowsProductName : Windows 11\nOsBuildNumber : 22631\n",
	}}
	wsl := &m8Runner{outputs: map[string]string{
		"--version":        "WSL version: 2.3.26.0\n",
		"--list --verbose": "  NAME      STATE           VERSION\n* Ubuntu    Running         1\n  Debian    Stopped         2\n",
	}}
	report := (WSLCompatibilityProbe{
		Windows:       host,
		WSL:           wsl,
		ConfigText:    "[wsl2]\nnetworkingMode=mirrored\n",
		LANProbe:      func(context.Context) error { return nil },
		LoopbackProbe: func(context.Context) error { return nil },
	}).Check(context.Background(), "Ubuntu", 0)
	if report.WSL2 {
		t.Fatalf("a distribuição selecionada em WSL 1 não deveria ser marcada como WSL 2: %#v", report)
	}

	missing := (WSLCompatibilityProbe{
		Windows:    host,
		WSL:        wsl,
		ConfigText: "[wsl2]\nnetworkingMode=mirrored\n",
		LANProbe:   func(context.Context) error { return nil },
	}).Check(context.Background(), "Fedora", 0)
	if missing.WSL2 {
		t.Fatalf("distribuição ausente não deveria herdar a versão do aplicativo WSL: %#v", missing)
	}
}

func TestHyperVFirewallNeverBroadensDefaultInboundPolicy(t *testing.T) {
	spec := DefaultHyperVFirewallSpec()
	if spec.DefaultInboundAction != "Block" || spec.Profile != "Private" || spec.RemoteAddresses != "LocalSubnet" || !spec.LoopbackEnabled || spec.AllowHostPolicyMerge {
		t.Fatalf("política Hyper-V insegura: %#v", spec)
	}
	for _, command := range []string{hyperVCreateCommand(spec), hyperVSetCommand(spec), hyperVVMSettingCommand(spec)} {
		if strings.Contains(command, "DefaultInboundAction Allow") {
			t.Fatalf("comando abriu o default inbound: %s", command)
		}
	}
	if !strings.Contains(hyperVCreateCommand(spec), "-VMCreatorId '") || !strings.Contains(hyperVCreateCommand(spec), "-LocalPorts @(\"80\",\"443\",\"8080-8179\")") {
		t.Fatalf("regra Hyper-V não está limitada ao WSL/pool: %s", hyperVCreateCommand(spec))
	}
	if command := hyperVSetCommand(spec); !strings.Contains(command, "-NewDisplayName '") || strings.Contains(command, " -DisplayName '") {
		t.Fatalf("reconciliação Hyper-V usa conjunto de parâmetros ambíguo: %s", command)
	}
}

func TestHyperVFirewallRecognizesLocaleNeutralMissingRuleID(t *testing.T) {
	err := errors.New("FullyQualifiedErrorId : CmdletizationQuery_NotFound_InstanceID,Get-NetFirewallHyperVRule")
	if !hyperVObjectMissing("", err) {
		t.Fatal("erro CDXML de regra ausente não foi reconhecido")
	}
}

func TestHyperVFirewallReconcileCreatesRuleAndDefaultSetting(t *testing.T) {
	runner := &m8Runner{errors: map[string]error{}, outputs: map[string]string{}}
	for _, command := range []string{
		"-NoProfile -NonInteractive -Command try { Get-NetFirewallHyperVRule -Name 'DevLAN-HyperV' -ErrorAction Stop | Select-Object Name,DisplayName,Enabled,Direction,Action,Protocol,LocalPorts,Profiles,RemoteAddresses,VMCreatorId | ConvertTo-Json -Compress } catch { if ($_.FullyQualifiedErrorId -like 'CmdletizationQuery_NotFound_InstanceID*') { exit 0 }; throw }",
	} {
		runner.errors[command] = errors.New("not found")
	}
	if err := (HyperVFirewall{Runner: runner}).Reconcile(context.Background(), DefaultHyperVFirewallSpec()); err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, call := range runner.calls {
		joined += strings.Join(call, " ") + "\n"
	}
	for _, expected := range []string{"New-NetFirewallHyperVRule", "-Action Allow", "Set-NetFirewallHyperVVMSetting", "-DefaultInboundAction Block", "-LoopbackEnabled true", "-AllowHostPolicyMerge false"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("comando Hyper-V esperado %q ausente:\n%s", expected, joined)
		}
	}
}

func TestCompositeFirewallRemoveUsesUnambiguousHyperVParameters(t *testing.T) {
	windowsRunner := &m8Runner{outputs: map[string]string{}, errors: map[string]error{}}
	hyperVRunner := &m8Runner{outputs: map[string]string{}, errors: map[string]error{}}
	firewall := CompositeFirewall{
		Windows: SystemFirewall{Runner: windowsRunner},
		HyperV:  HyperVFirewall{Runner: hyperVRunner},
	}
	if err := firewall.Remove(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(hyperVRunner.calls) != 1 {
		t.Fatalf("esperava uma remoção Hyper-V, recebeu %d chamadas", len(hyperVRunner.calls))
	}
	command := strings.Join(hyperVRunner.calls[0], " ")
	if !strings.Contains(command, "Remove-NetFirewallHyperVRule -Name 'DevLAN-HyperV'") || strings.Contains(command, "-VMCreatorId") {
		t.Fatalf("remoção Hyper-V usa conjunto de parâmetros ambíguo: %s", command)
	}
}

func TestCaddyMigrationOrdersNewEdgeBeforeLegacyRemovalAndRollsBack(t *testing.T) {
	legacy := filepath.Join(t.TempDir(), "Caddyfile.windows")
	if err := os.WriteFile(legacy, []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	order := []string{}
	result, err := (CaddyMigration{
		LegacyFiles:     []string{legacy},
		BackupRoot:      filepath.Join(t.TempDir(), "backup"),
		ConfirmShutdown: true,
		Now:             func() time.Time { return time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC) },
		ValidateUnified: func(context.Context) error { order = append(order, "validate"); return nil },
		StartUnified:    func(context.Context) error { order = append(order, "start"); return nil },
		HealthUnified:   func(context.Context) error { order = append(order, "health"); return nil },
		StopLegacy:      func(context.Context) error { order = append(order, "stop"); return nil },
		ShutdownWSL:     func(context.Context) error { order = append(order, "shutdown"); return nil },
		VerifyAfterWSL:  func(context.Context) error { order = append(order, "verify"); return nil },
		RemoveLegacy:    func() error { order = append(order, "remove"); return nil },
	}).Migrate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"validate", "start", "health", "stop", "shutdown", "start", "health", "verify", "remove"}
	if !reflect.DeepEqual(order, expected) {
		t.Fatalf("ordem de migração incorreta: %#v", order)
	}
	backup, err := os.ReadFile(filepath.Join(result.BackupDir, filepath.Base(legacy)))
	if err != nil || string(backup) != "legacy" {
		t.Fatalf("backup legado incorreto: %v %q", err, backup)
	}

	rollbackCalled := false
	_, err = (CaddyMigration{
		LegacyFiles:     []string{legacy},
		BackupRoot:      filepath.Join(t.TempDir(), "rollback"),
		ConfirmShutdown: true,
		ValidateUnified: func(context.Context) error { return nil },
		StartUnified:    func(context.Context) error { return nil },
		HealthUnified:   func(context.Context) error { return nil },
		RemoveLegacy:    func() error { return errors.New("remoção recusada") },
		Rollback:        func(context.Context, string) error { rollbackCalled = true; return nil },
	}).Migrate(context.Background())
	if err == nil || !rollbackCalled {
		t.Fatal("falha após subir o novo edge não acionou rollback")
	}
}

func TestCaddyMigrationCanHandoffPortsBeforeStartingNewEdge(t *testing.T) {
	order := []string{}
	_, err := (CaddyMigration{
		ConfirmShutdown:       true,
		StopLegacyBeforeStart: true,
		ValidateUnified:       func(context.Context) error { order = append(order, "validate"); return nil },
		StartUnified:          func(context.Context) error { order = append(order, "start"); return nil },
		HealthUnified:         func(context.Context) error { order = append(order, "health"); return nil },
		StopLegacy:            func(context.Context) error { order = append(order, "stop"); return nil },
		RemoveLegacy:          func() error { order = append(order, "remove"); return nil },
	}).Migrate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"validate", "stop", "start", "health", "remove"}
	if !reflect.DeepEqual(order, expected) {
		t.Fatalf("handoff de portas em ordem incorreta: %#v", order)
	}
}

func TestCaddyMigrationRollbackSurvivesExpiredForwardContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rollbackCalled := false
	_, err := (CaddyMigration{
		ConfirmShutdown: true,
		ValidateUnified: func(context.Context) error { return nil },
		StartUnified:    func(context.Context) error { return nil },
		HealthUnified:   func(context.Context) error { return nil },
		RemoveLegacy: func() error {
			cancel()
			return errors.New("falha após expirar operação")
		},
		Rollback: func(rollbackCtx context.Context, _ string) error {
			rollbackCalled = true
			if rollbackCtx.Err() != nil {
				t.Fatalf("rollback herdou contexto expirado: %v", rollbackCtx.Err())
			}
			if _, ok := rollbackCtx.Deadline(); !ok {
				t.Fatal("rollback deveria permanecer limitado por timeout")
			}
			return nil
		},
	}).Migrate(ctx)
	if err == nil || !rollbackCalled {
		t.Fatalf("rollback não executado: %v", err)
	}
}

func TestDetectCaddyTopologyTreatsUnifiedAndLegacyWSLAsPartial(t *testing.T) {
	snapshot := DetectCaddyTopology(true, false, true, false, true)
	if snapshot.Topology != TopologyPartial {
		t.Fatalf("topologia com Caddy unificado e artefato WSL legado deveria ser parcial: %#v", snapshot)
	}
	if got := DetectCaddyTopology(true, false, false, false, true); got.Topology != TopologySingleWSL {
		t.Fatalf("topologia unificada deveria ser single-wsl: %#v", got)
	}
}
