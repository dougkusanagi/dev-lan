//go:build windows

package service

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/api"
	"github.com/dougkusanagi/dev-lan/internal/app"
	"golang.org/x/sys/windows/svc"
)

type windowsManager struct{}

func newManager() Manager { return windowsManager{} }

func (windowsManager) Install(ctx context.Context, options Options) error {
	if err := validateOptions(options); err != nil {
		return err
	}
	// sc.exe receives every value as a separate argument. The executable and
	// data directory are quoted for the command line parsed by the SCM, not
	// concatenated into a shell command.
	binPath := fmt.Sprintf(`"%s" --data-dir "%s" service run`, options.Executable, options.DataDir)
	args := []string{"binPath=", binPath, "start=", "auto", "DisplayName=", "DevLAN background service"}
	if output, err := runSC(ctx, append([]string{"create", ServiceName}, args...)...); err != nil {
		// Re-running install updates an existing service instead of leaving a
		// stale executable/data directory behind.
		if _, configErr := runSC(ctx, append([]string{"config", ServiceName}, args...)...); configErr != nil {
			return fmt.Errorf("instalar serviço DevLAN: %w (atualizar existente: %v; saída: %s)", err, configErr, strings.TrimSpace(output))
		}
	}
	return nil
}

func (windowsManager) Remove(ctx context.Context) error {
	_, _ = runSC(ctx, "stop", ServiceName)
	output, err := runSC(ctx, "delete", ServiceName)
	if err != nil && !strings.Contains(strings.ToLower(output), "does not exist") && !strings.Contains(strings.ToLower(output), "não existe") {
		return fmt.Errorf("remover serviço DevLAN: %w", err)
	}
	return nil
}

func (windowsManager) Start(ctx context.Context) error {
	if _, err := runSC(ctx, "start", ServiceName); err != nil {
		return fmt.Errorf("iniciar serviço DevLAN: %w", err)
	}
	return nil
}

func (windowsManager) Stop(ctx context.Context) error {
	if _, err := runSC(ctx, "stop", ServiceName); err != nil {
		return fmt.Errorf("parar serviço DevLAN: %w", err)
	}
	return nil
}

func (windowsManager) Status(ctx context.Context) (Status, error) {
	output, err := runSC(ctx, "query", ServiceName)
	if err != nil {
		if strings.Contains(strings.ToLower(output), "does not exist") || strings.Contains(strings.ToLower(output), "não existe") {
			return Status{Detail: "serviço não instalado"}, nil
		}
		return Status{}, err
	}
	status := Status{Installed: true, Detail: strings.TrimSpace(output)}
	lower := strings.ToLower(output)
	status.Running = strings.Contains(lower, "running") || strings.Contains(lower, "em execução")
	if strings.Contains(lower, "auto") {
		status.StartType = "auto"
	}
	return status, nil
}

func validateOptions(options Options) error {
	if strings.TrimSpace(options.Executable) == "" || strings.ContainsAny(options.Executable, "\r\n\"") {
		return errors.New("executável do serviço inválido")
	}
	if strings.TrimSpace(options.DataDir) == "" || strings.ContainsAny(options.DataDir, "\r\n\"") {
		return errors.New("diretório do serviço inválido")
	}
	return nil
}

func runSC(ctx context.Context, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "sc.exe", args...)
	output, err := command.CombinedOutput()
	text := string(output)
	if err != nil {
		return text, fmt.Errorf("sc.exe %v: %w: %s", args, err, strings.TrimSpace(text))
	}
	return text, nil
}

type backgroundProgram struct {
	dataDir string
}

func (program *backgroundProgram) Execute(_ []string, requests <-chan svc.ChangeRequest, statuses chan<- svc.Status) (bool, uint32) {
	statuses <- svc.Status{State: svc.StartPending, WaitHint: 10_000}
	background := app.New(program.dataDir)
	preflightContext, preflightCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer preflightCancel()
	if err := background.CheckWSLAccess(preflightContext); err != nil {
		background.Audit("SERVICE_START_FAILED", err.Error())
		return false, 1
	}
	server := api.New(background)
	if _, err := server.Start(); err != nil {
		return false, 1
	}
	defer server.Close(context.Background())
	defer background.CloseDevProxies()
	startupContext, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	statuses <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	go func() {
		_, _ = background.Reload(startupContext)
	}()
	for request := range requests {
		switch request.Cmd {
		case svc.Interrogate:
			statuses <- request.CurrentStatus
		case svc.Stop, svc.Shutdown:
			statuses <- svc.Status{State: svc.StopPending, WaitHint: 10_000}
			statuses <- svc.Status{State: svc.Stopped}
			return false, 0
		}
	}
	return false, 0
}

func Run(ctx context.Context, dataDir string) error {
	if strings.TrimSpace(dataDir) == "" {
		return errors.New("diretório de dados do serviço não pode ser vazio")
	}
	// svc.Run blocks until SCM asks the process to stop. The context is kept in
	// the signature so callers can use one lifecycle contract on every OS.
	_ = ctx
	return svc.Run(ServiceName, &backgroundProgram{dataDir: filepath.Clean(dataDir)})
}
