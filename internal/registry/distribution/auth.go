package distribution

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// CredentialProvider resolves username/password credentials for a registry host.
type CredentialProvider func(registryHost string) (username, password string, ok bool)

// NoCredentials returns no credentials.
func NoCredentials(string) (string, string, bool) { return "", "", false }

// challenge is a parsed WWW-Authenticate: Bearer challenge.
type challenge struct {
	Realm   string
	Service string
	Scope   string
}

// parseBearerChallenge parses a WWW-Authenticate Bearer header.
func parseBearerChallenge(header string) (challenge, bool) {
	if !strings.HasPrefix(header, "Bearer ") {
		return challenge{}, false
	}
	rest := strings.TrimPrefix(header, "Bearer ")

	c := challenge{}
	for _, part := range splitChallengeParams(rest) {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.Trim(strings.TrimSpace(kv[1]), `"`)
		switch key {
		case "realm":
			c.Realm = val
		case "service":
			c.Service = val
		case "scope":
			c.Scope = val
		}
	}
	if c.Realm == "" {
		return challenge{}, false
	}
	return c, true
}

// splitChallengeParams splits comma-separated key="value" pairs.
func splitChallengeParams(s string) []string {
	var parts []string
	var cur strings.Builder
	inQuotes := false
	for _, r := range s {
		switch r {
		case '"':
			inQuotes = !inQuotes
			cur.WriteRune(r)
		case ',':
			if inQuotes {
				cur.WriteRune(r)
			} else {
				parts = append(parts, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}

type tokenResponse struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
}

func (t tokenResponse) resolvedToken() string {
	if t.Token != "" {
		return t.Token
	}
	return t.AccessToken
}

// authenticator fetches and caches bearer tokens.
type authenticator struct {
	httpClient  *http.Client
	credentials CredentialProvider

	mu     sync.Mutex
	tokens map[string]string // cache key -> bearer token
}

func newAuthenticator(httpClient *http.Client, creds CredentialProvider) *authenticator {
	if creds == nil {
		creds = NoCredentials
	}
	return &authenticator{
		httpClient:  httpClient,
		credentials: creds,
		tokens:      make(map[string]string),
	}
}

func cacheKey(c challenge) string {
	return c.Realm + "|" + c.Service + "|" + c.Scope
}

// tokenFor performs or reuses the token exchange described by a challenge.
func (a *authenticator) tokenFor(ctx context.Context, registryHost string, c challenge) (string, error) {
	a.mu.Lock()
	if tok, ok := a.tokens[cacheKey(c)]; ok {
		a.mu.Unlock()
		return tok, nil
	}
	a.mu.Unlock()

	u, err := url.Parse(c.Realm)
	if err != nil {
		return "", fmt.Errorf("distribution: invalid auth realm %q: %w", c.Realm, err)
	}
	q := u.Query()
	if c.Service != "" {
		q.Set("service", c.Service)
	}
	if c.Scope != "" {
		q.Set("scope", c.Scope)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	if username, password, ok := a.credentials(registryHost); ok {
		req.SetBasicAuth(username, password)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", newError(ErrClassNetwork, "", "token request failed", 0, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", newError(classifyStatus(resp.StatusCode), "", "authentication rejected during token exchange", resp.StatusCode, nil)
	}
	if resp.StatusCode != http.StatusOK {
		return "", newError(classifyStatus(resp.StatusCode), "", "unexpected status during token exchange", resp.StatusCode, nil)
	}

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", newError(ErrClassRegistry, "", "malformed token response", resp.StatusCode, err)
	}
	token := tr.resolvedToken()
	if token == "" {
		return "", newError(ErrClassAuthentication, "", "token response contained no token", resp.StatusCode, nil)
	}

	a.mu.Lock()
	a.tokens[cacheKey(c)] = token
	a.mu.Unlock()

	return token, nil
}

// invalidate drops any cached token for a challenge.
func (a *authenticator) invalidate(c challenge) {
	a.mu.Lock()
	delete(a.tokens, cacheKey(c))
	a.mu.Unlock()
}
