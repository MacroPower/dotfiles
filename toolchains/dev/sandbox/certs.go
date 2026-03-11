package sandbox

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// GenerateCA creates an ECDSA P-256 CA certificate and key, writing them
// to dir/ca.pem and dir/ca-key.pem. Returns the paths to the written files.
func GenerateCA(dir string) (string, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generating CA key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", fmt.Errorf("generating serial: %w", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "Sandbox CA",
			Organization: []string{"sandbox"},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", fmt.Errorf("creating CA certificate: %w", err)
	}

	err = os.MkdirAll(dir, 0o755)
	if err != nil {
		return "", "", fmt.Errorf("creating CA dir: %w", err)
	}

	certPath := filepath.Join(dir, "ca.pem")

	err = writePEM(certPath, "CERTIFICATE", certDER)
	if err != nil {
		return "", "", fmt.Errorf("writing CA cert: %w", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", fmt.Errorf("marshaling CA key: %w", err)
	}

	keyPath := filepath.Join(dir, "ca-key.pem")

	err = writePEM(keyPath, "EC PRIVATE KEY", keyDER)
	if err != nil {
		return "", "", fmt.Errorf("writing CA key: %w", err)
	}

	return certPath, keyPath, nil
}

// GenerateLeafCert creates a leaf certificate for domain signed by the CA
// in caDir, writing cert.pem and key.pem to certsDir/<domain>/.
func GenerateLeafCert(caDir, certsDir, domain string) error {
	caCertPEM, err := os.ReadFile(filepath.Join(caDir, "ca.pem"))
	if err != nil {
		return fmt.Errorf("reading CA cert: %w", err)
	}

	caKeyPEM, err := os.ReadFile(filepath.Join(caDir, "ca-key.pem"))
	if err != nil {
		return fmt.Errorf("reading CA key: %w", err)
	}

	caCertBlock, _ := pem.Decode(caCertPEM)
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		return fmt.Errorf("parsing CA cert: %w", err)
	}

	caKeyBlock, _ := pem.Decode(caKeyPEM)
	caKey, err := x509.ParseECPrivateKey(caKeyBlock.Bytes)
	if err != nil {
		return fmt.Errorf("parsing CA key: %w", err)
	}

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generating leaf key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("generating serial: %w", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: domain,
		},
		DNSNames:  []string{domain},
		NotBefore: time.Now().Add(-1 * time.Hour),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("creating leaf certificate: %w", err)
	}

	domainDir := filepath.Join(certsDir, domain)

	err = os.MkdirAll(domainDir, 0o755)
	if err != nil {
		return fmt.Errorf("creating cert dir: %w", err)
	}

	err = writePEM(filepath.Join(domainDir, "cert.pem"), "CERTIFICATE", certDER)
	if err != nil {
		return fmt.Errorf("writing leaf cert: %w", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		return fmt.Errorf("marshaling leaf key: %w", err)
	}

	err = writePEM(filepath.Join(domainDir, "key.pem"), "EC PRIVATE KEY", keyDER)
	if err != nil {
		return fmt.Errorf("writing leaf key: %w", err)
	}

	return nil
}

// GenerateCerts generates a CA and leaf certificates for all restricted
// rules (those with path or method constraints). Unrestricted domains
// are skipped.
func GenerateCerts(rules []ResolvedRule, caDir, certsDir string) error {
	_, _, err := GenerateCA(caDir)
	if err != nil {
		return fmt.Errorf("generating CA: %w", err)
	}

	for _, r := range rules {
		if !r.IsRestricted() {
			continue
		}

		err := GenerateLeafCert(caDir, certsDir, r.Domain)
		if err != nil {
			return fmt.Errorf("generating cert for %s: %w", r.Domain, err)
		}
	}

	return nil
}

// writePEM writes a PEM-encoded block to path with mode 0o644.
func writePEM(path, pemType string, data []byte) error {
	// 0o644: Envoy (uid 999) needs read access to cert and key files.
	// These are ephemeral, container-scoped MITM certs -- not long-lived secrets.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}

	err = pem.Encode(f, &pem.Block{Type: pemType, Bytes: data})
	if err != nil {
		cerr := f.Close()
		if cerr != nil {
			slog.Warn("closing file after encode error",
				slog.String("path", path),
				slog.Any("error", cerr),
			)
		}

		return fmt.Errorf("encoding PEM to %s: %w", path, err)
	}

	err = f.Close()
	if err != nil {
		return fmt.Errorf("closing %s: %w", path, err)
	}

	return nil
}
