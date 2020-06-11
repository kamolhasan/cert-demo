/*
Copyright The KubeDB Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cert

import (
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"fmt"
	"math"
	"math/big"
	"net"
	"os/exec"
	"path/filepath"
	"time"

	api "kubedb.dev/apimachinery/apis/kubedb/v1alpha1"
	"kubedb.dev/elasticsearch/pkg/keytool"

	"github.com/appscode/go/crypto/rand"
	"github.com/appscode/go/ioutil"
	"github.com/pkg/errors"
	"gomodules.xyz/cert"
)

const (
	duration365d = time.Hour * 24 * 365
	CertsDir     = "tmp/certs"

	RootKey      = "root-key.pem"
	RootCert     = "root-ca.pem"
	RootKeyStore = "root.jks"
	RootAlias    = "root-ca"

	NodeKey      = "node-key.pem"
	NodeCert     = "node.pem"
	NodePKCS12   = "node.pkcs12"
	NodeKeyStore = "node.jks"
	NodeAlias    = "elasticsearch-node"

	AdminKey  = "admin-key.pem"
	AdminCert = "admin.pem"

	sgAdminKey      = "sgadmin-key.pem"
	sgAdminCert     = "sgadmin.pem"
	sgAdminPKCS12   = "sgadmin.pkcs12"
	sgAdminKeyStore = "sgadmin.jks"
	sgAdminAlias    = "elasticsearch-sgadmin"

	ClientKey      = "client-key.pem"
	ClientCert     = "client.pem"
	ClientPKCS12   = "client.pkcs12"
	ClientKeyStore = "client.jks"
	ClientAlias    = "elasticsearch-client"
)

func CreateCaCertificate(certPath string) (*rsa.PrivateKey, *x509.Certificate, string, error) {
	cfg := cert.Config{
		CommonName:   "KubeDB Com. Root CA",
		Organization: []string{"Elasticsearch Operator"},
	}

	caKey, err := cert.NewPrivateKey()
	if err != nil {
		return nil, nil, "", errors.New("failed to generate key for CA certificate")
	}

	caCert, err := cert.NewSelfSignedCACert(cfg, caKey)
	if err != nil {
		return nil, nil, "", errors.New("failed to generate CA certificate")
	}

	nodeKeyByte := cert.EncodePrivateKeyPEM(caKey)
	if !ioutil.WriteString(filepath.Join(certPath, RootKey), string(nodeKeyByte)) {
		return nil, nil, "", errors.New("failed to write key for CA certificate")
	}
	caCertByte := cert.EncodeCertPEM(caCert)
	if !ioutil.WriteString(filepath.Join(certPath, RootCert), string(caCertByte)) {
		return nil, nil, "", errors.New("failed to write CA certificate")
	}

	pass := rand.Characters(6)

	err = keytool.PEMToJKS(filepath.Join(certPath, RootCert), filepath.Join(certPath, RootKeyStore), pass, RootAlias)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to convert %s to %s. Reason: %v", RootCert, RootKeyStore, errors.Cause(err))
	}

	return caKey, caCert, pass, nil
}

func CreateNodeCertificateJKS(certPath string, elasticsearch *api.Elasticsearch, caKey *rsa.PrivateKey, caCert *x509.Certificate, pass string) error {
	cfg := cert.Config{
		CommonName:   elasticsearch.OffshootName(),
		Organization: []string{"Elasticsearch Operator"},
		Usages: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
	}

	nodePrivateKey, err := cert.NewPrivateKey()
	if err != nil {
		return errors.New("failed to generate key for node certificate")
	}
	nodeCertificate, err := NewSignedCert(cfg, nodePrivateKey, caCert, caKey)
	if err != nil {
		return errors.New("failed to sign node certificate")
	}

	nodeKeyByte := cert.EncodePrivateKeyPEM(nodePrivateKey)
	if !ioutil.WriteString(filepath.Join(certPath, NodeKey), string(nodeKeyByte)) {
		return errors.New("failed to write key for node certificate")
	}
	nodeCertByte := cert.EncodeCertPEM(nodeCertificate)
	if !ioutil.WriteString(filepath.Join(certPath, NodeCert), string(nodeCertByte)) {
		return errors.New("failed to write node certificate")
	}

	_, err = exec.Command(
		"openssl",
		"pkcs12",
		"-export",
		"-certfile", filepath.Join(certPath, RootCert),
		"-inkey", filepath.Join(certPath, NodeKey),
		"-in", filepath.Join(certPath, NodeCert),
		"-password", fmt.Sprintf("pass:%s", pass),
		"-out", filepath.Join(certPath, NodePKCS12),
	).Output()
	if err != nil {
		return errors.New(fmt.Sprintf("failed to generate %s. Reason: %s", NodePKCS12, err.Error()))
	}

	err = keytool.PKCS12ToJKS(filepath.Join(certPath, NodePKCS12), filepath.Join(certPath, NodeKeyStore), pass, NodeAlias)
	if err != nil {
		return fmt.Errorf("failed to convert %s to %s. Reason: %v", NodePKCS12, NodeKeyStore, errors.Cause(err))
	}

	return nil
}

func CreateNodeCertificatePEM(certPath string, caKey *rsa.PrivateKey, caCert *x509.Certificate) error {
	cfg := cert.Config{
		CommonName:   "AppsCode",
		Organization: []string{"Elasticsearch Operator"},
		Usages: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
	}

	nodePrivateKey, err := cert.NewPrivateKey()
	if err != nil {
		return errors.New("failed to generate key for node certificate")
	}
	nodeCertificate, err := NewSignedCert(cfg, nodePrivateKey, caCert, caKey)
	if err != nil {
		return errors.New("failed to sign node certificate")
	}

	nodeKeyByte, err := cert.EncodePKCS8PrivateKeyPEM(nodePrivateKey)
	if err != nil {
		return err
	}

	if !ioutil.WriteString(filepath.Join(certPath, NodeKey), string(nodeKeyByte)) {
		return errors.New("failed to write key for node certificate")
	}

	nodeCertByte := cert.EncodeCertPEM(nodeCertificate)
	if !ioutil.WriteString(filepath.Join(certPath, NodeCert), string(nodeCertByte)) {
		return errors.New("failed to write node certificate")
	}

	return nil
}

func CreateSGAdminCertificateJKS(certPath string, caKey *rsa.PrivateKey, caCert *x509.Certificate, pass string) error {
	cfg := cert.Config{
		CommonName:   "sgadmin",
		Organization: []string{"Elasticsearch Operator"},
		AltNames: cert.AltNames{
			DNSNames: []string{
				"localhost",
			},
		},
		Usages: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
	}

	sgAdminPrivateKey, err := cert.NewPrivateKey()
	if err != nil {
		return errors.New("failed to generate key for sgadmin certificate")
	}
	sgAdminCertificate, err := cert.NewSignedCert(cfg, sgAdminPrivateKey, caCert, caKey)
	if err != nil {
		return errors.New("failed to sign sgadmin certificate")
	}

	sgAdminKeyByte := cert.EncodePrivateKeyPEM(sgAdminPrivateKey)
	if !ioutil.WriteString(filepath.Join(certPath, sgAdminKey), string(sgAdminKeyByte)) {
		return errors.New("failed to write key for sgadmin certificate")
	}
	sgAdminCertByte := cert.EncodeCertPEM(sgAdminCertificate)
	if !ioutil.WriteString(filepath.Join(certPath, sgAdminCert), string(sgAdminCertByte)) {
		return errors.New("failed to write sgadmin certificate")
	}

	_, err = exec.Command(
		"openssl",
		"pkcs12",
		"-export",
		"-certfile", filepath.Join(certPath, RootCert),
		"-inkey", filepath.Join(certPath, sgAdminKey),
		"-in", filepath.Join(certPath, sgAdminCert),
		"-password", fmt.Sprintf("pass:%s", pass),
		"-out", filepath.Join(certPath, sgAdminPKCS12),
	).Output()
	if err != nil {
		return errors.New("failed to generate " + sgAdminKeyStore)
	}

	err = keytool.PKCS12ToJKS(filepath.Join(certPath, sgAdminPKCS12), filepath.Join(certPath, sgAdminKeyStore), pass, sgAdminAlias)
	if err != nil {
		return fmt.Errorf("failed to convert %s to %s. Reason: %v", sgAdminPKCS12, sgAdminKeyStore, errors.Cause(err))
	}

	return nil
}

func CreateAdminCertificatePEM(certPath string, elasticsearch *api.Elasticsearch, caKey *rsa.PrivateKey, caCert *x509.Certificate) error {
	cfg := cert.Config{
		CommonName:   elasticsearch.OffshootName() + "-admin",
		Organization: []string{"Elasticsearch Operator"},
		AltNames: cert.AltNames{
			DNSNames: []string{
				"localhost",
				fmt.Sprintf("%v.%v.svc", elasticsearch.OffshootName(), elasticsearch.Namespace),
			},
		},
		Usages: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
	}

	clientPrivateKey, err := cert.NewPrivateKey()
	if err != nil {
		return errors.New("failed to generate key for admin certificate")
	}

	clientCertificate, err := cert.NewSignedCert(cfg, clientPrivateKey, caCert, caKey)
	if err != nil {
		return errors.New("failed to sign admin certificate")
	}

	adminKeyByte := cert.EncodePrivateKeyPEM(clientPrivateKey)
	if !ioutil.WriteString(filepath.Join(certPath, AdminKey), string(adminKeyByte)) {
		return errors.New("failed to write key for admin certificate")
	}

	adminCertByte := cert.EncodeCertPEM(clientCertificate)
	if !ioutil.WriteString(filepath.Join(certPath, AdminCert), string(adminCertByte)) {
		return errors.New("failed to write admin certificate")
	}

	return nil
}

func ExtractSubjectFromCertificate(crt []byte) (*pkix.Name, error) {
	block, _ := pem.Decode(crt)
	if block == nil || block.Type != cert.CertificateBlockType {
		return nil, errors.New("failed to decode PEM file")
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse the certificate")
	}
	return &c.Subject, nil
}

func CreateClientCertificateJKS(certPath string, elasticsearch *api.Elasticsearch, caKey *rsa.PrivateKey, caCert *x509.Certificate, pass string) error {
	cfg := cert.Config{
		CommonName:   elasticsearch.OffshootName(),
		Organization: []string{"Elasticsearch Operator"},
		AltNames: cert.AltNames{
			DNSNames: []string{
				"localhost",
				fmt.Sprintf("%v.%v.svc", elasticsearch.OffshootName(), elasticsearch.Namespace),
			},
		},
		Usages: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
	}

	clientPrivateKey, err := cert.NewPrivateKey()
	if err != nil {
		return errors.New("failed to generate key for client certificate")
	}

	clientCertificate, err := cert.NewSignedCert(cfg, clientPrivateKey, caCert, caKey)
	if err != nil {
		return errors.New("failed to sign client certificate")
	}

	clientKeyByte := cert.EncodePrivateKeyPEM(clientPrivateKey)
	if !ioutil.WriteString(filepath.Join(certPath, ClientKey), string(clientKeyByte)) {
		return errors.New("failed to write key for client certificate")
	}

	clientCertByte := cert.EncodeCertPEM(clientCertificate)
	if !ioutil.WriteString(filepath.Join(certPath, ClientCert), string(clientCertByte)) {
		return errors.New("failed to write client certificate")
	}

	_, err = exec.Command(
		"openssl",
		"pkcs12",
		"-export",
		"-certfile", filepath.Join(certPath, RootCert),
		"-inkey", filepath.Join(certPath, ClientKey),
		"-in", filepath.Join(certPath, ClientCert),
		"-password", fmt.Sprintf("pass:%s", pass),
		"-out", filepath.Join(certPath, ClientPKCS12),
	).Output()
	if err != nil {
		return errors.New("failed to generate client.pkcs12")
	}

	err = keytool.PKCS12ToJKS(filepath.Join(certPath, ClientPKCS12), filepath.Join(certPath, ClientKeyStore), pass, ClientAlias)
	if err != nil {
		return fmt.Errorf("failed to convert %s to %s: Reason: %v", ClientPKCS12, ClientKeyStore, errors.Cause(err))
	}

	return nil
}

// NewSignedCert creates a signed certificate using the given CA certificate and key
func NewSignedCert(cfg cert.Config, key *rsa.PrivateKey, caCert *x509.Certificate, caKey *rsa.PrivateKey) (*x509.Certificate, error) {
	serial, err := cryptorand.Int(cryptorand.Reader, new(big.Int).SetInt64(math.MaxInt64))
	if err != nil {
		return nil, err
	}
	if len(cfg.CommonName) == 0 {
		return nil, errors.New("must specify a CommonName")
	}
	if len(cfg.Usages) == 0 {
		return nil, errors.New("must specify at least one ExtKeyUsage")
	}

	certTmpl := x509.Certificate{
		Subject: pkix.Name{
			CommonName:   cfg.CommonName,
			Organization: cfg.Organization,
		},
		DNSNames:     cfg.AltNames.DNSNames,
		IPAddresses:  cfg.AltNames.IPs,
		SerialNumber: serial,
		NotBefore:    caCert.NotBefore,
		NotAfter:     time.Now().Add(duration365d).UTC(),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  cfg.Usages,
		ExtraExtensions: []pkix.Extension{
			{
				Id: oidExtensionSubjectAltName,
			},
		},
	}
	certTmpl.ExtraExtensions[0].Value, err = marshalSANs(cfg.AltNames.DNSNames, nil, cfg.AltNames.IPs)
	if err != nil {
		return nil, err
	}

	certDERBytes, err := x509.CreateCertificate(cryptorand.Reader, &certTmpl, caCert, key.Public(), caKey)
	if err != nil {
		return nil, err
	}

	return x509.ParseCertificate(certDERBytes)
}

var (
	oidExtensionSubjectAltName = []int{2, 5, 29, 17}
)

// marshalSANs marshals a list of addresses into a the contents of an X.509
// SubjectAlternativeName extension.
func marshalSANs(dnsNames, emailAddresses []string, ipAddresses []net.IP) (derBytes []byte, err error) {
	var rawValues []asn1.RawValue
	for _, name := range dnsNames {
		rawValues = append(rawValues, asn1.RawValue{Tag: 2, Class: 2, Bytes: []byte(name)})
	}
	for _, email := range emailAddresses {
		rawValues = append(rawValues, asn1.RawValue{Tag: 1, Class: 2, Bytes: []byte(email)})
	}
	for _, rawIP := range ipAddresses {
		// If possible, we always want to encode IPv4 addresses in 4 bytes.
		ip := rawIP.To4()
		if ip == nil {
			ip = rawIP
		}
		rawValues = append(rawValues, asn1.RawValue{Tag: 7, Class: 2, Bytes: ip})
	}
	// https://github.com/floragunncom/search-guard-docs/blob/master/tls_certificates_production.md#using-an-oid-value-as-san-entry
	// https://github.com/floragunncom/search-guard-ssl/blob/a2d1e8e9b25a10ecaf1cb47e48cf04328af7d7fb/example-pki-scripts/gen_node_cert.sh#L55
	// Adds AltName: OID: 1.2.3.4.5.5
	// ref: https://stackoverflow.com/a/47917273/244009
	rawValues = append(rawValues, asn1.RawValue{FullBytes: []byte{0x88, 0x05, 0x2A, 0x03, 0x04, 0x05, 0x05}})
	return asn1.Marshal(rawValues)
}
