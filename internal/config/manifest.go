package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// InstallManifestVersion is bumped when the ownership model changes. Older
// manifests are still readable when their fields are a subset of this schema;
// unknown future versions are rejected rather than interpreted unsafely.
const InstallManifestVersion = 1

type ResourceOwnership string

const (
	OwnershipPreexisting ResourceOwnership = "preexisting"
	OwnershipCreated     ResourceOwnership = "created"
	OwnershipModified    ResourceOwnership = "modified"
	OwnershipAdopted     ResourceOwnership = "adopted"
	OwnershipUnknown     ResourceOwnership = "unknown"
)

// ManifestResource records one resource outside the project directories that
// the bootstrap or controller may have created. Paths may be Windows paths or
// absolute paths in the selected WSL distribution. No secret or project
// content belongs here.
type ManifestResource struct {
	ID            string            `json:"id"`
	Scope         string            `json:"scope"` // windows, wsl or shared
	Kind          string            `json:"kind"`  // file, directory, package, service, firewall, trust, path
	Path          string            `json:"path,omitempty"`
	Target        string            `json:"target,omitempty"`
	Distribution  string            `json:"distribution,omitempty"`
	Package       string            `json:"package,omitempty"`
	Fingerprint   string            `json:"fingerprint,omitempty"`
	Ownership     ResourceOwnership `json:"ownership"`
	BeforeSHA256  string            `json:"before_sha256,omitempty"`
	ManagedSHA256 string            `json:"managed_sha256,omitempty"`
	BackupPath    string            `json:"backup_path,omitempty"`
	Remove        bool              `json:"remove,omitempty"`
	Restore       bool              `json:"restore,omitempty"`
}

type InstallManifest struct {
	Version      int                `json:"version"`
	CreatedAt    string             `json:"created_at"`
	UpdatedAt    string             `json:"updated_at"`
	DataDir      string             `json:"data_dir"`
	Distribution string             `json:"distribution,omitempty"`
	Resources    []ManifestResource `json:"resources"`
}

func (s Store) LoadManifest() (InstallManifest, bool, error) {
	data, err := os.ReadFile(s.Paths().InstallManifest)
	if errors.Is(err, os.ErrNotExist) {
		return InstallManifest{}, false, nil
	}
	if err != nil {
		return InstallManifest{}, false, fmt.Errorf("ler manifesto de instalação: %w", err)
	}
	var manifest InstallManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return InstallManifest{}, false, fmt.Errorf("decodificar manifesto de instalação: %w", err)
	}
	if manifest.Version == 0 {
		manifest.Version = InstallManifestVersion
	}
	if manifest.Version > InstallManifestVersion {
		return InstallManifest{}, false, fmt.Errorf("versão do manifesto de instalação não suportada: %d", manifest.Version)
	}
	return manifest, true, nil
}

func (s Store) SaveManifest(manifest InstallManifest) error {
	if manifest.Version == 0 {
		manifest.Version = InstallManifestVersion
	}
	if manifest.Version > InstallManifestVersion {
		return fmt.Errorf("versão do manifesto de instalação não suportada: %d", manifest.Version)
	}
	if manifest.DataDir == "" {
		manifest.DataDir = s.Dir
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	if manifest.CreatedAt == "" {
		manifest.CreatedAt = now
	}
	manifest.UpdatedAt = now
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("serializar manifesto de instalação: %w", err)
	}
	if err := s.Ensure(); err != nil {
		return err
	}
	return atomicWrite(s.Paths().InstallManifest, append(data, '\n'), 0o600)
}

