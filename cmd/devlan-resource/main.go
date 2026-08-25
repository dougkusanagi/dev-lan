package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"

	"github.com/tc-hib/winres"
)

const iconSize = 256

func main() {
	output := flag.String("output", "", "caminho do arquivo .syso a ser gerado")
	flag.Parse()
	if *output == "" {
		fmt.Fprintln(os.Stderr, "uso: devlan-resource -output PATH")
		os.Exit(2)
	}

	if err := writeResource(*output); err != nil {
		fmt.Fprintf(os.Stderr, "gerar recurso Windows: %v\n", err)
		os.Exit(1)
	}
}

func writeResource(output string) error {
	icon, err := winres.NewIconFromResizedImage(appIcon(), []int{256, 128, 64, 48, 32, 16})
	if err != nil {
		return fmt.Errorf("criar ícone: %w", err)
	}

	resources := winres.ResourceSet{}
	if err := resources.SetIcon(winres.RT_ICON, icon); err != nil {
		return fmt.Errorf("adicionar ícone: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("criar diretório: %w", err)
	}
	file, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("abrir saída: %w", err)
	}
	defer file.Close()

	if err := resources.WriteObject(file, winres.ArchAMD64); err != nil {
		return fmt.Errorf("escrever recurso: %w", err)
	}
	return nil
}

func appIcon() image.Image {
	icon := image.NewRGBA(image.Rect(0, 0, iconSize, iconSize))
	draw.Draw(icon, icon.Bounds(), image.NewUniform(color.RGBA{R: 15, G: 23, B: 42, A: 255}), image.Point{}, draw.Src)

	white := image.NewUniform(color.RGBA{R: 248, G: 250, B: 252, A: 255})
	blue := image.NewUniform(color.RGBA{R: 56, G: 189, B: 248, A: 255})

	// A compact DL monogram remains legible at the 16px taskbar size.
	fill(icon, white, 48, 42, 76, 214)
	fill(icon, white, 48, 42, 137, 70)
	fill(icon, white, 48, 186, 137, 214)
	fill(icon, white, 109, 68, 137, 188)
	fill(icon, blue, 153, 42, 181, 214)
	fill(icon, blue, 153, 186, 222, 214)

	return icon
}

func fill(dst draw.Image, source image.Image, left, top, right, bottom int) {
	draw.Draw(dst, image.Rect(left, top, right, bottom), source, image.Point{}, draw.Src)
}
