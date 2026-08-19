package distribution

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNoCredentials(t *testing.T) {
	u, p, ok := NoCredentials("any-host")
	if u != "" || p != "" || ok {
		t.Errorf("NoCredentials() = %q, %q, %v; want empty,empty,false", u, p, ok)
	}
}

func TestParseBearerChallenge_Edges(t *testing.T) {
	if c, ok := parseBearerChallenge("Basic realm=\"x\""); ok {
		t.Errorf("non-Bearer challenge parsed as %+v", c)
	}

	if c, ok := parseBearerChallenge(`Bearer service="only-service"`); ok {
		t.Errorf("challenge without realm parsed as %+v", c)
	}

	if c, ok := parseBearerChallenge(`realm`); ok {
		t.Errorf("key without value parsed as %+v", c)
	}

	c, ok := parseBearerChallenge(`Bearer realm="https://auth.example.com/token",unknown="x",scope="repository:a/b:pull,repository:x/y:pull"`)
	if !ok {
		t.Fatal("expected a valid challenge to parse")
	}
	if c.Realm != "https://auth.example.com/token" {
		t.Errorf("Realm = %q", c.Realm)
	}
	if c.Scope != "repository:a/b:pull,repository:x/y:pull" {
		t.Errorf("Scope = %q, want comma inside quotes preserved", c.Scope)
	}
}

func TestSplitChallengeParams_InQuotesComma(t *testing.T) {
	parts := splitChallengeParams(`scope="a,b",realm="c"`)
	if len(parts) != 2 {
		t.Fatalf("splitChallengeParams() = %v, want 2 parts", parts)
	}
	if parts[0] != `scope="a,b"` || parts[1] != `realm="c"` {
		t.Errorf("unexpected parts: %v", parts)
	}
}

func TestTokenResponse_ResolvedToken(t *testing.T) {
	if got := (tokenResponse{Token: "tok"}).resolvedToken(); got != "tok" {
		t.Errorf("resolvedToken(Token) = %q", got)
	}
	if got := (tokenResponse{AccessToken: "at"}).resolvedToken(); got != "at" {
		t.Errorf("resolvedToken(AccessToken) = %q", got)
	}
}

func TestTokenFor_Flow(t *testing.T) {
	var requests int
	var sawBasicAuth bool
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _, ok := r.BasicAuth()
		sawBasicAuth = ok
		json.NewEncoder(w).Encode(tokenResponse{Token: "tok-" + string(rune('a'+requests-1))})
	}))
	defer tokenSrv.Close()

	creds := func(host string) (string, string, bool) { return "u", "p", true }
	a := newAuthenticator(http.DefaultClient, creds)
	c := challenge{Realm: tokenSrv.URL, Service: "svc", Scope: "repository:a/b:pull"}

	tok1, err := a.tokenFor(context.Background(), "host", c)
	if err != nil {
		t.Fatalf("tokenFor: %v", err)
	}
	if tok1 != "tok-a" || !sawBasicAuth {
		t.Errorf("token = %q, sawBasicAuth = %v", tok1, sawBasicAuth)
	}

	tok2, err := a.tokenFor(context.Background(), "host", c)
	if err != nil {
		t.Fatalf("tokenFor (cached): %v", err)
	}
	if requests != 1 {
		t.Errorf("cached tokenFor made %d requests, want 1", requests)
	}
	if tok2 != tok1 {
		t.Errorf("cached token %q != %q", tok2, tok1)
	}

	a.invalidate(c)
	if _, err := a.tokenFor(context.Background(), "host", c); err != nil {
		t.Fatalf("tokenFor after invalidate: %v", err)
	}
	if requests != 2 {
		t.Errorf("after invalidate tokenFor made %d requests, want 2", requests)
	}
}

func TestNewAuthenticator_NilCredentials(t *testing.T) {
	a := newAuthenticator(http.DefaultClient, nil)
	if a.credentials == nil {
		t.Fatal("expected nil credentials to be replaced by NoCredentials")
	}
}

func TestTokenFor_Errors(t *testing.T) {
	if _, err := (&authenticator{credentials: NoCredentials}).tokenFor(context.Background(), "h", challenge{Realm: "http://[::1"}); err == nil {
		t.Error("expected invalid realm URL to error")
	}

	cases := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"unauthorized", http.StatusUnauthorized, `{}`, "authentication rejected"},
		{"forbidden", http.StatusForbidden, `{}`, "authentication rejected"},
		{"unexpected status", http.StatusInternalServerError, `{}`, "unexpected status"},
		{"malformed json", http.StatusOK, `{bad`, "malformed token response"},
		{"empty token", http.StatusOK, `{}`, "no token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			a := newAuthenticator(http.DefaultClient, NoCredentials)
			_, err := a.tokenFor(context.Background(), "h", challenge{Realm: srv.URL})
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestTokenFor_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	a := newAuthenticator(http.DefaultClient, NoCredentials)
	_, err := a.tokenFor(context.Background(), "h", challenge{Realm: url})
	if err == nil {
		t.Fatal("expected a network error")
	}
	if derr, ok := err.(*Error); !ok || derr.Class != ErrClassNetwork {
		t.Fatalf("expected ErrClassNetwork, got %v", err)
	}
}
