package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"runtime"
	"path/filepath"
	"time"
)

// TLS 证书: 首次启动生成自签证书并持久化 (Android: /data/local/devctl/, Windows: exe 同目录)

func certPath() string {
	if runtime.GOOS == "android" {
		return "/data/local/devctl/tls"
	}
	// windows/linux: exe 同目录
	exe, err := os.Executable()
	if err == nil {
		return filepath.Join(filepath.Dir(exe), "devctl-tls")
	}
	return "devctl-tls"
}

func loadOrCreateCert() (tls.Certificate, error) {
	dir := certPath()
	os.MkdirAll(dir, 0755)
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")

	if _, err := os.Stat(certFile); err == nil {
		if cert, err := tls.LoadX509KeyPair(certFile, keyFile); err == nil {
			return cert, nil
		}
	}
	// 生成自签证书 (10 年)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().Unix()),
		Subject:      pkix.Name{CommonName: "devctl-agent"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, _ := x509.MarshalECPrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	os.WriteFile(certFile, certPEM, 0600)
	os.WriteFile(keyFile, keyPEM, 0600)
	fmt.Fprintf(os.Stderr, "已生成 TLS 证书: %s\n", certFile)
	return tls.X509KeyPair(certPEM, keyPEM)
}
