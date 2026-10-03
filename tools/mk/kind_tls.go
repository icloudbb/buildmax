package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

// workerAPIServiceDNS is the in-cluster name the worker verifies the worker
// listener's certificate against. It must match buildmax-worker-api in the
// buildmax namespace.
const workerAPIServiceDNS = "buildmax-worker-api.buildmax.svc.cluster.local"

// generateWorkerAPICert makes a self-signed certificate for the worker listener,
// valid for the internal Service DNS name. Because it is self-signed it is its
// own CA: the same PEM is the server certificate the listener presents and the
// root the worker trusts. That is enough for kind, where the point is to
// exercise a real TLS handshake and a real hostname/CA check, not to model a
// production PKI.
func generateWorkerAPICert() (certPEM, keyPEM []byte, err error) {
	return generateSelfSignedCert(workerAPIServiceDNS)
}

// generateSelfSignedCert makes a certificate for one DNS name that is also its
// own CA, the shape every in-cluster TLS endpoint kind stands up uses.
func generateSelfSignedCert(dnsName string) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("%s key: %w", dnsName, err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: dnsName},
		DNSNames:              []string{dnsName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("%s certificate: %w", dnsName, err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("%s key marshal: %w", dnsName, err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

// applyKindWorkerAPITLS provisions the worker listener's TLS material in the
// cluster: a TLS Secret the server pod mounts as its certificate, and a
// ConfigMap the worker pods mount as the CA to verify it. Both are recreated on
// every up so a rebuilt cluster gets a fresh, matching pair. This is what lets
// kind exercise the worker control channel over HTTPS instead of plaintext.
func applyKindWorkerAPITLS() error {
	certPEM, keyPEM, err := generateWorkerAPICert()
	if err != nil {
		return err
	}

	// A generic Secret with the tls type, built from literals: kubectl's
	// `secret tls` form needs two files, and this keeps the key off disk.
	tlsManifest, err := captureKindKubectl(
		"create", "secret", "generic", "buildmax-worker-api-tls", "-n", "buildmax",
		"--type=kubernetes.io/tls",
		"--from-literal=tls.crt="+string(certPEM),
		"--from-literal=tls.key="+string(keyPEM),
		"--dry-run=client", "-o", "yaml",
	)
	if err != nil {
		return fmt.Errorf("render worker-api TLS secret: %w", err)
	}
	if err := runStdin(tlsManifest, "kubectl", "--context", kindContext(), "apply", "-f", "-"); err != nil {
		return err
	}

	caManifest, err := captureKindKubectl(
		"create", "configmap", "buildmax-worker-api-ca", "-n", "buildmax",
		"--from-literal=worker-api-ca.crt="+string(certPEM),
		"--dry-run=client", "-o", "yaml",
	)
	if err != nil {
		return fmt.Errorf("render worker-api CA configmap: %w", err)
	}
	return runStdin(caManifest, "kubectl", "--context", kindContext(), "apply", "-f", "-")
}
