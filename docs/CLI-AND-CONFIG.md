# CLI e configuração

## Registro de projetos

### Link explícito

```powershell
devlan link financeiro /home/silver/dev/financeiro
devlan unlink financeiro
devlan links
```

`link` associa um nome estável a um diretório específico e tem prioridade sobre projetos descobertos por `park`.

### Diretórios estacionados

```powershell
devlan park /home/silver/dev
devlan unpark /home/silver/dev
devlan parked
```

Cada filho direto contendo um projeto reconhecido pode ser publicado com o nome do diretório. A descoberta não deve executar scripts.

## Modos de atendimento

Valores planejados:

- `auto`: detecta PHP, servidor JS ou saída estática;
- `php`: Caddy e PHP-FPM;
- `dev`: script de desenvolvimento do projeto JS;
- `static`: pasta já compilada, como `dist`.

O modo pode ser definido globalmente:

```powershell
devlan mode default auto
```

E sobrescrito por projeto:

```powershell
devlan mode painel dev
devlan mode site static
devlan mode financeiro php
```

Para voltar a herdar o padrão global:

```powershell
devlan mode painel inherit
```

Regra de resolução:

```text
modo explícito do projeto
  > configuração da entrada park
  > padrão global
```

No MVP, apenas `php` será implementado. O schema e os comandos já devem aceitar evolução sem quebrar os projetos registrados.

## Configuração por projeto

Exemplos futuros:

```powershell
devlan config financeiro php.version 8.3
devlan config financeiro route.mode path
devlan config financeiro route.path financeiro
devlan config painel js.idle-timeout 15m
devlan config painel static.dir dist
```

Um comando deve mostrar valor efetivo e origem:

```powershell
devlan config painel --resolved
```

```text
mode                 dev       project
js.idle-timeout      15m       global
route.mode           path      default
```

## Comandos do MVP

```text
devlan install                  bootstrap de Caddy, PHP-FPM e firewall
devlan uninstall                remove componentes gerenciados, preserva projetos
devlan link NAME PATH           registra um projeto Laravel
devlan unlink NAME              remove o registro e a rota
devlan park PATH                registra uma pasta de projetos
devlan unpark PATH              remove a pasta estacionada
devlan status                   mostra componentes, projetos e URLs
devlan open [NAME]              abre projeto ou dashboard textual
devlan reload                   valida e aplica configurações
devlan doctor [NAME]            diagnóstico completo ou por projeto
devlan logs [COMPONENT]         exibe logs relevantes
devlan mode default php         define padrão do MVP
devlan mode NAME php|inherit    sobrescreve ou restaura herança
```

## Comandos posteriores

```text
devlan php install 8.4
devlan php use NAME 8.4
devlan deps install NAME
devlan build NAME
devlan start|stop|restart NAME
devlan expose NAME --mode path|port|host
devlan mode NAME auto|php|dev|static
devlan ui
```

## Detecção JavaScript futura

Prioridade inicial de lockfiles:

```text
bun.lock / bun.lockb → Bun
pnpm-lock.yaml       → pnpm
yarn.lock            → Yarn
package-lock.json    → npm
```

Se mais de um lockfile existir, `doctor` deve marcar ambiguidade em vez de escolher silenciosamente. `packageManager` no `package.json`, quando válido, pode resolver a ambiguidade.

O comando padrão vem de `scripts.dev`, mas pode ser sobrescrito explicitamente. Apenas projetos confiáveis e registrados podem iniciar processos.
