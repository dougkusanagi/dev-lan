package platform

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

type CARootDetails struct {
	Exists        bool   `json:"exists"`
	Valid         bool   `json:"valid"`
	Trusted       bool   `json:"trusted"`
	RenewalDue    bool   `json:"renewalDue"`
	RemainingDays int    `json:"remainingDays,omitempty"`
	Path          string `json:"path,omitempty"`
	Subject       string `json:"subject,omitempty"`
	Fingerprint   string `json:"fingerprint,omitempty"`
	NotBefore     string `json:"notBefore,omitempty"`
	NotAfter      string `json:"notAfter,omitempty"`
	Detail        string `json:"detail,omitempty"`
}

const CARootRenewalWindow = 30 * 24 * time.Hour

func ValidateCARootPEM(data []byte) error {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return errors.New("CA raiz do Caddy não é um certificado PEM")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("ler CA raiz do Caddy: %w", err)
	}
	if !certificate.IsCA {
		return errors.New("certificado exportado não é uma autoridade certificadora")
	}
	now := time.Now()
	if now.Before(certificate.NotBefore) || now.After(certificate.NotAfter) {
		return errors.New("certificado da CA raiz está fora do período de validade")
	}
	return nil
}

func ReadCARootDetails(path string) CARootDetails {
	details := CARootDetails{Path: path}
	if strings.TrimSpace(path) == "" {
		details.Detail = "caminho da CA não configurado"
		return details
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			details.Detail = "certificado raiz ausente"
		} else {
			details.Detail = err.Error()
		}
		return details
	}
	details.Exists = true
	block, _ := pem.Decode(data)
	if block == nil {
		details.Detail = "PEM ausente ou inválido"
		return details
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		details.Detail = err.Error()
		return details
	}
	now := time.Now()
	details.Valid = certificate.IsCA && !now.Before(certificate.NotBefore) && !now.After(certificate.NotAfter)
	details.Subject = certificate.Subject.String()
	fingerprint := sha256.Sum256(certificate.Raw)
	details.Fingerprint = strings.ToUpper(hex.EncodeToString(fingerprint[:]))
	details.NotBefore = certificate.NotBefore.UTC().Format("2006-01-02T15:04:05Z")
	details.NotAfter = certificate.NotAfter.UTC().Format("2006-01-02T15:04:05Z")
	if certificate.NotAfter.After(now) {
		details.RemainingDays = int(certificate.NotAfter.Sub(now) / (24 * time.Hour))
	}
	details.RenewalDue = details.Valid && certificate.NotAfter.Sub(now) <= CARootRenewalWindow
	if !certificate.IsCA {
		details.Detail = "certificado não é CA"
	} else if now.Before(certificate.NotBefore) || now.After(certificate.NotAfter) {
		details.Detail = "certificado fora do período de validade"
	} else if details.RenewalDue {
		details.Detail = fmt.Sprintf("certificado expira em %d dias; o Caddy deve renová-lo automaticamente", details.RemainingDays)
	}
	return details
}

// InstallCARoot installs only the public certificate in the current user's
// Windows Root store. The optional runner exists for deterministic tests; a
// machine-wide fallback is intentionally not automatic because it would make
// a local DevLAN install unexpectedly modify every user profile.
func InstallCARoot(ctx context.Context, certificatePath string, runners ...Runner) error {
	if runtime.GOOS != "windows" && len(runners) == 0 {
		return fmt.Errorf("trust store do Windows só é suportado no Windows")
	}
	if err := ValidateCARootFile(certificatePath); err != nil {
		return err
	}
	var runner Runner
	if len(runners) > 0 && runners[0] != nil {
		runner = runners[0]
	} else {
		runner = NewExecRunner("certutil.exe")
	}
	if _, err := runner.Run(ctx, "-user", "-addstore", "Root", certificatePath); err != nil {
		return fmt.Errorf("instalar CA raiz no trust store do usuário: %w", err)
	}
	return nil
}

func ValidateCARootFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("ler certificado raiz %s: %w", path, err)
	}
	return ValidateCARootPEM(data)
}

// CARootThumbprint returns the SHA-1 certificate thumbprint used by
// certutil's -delstore selector. It is an identity only, not a security hash
// for validating certificate contents.
func CARootThumbprint(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return "", errors.New("certificado raiz PEM ausente")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", err
	}
	fingerprint := sha1.Sum(certificate.Raw)
	return strings.ToUpper(hex.EncodeToString(fingerprint[:])), nil
}
