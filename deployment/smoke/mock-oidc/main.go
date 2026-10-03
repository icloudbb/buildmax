// Command mock-oidc serves the kind deployment an OpenID Connect provider, so
// the deployed server's SSO path can be signed in through end to end without an
// IdP tenant.
//
// It is a packaging of internal/testsupport/mockoidc. It serves TLS itself
// because the server accepts only an https issuer, and it reaches the browser
// through the ingress and the server pods through its Service under one issuer
// URL; see docs/deploy/local-kind.md.
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/icloudbb/buildmax/internal/testsupport/mockoidc"
)

func main() {
	provider, err := mockoidc.New(mockoidc.Config{
		Issuer:       os.Getenv("MOCK_OIDC_ISSUER"),
		ClientID:     os.Getenv("MOCK_OIDC_CLIENT_ID"),
		ClientSecret: os.Getenv("MOCK_OIDC_CLIENT_SECRET"),
	})
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		Addr:              ":9443",
		Handler:           provider.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Fatal(server.ListenAndServeTLS(os.Getenv("MOCK_OIDC_TLS_CERT"), os.Getenv("MOCK_OIDC_TLS_KEY")))
}
