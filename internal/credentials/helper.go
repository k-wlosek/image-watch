package credentials

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
)

var helperNameRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

// runHelper invokes docker-credential-<name> get, per the standard
// credential helper protocol.
func runHelper(ctx context.Context, name, host string) (string, string, bool) {
	if !helperNameRe.MatchString(name) {
		return "", "", false
	}
	cmd := exec.CommandContext(ctx, "docker-credential-"+name, "get")
	cmd.Stdin = strings.NewReader(host)
	out, err := cmd.Output()
	if err != nil {
		return "", "", false
	}

	var resp struct {
		Username string
		Secret   string
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", "", false
	}
	if resp.Username == "" && resp.Secret == "" {
		return "", "", false
	}
	return resp.Username, resp.Secret, true
}

func decodeBasicAuth(encoded string) (string, string, bool) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", "", false
	}
	user, pass, ok := strings.Cut(string(raw), ":")
	if !ok {
		return "", "", false
	}
	return user, pass, true
}
