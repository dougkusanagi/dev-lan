# Contratos das portas de aplicação

As interfaces consumidas por `internal/application` têm suítes reutilizáveis em
`internal/portcontract`. Cada suíte recebe uma fábrica e pode ser aplicada a
um fake ou à implementação concreta correspondente.

| Porta | Suíte | Implementações cobertas no caminho padrão |
| --- | --- | --- |
| Store | `RunStoreContract` | fake em `application/ports`, `config.Store` |
| Runner | `RunRunnerContract` | fake, `ExecRunner`, `WSLRunner` com invoker injetado |
| Firewall | `RunFirewallContract` | fake, `ApplicationFirewall` com backend nativo de teste |
| Caddy/lifecycle | `RunCaddyContract`, `RunCaddyLifecycleContract` | fake, `CaddyClient` |
| Certificados/trust store | `RunCaddyCertificatesContract`, `RunTrustStoreContract` | fake, `CaddyClient` e `WindowsTrustStore` com runner injetado |
| Network/Clock | `RunNetworkContract`, `RunClockContract` | fake, `HostNetwork` e `SystemClock` |
| Reconciler | `RunReconcilerContract` | fake, `reconcile.Runner` |

`go test ./...` executa somente cenários herméticos. Os adapters de plataforma
aceitam seams explícitos para esses testes, mas seus defaults continuam usando
`netsh`, `certutil`, `netstat`, `wsl.exe` e as sondagens nativas.

Integrações reais continuam opt-in:

- `DEVLAN_RUN_CADDY_TESTS=1 go test ./internal/caddy -run TestRealCaddyPair`
- `DEVLAN_REAL_CADDY=1 go test ./internal/caddy -run TestRenderWSLUnifiedWithRealCaddy`
- `DEVLAN_M8_REAL=1 go test ./internal/platform -run TestM8RealWindowsWSL`
- `scripts/test-m8-real.ps1` para o smoke operacional Windows+WSL.
