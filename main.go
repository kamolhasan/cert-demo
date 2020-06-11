package main

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	certlib "github.com/kamolhasan/cert-demo/cert"
)

func main() {
	caKey, caCert, _, err := certlib.CreateCaCertificate("tmp")
	if err != nil {
		panic(err)
	}
	err = certlib.CreateNodeCertificatePEM("tmp", caKey, caCert)
	if err != nil {
		panic(err)
	}
	node, err := ioutil.ReadFile(filepath.Join("tmp", certlib.NodeCert))
	if err != nil {
		panic(err)
	}
	subj, err := certlib.ExtractSubjectFromCertificate(node)
	if err != nil {
		panic(err)
	}

	fmt.Println(subj.String())

}
