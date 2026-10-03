package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

// kindOIDCProbe signs in through SSO on the deployed stack the way a browser
// does — through the ingress, the mock provider's sign-in form, the callback on
// whichever server replica answers, and the Portal cookie exchange — while the
// smoke above has already signed in with a login code on the same deployment.
// Together they are the deployed evidence that both ways in work side by side.
//
// Against a real IdP it can only check the half that needs no person: that
// sign-in is offered and starts at that IdP. The rest is signed in by hand.
func kindOIDCProbe() error {
	fmt.Println("Probing SSO next to native login...")
	ctx := context.Background()
	portal := kindPortalURL()
	browser, err := newOIDCProbeBrowser()
	if err != nil {
		return err
	}

	var methods struct {
		LocalLogin string `json:"local_login"`
		OIDC       struct {
			Enabled bool `json:"enabled"`
		} `json:"oidc"`
	}
	if err := requestJSON(ctx, browser, http.MethodGet, portal+"/api/auth/methods", "", nil, &methods, http.StatusOK); err != nil {
		return err
	}
	if methods.LocalLogin != "all" || !methods.OIDC.Enabled {
		return fmt.Errorf("sign-in methods are local_login=%q, oidc=%t; want both native login and SSO offered", methods.LocalLogin, methods.OIDC.Enabled)
	}

	authorize, err := oidcProbeStart(browser, portal)
	if err != nil {
		return err
	}
	if kindExternalOIDC() {
		fmt.Printf("SSO starts at %s://%s; sign in by hand at %s to finish the check.\n", authorize.Scheme, authorize.Host, portal)
		return nil
	}

	email := "sso-probe-" + mustRandomHex(4) + "@" + kindOIDCDefaultDomain
	landed, err := oidcProbeSignIn(browser, authorize, email)
	if err != nil {
		return err
	}
	if landed != portal+"/" {
		return fmt.Errorf("SSO as %s landed on %s, want the Portal root", email, landed)
	}
	token, err := oidcProbePortalSession(ctx, browser, portal)
	if err != nil {
		return fmt.Errorf("exchange the SSO session cookie: %w", err)
	}
	// A provisioned account owns a personal Space; listing it proves the account
	// and its session are real rather than a cookie that only looks right.
	var spaces []json.RawMessage
	if err := requestJSON(ctx, browser, http.MethodGet, portal+"/api/spaces", token, nil, &spaces, http.StatusOK); err != nil {
		return fmt.Errorf("list spaces as the SSO account: %w", err)
	}
	if len(spaces) == 0 {
		return fmt.Errorf("the SSO-provisioned account %s has no personal space", email)
	}

	// Outside allowed_email_domains, the IdP vouching for someone is not enough.
	authorize, err = oidcProbeStart(browser, portal)
	if err != nil {
		return err
	}
	outsider := "sso-probe-" + mustRandomHex(4) + "@outside.example"
	landed, err = oidcProbeSignIn(browser, authorize, outsider)
	if err != nil {
		return err
	}
	if want := portal + "/login?sso_error=not_authorized"; landed != want {
		return fmt.Errorf("SSO from a disallowed domain landed on %s, want %s", landed, want)
	}
	fmt.Printf("SSO ok: %s signed in and was provisioned; a disallowed domain was refused.\n", email)
	return nil
}

// newOIDCProbeBrowser is a cookie-keeping client that stops at each redirect,
// resolves the mock's in-cluster name to kind's TLS port on this machine, and
// trusts the CA the server trusts.
func newOIDCProbeBrowser() (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	caPEM, err := captureKindKubectl("get", "configmap", "buildmax-trust", "-n", "buildmax", "-o", "jsonpath={.data.smoke-oidc-ca\\.crt}")
	if err != nil {
		return nil, fmt.Errorf("read the mock OIDC provider's CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(caPEM)) {
		return nil, errors.New("buildmax-trust holds no usable mock OIDC CA")
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if host, port, err := net.SplitHostPort(addr); err == nil && host == kindOIDCServiceDNS {
				addr = net.JoinHostPort("127.0.0.1", port)
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}
	return &http.Client{
		Jar:           jar,
		Transport:     transport,
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, nil
}

// oidcProbeStart begins a sign-in and returns the IdP authorize URL it sent the
// browser to.
func oidcProbeStart(browser *http.Client, portal string) (*url.URL, error) {
	location, err := oidcProbeRedirect(browser.Get(portal + "/api/auth/oidc/start"))
	if err != nil {
		return nil, fmt.Errorf("start SSO: %w", err)
	}
	authorize, err := url.Parse(location)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(authorize.Path, "/authorize") {
		return nil, fmt.Errorf("SSO start redirected to %s, not an authorize endpoint", location)
	}
	if !kindExternalOIDC() && authorize.Scheme+"://"+authorize.Host != kindOIDCIssuer() {
		return nil, fmt.Errorf("SSO start redirected to %s, want the mock issuer %s", location, kindOIDCIssuer())
	}
	return authorize, nil
}

// oidcProbeSignIn fills the mock's sign-in form and follows it back through the
// callback, returning where the callback sent the browser.
func oidcProbeSignIn(browser *http.Client, authorize *url.URL, email string) (string, error) {
	form, err := browser.Get(authorize.String())
	if err != nil {
		return "", fmt.Errorf("open the IdP sign-in form: %w", err)
	}
	_, _ = io.Copy(io.Discard, form.Body)
	_ = form.Body.Close()
	if form.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the IdP sign-in form returned %s", form.Status)
	}
	values := authorize.Query()
	values.Set("email", email)
	values.Set("email_verified", "true")
	values.Set("decision", "allow")
	endpoint := *authorize
	endpoint.RawQuery = ""
	callback, err := oidcProbeRedirect(browser.PostForm(endpoint.String(), values))
	if err != nil {
		return "", fmt.Errorf("submit the IdP sign-in form: %w", err)
	}
	landed, err := oidcProbeRedirect(browser.Get(callback))
	if err != nil {
		return "", fmt.Errorf("follow the SSO callback: %w", err)
	}
	return landed, nil
}

func oidcProbePortalSession(ctx context.Context, browser *http.Client, portal string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, portal+"/api/auth/portal/session", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Origin", portal)
	resp, err := browser.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("POST /api/auth/portal/session returned %s: %s", resp.Status, body)
	}
	var session struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil || session.AccessToken == "" {
		return "", fmt.Errorf("no access token from the Portal session exchange")
	}
	return session.AccessToken, nil
}

// oidcProbeRedirect requires a 302 and returns its Location.
func oidcProbeRedirect(resp *http.Response, err error) (string, error) {
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusFound {
		return "", fmt.Errorf("%s %s returned %s, want a redirect", resp.Request.Method, resp.Request.URL.Redacted(), resp.Status)
	}
	return resp.Header.Get("Location"), nil
}

func mustRandomHex(bytes int) string {
	s, err := randomHex(bytes)
	if err != nil {
		panic(err)
	}
	return s
}
