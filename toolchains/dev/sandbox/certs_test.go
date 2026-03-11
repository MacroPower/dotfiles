package sandbox_test

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/toolchains/dev/sandbox"
)

func TestGenerateCA(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	certPath, keyPath, err := sandbox.GenerateCA(dir)
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dir, "ca.pem"), certPath)
	assert.Equal(t, filepath.Join(dir, "ca-key.pem"), keyPath)

	certPEM, err := os.ReadFile(certPath)
	require.NoError(t, err)

	block, _ := pem.Decode(certPEM)
	require.NotNil(t, block)
	assert.Equal(t, "CERTIFICATE", block.Type)

	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	assert.True(t, cert.IsCA)
	assert.Equal(t, "Sandbox CA", cert.Subject.CommonName)
	assert.Equal(t, 0, cert.MaxPathLen)
	assert.True(t, cert.MaxPathLenZero)

	keyPEM, err := os.ReadFile(keyPath)
	require.NoError(t, err)

	keyBlock, _ := pem.Decode(keyPEM)
	require.NotNil(t, keyBlock)
	assert.Equal(t, "EC PRIVATE KEY", keyBlock.Type)
}

func TestGenerateLeafCert(t *testing.T) {
	t.Parallel()

	caDir := t.TempDir()
	_, _, err := sandbox.GenerateCA(caDir)
	require.NoError(t, err)

	certsDir := t.TempDir()

	tests := map[string]struct {
		domain  string
		wantSAN []string
	}{
		"simple domain": {
			domain:  "api.example.com",
			wantSAN: []string{"api.example.com"},
		},
		"wildcard domain": {
			domain:  "*.example.com",
			wantSAN: []string{"*.example.com"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			localCertsDir := t.TempDir()
			err := sandbox.GenerateLeafCert(caDir, localCertsDir, tt.domain)
			require.NoError(t, err)

			certPEM, err := os.ReadFile(filepath.Join(localCertsDir, tt.domain, "cert.pem"))
			require.NoError(t, err)

			block, _ := pem.Decode(certPEM)
			require.NotNil(t, block)

			cert, err := x509.ParseCertificate(block.Bytes)
			require.NoError(t, err)
			assert.Equal(t, tt.domain, cert.Subject.CommonName)
			assert.Equal(t, tt.wantSAN, cert.DNSNames)
			assert.False(t, cert.IsCA)

			keyPEM, err := os.ReadFile(filepath.Join(localCertsDir, tt.domain, "key.pem"))
			require.NoError(t, err)

			keyBlock, _ := pem.Decode(keyPEM)
			require.NotNil(t, keyBlock)
			assert.Equal(t, "EC PRIVATE KEY", keyBlock.Type)

			// Verify cert is signed by CA.
			caCertPEM, err := os.ReadFile(filepath.Join(caDir, "ca.pem"))
			require.NoError(t, err)

			caBlock, _ := pem.Decode(caCertPEM)
			caCert, err := x509.ParseCertificate(caBlock.Bytes)
			require.NoError(t, err)

			pool := x509.NewCertPool()
			pool.AddCert(caCert)

			_, err = cert.Verify(x509.VerifyOptions{
				Roots:   pool,
				DNSName: tt.domain,
			})
			require.NoError(t, err)
		})
	}

	_ = certsDir
}

func TestGenerateCerts(t *testing.T) {
	t.Parallel()

	caDir := t.TempDir()
	certsDir := t.TempDir()

	rules := []sandbox.ResolvedRule{
		{Domain: "api.example.com", HTTPRules: []sandbox.ResolvedHTTPRule{{Path: "/v1/"}}},
		{Domain: "cdn.example.com"},
		{Domain: "internal.example.com", HTTPRules: []sandbox.ResolvedHTTPRule{{Path: "/api/"}, {Path: "/health"}}},
	}

	err := sandbox.GenerateCerts(rules, caDir, certsDir)
	require.NoError(t, err)

	// CA should exist.
	_, err = os.Stat(filepath.Join(caDir, "ca.pem"))
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(caDir, "ca-key.pem"))
	require.NoError(t, err)

	// Path-restricted domains should have certs.
	for _, domain := range []string{"api.example.com", "internal.example.com"} {
		_, err = os.Stat(filepath.Join(certsDir, domain, "cert.pem"))
		require.NoError(t, err, "cert should exist for %s", domain)

		_, err = os.Stat(filepath.Join(certsDir, domain, "key.pem"))
		require.NoError(t, err, "key should exist for %s", domain)
	}

	// Unrestricted domain should NOT have certs.
	_, err = os.Stat(filepath.Join(certsDir, "cdn.example.com", "cert.pem"))
	assert.True(t, os.IsNotExist(err), "cert should not exist for cdn.example.com")
}
