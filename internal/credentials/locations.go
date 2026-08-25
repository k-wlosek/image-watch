package credentials

import (
	"os"
	"path/filepath"
)

// DockerConfigPaths returns candidate Docker config.json locations.
func DockerConfigPaths() []string {
	if v := os.Getenv("DOCKER_CONFIG"); v != "" {
		return []string{filepath.Join(v, "config.json")}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".docker", "config.json")}
}

// PodmanConfigPaths returns candidate Podman auth.json locations.
func PodmanConfigPaths() []string {
	if v := os.Getenv("REGISTRY_AUTH_FILE"); v != "" {
		return []string{v}
	}
	var paths []string
	if v := os.Getenv("XDG_RUNTIME_DIR"); v != "" {
		paths = append(paths, filepath.Join(v, "containers", "auth.json"))
	}
	return append(paths, "/run/containers/0/auth.json")
}
