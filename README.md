# DevLAN

DevLAN é uma ferramenta para publicar projetos que rodam no WSL para outros dispositivos da rede local, usando o Windows como ponto de entrada.

O primeiro caso de uso é simples:

```text
http://192.168.1.50/meu-projeto
```

O MVP será focado em projetos PHP, principalmente Laravel. Depois, a ferramenta evoluirá para múltiplas versões de PHP, sites estáticos, servidores JavaScript sob demanda, dashboard e menu na área de notificação do Windows.

## Princípios

- A CLI e a interface gráfica usam o mesmo núcleo em Go.
- O Windows controla a exposição na rede e o firewall.
- O WSL executa Caddy, PHP-FPM e os processos dos projetos.
- Configurações geradas são validadas antes de serem aplicadas.
- Cada projeto pode sobrescrever padrões globais.
- A primeira versão deve resolver bem Laravel/PHP antes de crescer.
- Projetos nunca são publicados sem terem sido registrados explicitamente por `link` ou `park`.

## Arquitetura resumida

```text
Dispositivo na LAN
       │
       ▼
Caddy no Windows :80
       │ localhost / integração WSL
       ▼
Caddy no WSL :8181
       │
       ▼
PHP-FPM ── projeto Laravel
```

No futuro, projetos JavaScript serão atendidos por um supervisor no WSL, que poderá iniciar o script `dev` no primeiro acesso e encerrar o processo depois de um período ocioso.

## Experiência esperada no MVP

```powershell
devlan install
devlan park /home/silver/dev
devlan link financeiro /home/silver/dev/financeiro
devlan status
devlan doctor
devlan open financeiro
```

Resultado:

```text
http://192.168.1.50/financeiro
```

## Documentos

- [Arquitetura](docs/ARCHITECTURE.md)
- [CLI e configuração](docs/CLI-AND-CONFIG.md)
- [Roadmap](docs/ROADMAP.md)
- [Decisões técnicas](docs/DECISIONS.md)

## Fora do escopo do MVP

- Interface gráfica ou menu na tray.
- Servidores JavaScript sob demanda.
- HTTPS e DNS interno.
- Múltiplas versões de PHP.
- Containers e bancos de dados.
- Instalação automática de dependências dos projetos.
