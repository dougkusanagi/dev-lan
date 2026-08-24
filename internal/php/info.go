package php

import (
	"html/template"
	"sort"
	"strings"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

type InfoProject struct {
	Name       string
	Path       string
	Preset     string
	Version    string
	Pool       string
	Socket     string
	Extensions []string
}

type InfoPage struct {
	GeneratedBy string
	Projects    []InfoProject
}

// RenderInfoPage intentionally exposes an allowlisted set of fields only. It
// never dumps $_SERVER, environment variables, request headers or phpinfo(),
// which would otherwise leak credentials and local paths to the LAN.
func RenderInfoPage(cfg domain.Config) (string, error) {
	projects := make([]InfoProject, 0, len(cfg.Projects))
	for _, project := range cfg.Projects {
		version := cfg.EffectivePHPVersion(project)
		configured, _ := cfg.PHPVersion(version)
		extensions := append([]string(nil), configured.Extensions...)
		sort.Strings(extensions)
		projects = append(projects, InfoProject{
			Name:       project.Name,
			Path:       project.Path,
			Preset:     string(cfg.PHPProjectPreset(project)),
			Version:    version,
			Pool:       PoolSummary(cfg, project),
			Socket:     cfg.PHPSocket(project),
			Extensions: extensions,
		})
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })
	const page = `<!doctype html>
<html lang="pt-BR">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>DevLAN PHP</title>
<style>body{font:15px system-ui,sans-serif;max-width:1100px;margin:2rem auto;padding:0 1rem;color:#202124}table{border-collapse:collapse;width:100%}th,td{border:1px solid #ddd;padding:.5rem;text-align:left}th{background:#f3f4f6}code{font-family:ui-monospace,monospace}</style>
<h1>DevLAN — PHP</h1>
<p>Informações sanitizadas. Variáveis de ambiente, segredos, headers e conteúdo da aplicação não são exibidos.</p>
{{if .Projects}}<table><thead><tr><th>Projeto</th><th>Preset</th><th>PHP</th><th>Pool</th><th>Socket</th><th>Extensões</th></tr></thead><tbody>{{range .Projects}}<tr><td>{{.Name}}</td><td>{{.Preset}}</td><td>{{.Version}}</td><td>{{.Pool}}</td><td><code>{{.Socket}}</code></td><td>{{join .Extensions ", "}}</td></tr>{{end}}</tbody></table>{{else}}<p>Nenhum projeto PHP registrado.</p>{{end}}
<p>Gerado por {{.GeneratedBy}}.</p>
</html>`
	tpl, err := template.New("devlan-php-info").Funcs(template.FuncMap{"join": strings.Join}).Parse(page)
	if err != nil {
		return "", err
	}
	var output strings.Builder
	if err := tpl.Execute(&output, InfoPage{GeneratedBy: "DevLAN", Projects: projects}); err != nil {
		return "", err
	}
	return output.String(), nil
}
