package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	devlanupdate "github.com/dougkusanagi/dev-lan/internal/update"
)

func runUpdate(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: devlan update check CHANNEL [MANIFEST_URL] | devlan update download CHANNEL MANIFEST_URL PATH")
	}
	if len(args) < 2 {
		return fmt.Errorf("canal obrigatório: stable ou preview")
	}
	channel, err := devlanupdate.ParseChannel(args[1])
	if err != nil {
		return err
	}
	switch args[0] {
	case "check":
		if len(args) > 3 {
			return fmt.Errorf("uso: devlan update check CHANNEL [MANIFEST_URL]")
		}
		manifestURL := ""
		if len(args) == 3 {
			manifestURL = args[2]
		} else {
			manifestURL = os.Getenv("DEVLAN_UPDATE_MANIFEST_" + strings.ToUpper(string(channel)) + "_URL")
		}
		if strings.TrimSpace(manifestURL) == "" {
			return fmt.Errorf("informe MANIFEST_URL ou DEVLAN_UPDATE_MANIFEST_%s_URL", strings.ToUpper(string(channel)))
		}
		manifest, err := devlanupdate.FetchManifest(ctx, nil, manifestURL, channel)
		if err != nil {
			return err
		}
		data, _ := json.MarshalIndent(map[string]any{
			"channel":      channel,
			"current":      version,
			"available":    manifest.Version,
			"update":       devlanupdate.IsNewer(version, manifest.Version),
			"sha256":       manifest.SHA256,
			"artifact_url": manifest.URL,
		}, "", "  ")
		fmt.Println(string(data))
		return nil
	case "download":
		if len(args) != 4 {
			return fmt.Errorf("uso: devlan update download CHANNEL MANIFEST_URL PATH")
		}
		manifest, err := devlanupdate.FetchManifest(ctx, nil, args[2], channel)
		if err != nil {
			return err
		}
		if err := devlanupdate.DownloadVerified(ctx, nil, manifest, channel, args[3]); err != nil {
			return err
		}
		fmt.Printf("Update %s verificado por SHA-256 e preparado em %s.\n", manifest.Version, args[3])
		return nil
	default:
		return fmt.Errorf("subcomando update desconhecido: %s", args[0])
	}
}