// EnsureInstallManifest creates the ownership ledger only once. This is
// intentionally conservative: an existing resource is marked preexisting and
// is not eligible for deletion until a bootstrap step explicitly records that
// it was created or modified by DevLAN.
func (s Store) EnsureInstallManifest(resources []ManifestResource, distribution string) (InstallManifest, error) {
	var result InstallManifest
	err := s.WithLock(context.Background(), func() error {
		manifest, exists, err := s.LoadManifest()
		if err != nil {
			return err
		}
		if exists {
			changed := false
			byID := make(map[string]int, len(manifest.Resources))
			for index, resource := range manifest.Resources {
				byID[resource.ID] = index
			}
			for _, resource := range resources {
				if index, found := byID[resource.ID]; found {
					// A later bootstrap can prove ownership for a resource that was
					// unknown during an older/partial install, but never downgrades
					// an existing conservative classification.
					if manifest.Resources[index].Ownership == OwnershipUnknown && resource.Ownership != "" && resource.Ownership != OwnershipUnknown {
						manifest.Resources[index].Ownership = resource.Ownership
						changed = true
					}
					// A first host-side pass cannot inspect an absolute path inside
					// WSL, so older manifests may have conservatively called a
					// restore resource "created". A bootstrap marker proving that a
					// pre-existing file was modified is stronger evidence and may
					// upgrade that one-way classification.
					if manifest.Resources[index].Ownership == OwnershipCreated && resource.Ownership == OwnershipModified && resource.Restore {
						manifest.Resources[index].Ownership = OwnershipModified
						changed = true
					}
					if manifest.Resources[index].Ownership == OwnershipPreexisting && resource.Scope == "wsl" && (resource.Ownership == OwnershipCreated || resource.Ownership == OwnershipModified) {
						// The bootstrap's marker files are explicit evidence that a
						// resource previously classified as pre-existing was created
						// or modified during this installation.
						manifest.Resources[index].Ownership = resource.Ownership
						changed = true
					}
					continue
				}
				if resource.Ownership == "" {
					resource.Ownership = OwnershipUnknown
				}
				if resource.Distribution == "" {
					resource.Distribution = distribution
				}
				manifest.Resources = append(manifest.Resources, resource)
				changed = true
			}
			if changed {
				if err := s.SaveManifest(manifest); err != nil {
					return err
				}
			}
			result = manifest
			return nil
		}
		manifest = InstallManifest{Version: InstallManifestVersion, DataDir: s.Dir, Distribution: distribution, Resources: make([]ManifestResource, 0, len(resources))}
		for _, resource := range resources {
			if resource.Ownership == "" {
				resource.Ownership = OwnershipUnknown
			}
			if resource.Distribution == "" {
				resource.Distribution = distribution
			}
			// Paths in the WSL scope are not visible to host os.Stat/os.ReadFile;
			// their ownership is established by the bootstrap markers and their
			// fingerprints are recorded by the app through WSL commands.
			if resource.Path != "" && resource.Scope != "wsl" {
				if sum, ok := fileSHA256(resource.Path); ok {
					resource.BeforeSHA256 = sum
					if resource.Restore && resource.BackupPath == "" {
						backupDir := s.Paths().BackupsDir
						if err := os.MkdirAll(backupDir, 0o700); err != nil {
							return err
						}
						backupPath := filepath.Join(backupDir, safeManifestName(resource.ID)+".before")
						data, readErr := os.ReadFile(resource.Path)
						if readErr != nil {
							return fmt.Errorf("salvar backup de %s: %w", resource.Path, readErr)
						}
						if writeErr := os.WriteFile(backupPath, data, 0o600); writeErr != nil {
							return fmt.Errorf("gravar backup de %s: %w", resource.Path, writeErr)
						}
						resource.BackupPath = backupPath
					}
					if resource.Restore && (resource.Ownership == OwnershipUnknown || resource.Ownership == OwnershipCreated) {
						resource.Ownership = OwnershipModified
					} else if resource.Ownership == OwnershipUnknown || resource.Ownership == OwnershipCreated {
						resource.Ownership = OwnershipPreexisting
					}
				} else if resource.Ownership == OwnershipUnknown {
					resource.Ownership = OwnershipCreated
				}
			}
			manifest.Resources = append(manifest.Resources, resource)
		}
		if err := s.SaveManifest(manifest); err != nil {
			return err
		}
		result = manifest
		return nil
	})
	if err != nil {
		return InstallManifest{}, err
	}
	return result, nil
}

// RecordManagedState fills the post-apply fingerprint for resources whose
// contents are now managed. It never changes ownership of a preexisting or
// adopted resource implicitly.
func (s Store) RecordManagedState(ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.WithLock(context.Background(), func() error {
		manifest, exists, err := s.LoadManifest()
		if err != nil || !exists {
			return err
		}
		wanted := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			wanted[id] = struct{}{}
		}
		for index := range manifest.Resources {
			resource := &manifest.Resources[index]
			if _, ok := wanted[resource.ID]; !ok || resource.Path == "" {
				continue
			}
			if sum, ok := fileSHA256(resource.Path); ok {
				resource.ManagedSHA256 = sum
				if resource.Ownership == OwnershipUnknown {
					resource.Ownership = OwnershipCreated
				}
			}
		}
		return s.SaveManifest(manifest)
	})
}

// UpdateManifestResource applies a small, validated ownership update without
// replacing unrelated resources. It is used when an operation discovers a
// runtime identity (for example a certificate thumbprint) after installation.
func (s Store) UpdateManifestResource(id string, update func(*ManifestResource)) error {
	if update == nil || id == "" {
		return fmt.Errorf("atualização de manifesto inválida")
	}
	return s.WithLock(context.Background(), func() error {
		manifest, exists, err := s.LoadManifest()
		if err != nil {
			return err
		}
		if !exists {
			return os.ErrNotExist
		}
		for index := range manifest.Resources {
			if manifest.Resources[index].ID == id {
				update(&manifest.Resources[index])
				return s.SaveManifest(manifest)
			}
		}
		return fmt.Errorf("recurso não encontrado no manifesto: %s", id)
	})
}

func fileSHA256(path string) (string, bool) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), true
}

// FileSHA256 is the public fingerprint primitive used by uninstall's
// three-state comparison. Missing or unreadable paths return an empty value.
func FileSHA256(path string) (string, bool) { return fileSHA256(path) }

func safeManifestName(id string) string {
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "resource"
	}
	return b.String()
}
