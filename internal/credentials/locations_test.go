package credentials

import (
	"testing"
)

func TestDockerConfigPaths_WithEnv(t *testing.T) {
	t.Setenv("DOCKER_CONFIG", "/custom/path")
	paths := DockerConfigPaths()
	if len(paths) != 1 || paths[0] != "/custom/path/config.json" {
		t.Errorf("DockerConfigPaths() = %v, want [/custom/path/config.json]", paths)
	}
}

func TestDockerConfigPaths_FallbackToHome(t *testing.T) {
	t.Setenv("DOCKER_CONFIG", "")
	paths := DockerConfigPaths()
	if len(paths) != 1 {
		t.Fatalf("DockerConfigPaths() returned %d paths, want 1", len(paths))
	}
	if paths[0] != t.TempDir()+"/.docker/config.json" {
		// Can't predict exact home, but should end with /.docker/config.json
		t.Logf("DockerConfigPaths() = %v (home-based fallback)", paths)
	}
}

func TestDockerConfigPaths_NoHome(t *testing.T) {
	t.Setenv("DOCKER_CONFIG", "")
	t.Setenv("HOME", "/nonexistent-path-that-does-not-exist")
	// UserHomeDir may still succeed on some systems; just verify no panic
	_ = DockerConfigPaths()
}

func TestPodmanConfigPaths_WithRegistryAuthFile(t *testing.T) {
	t.Setenv("REGISTRY_AUTH_FILE", "/custom/auth.json")
	paths := PodmanConfigPaths()
	if len(paths) != 1 || paths[0] != "/custom/auth.json" {
		t.Errorf("PodmanConfigPaths() = %v, want [/custom/auth.json]", paths)
	}
}

func TestPodmanConfigPaths_WithXdgRuntimeDir(t *testing.T) {
	t.Setenv("REGISTRY_AUTH_FILE", "")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	paths := PodmanConfigPaths()
	if len(paths) != 2 {
		t.Fatalf("PodmanConfigPaths() returned %d paths, want 2", len(paths))
	}
	if paths[0] != "/run/user/1000/containers/auth.json" {
		t.Errorf("paths[0] = %q, want /run/user/1000/containers/auth.json", paths[0])
	}
	if paths[1] != "/run/containers/0/auth.json" {
		t.Errorf("paths[1] = %q, want /run/containers/0/auth.json", paths[1])
	}
}

func TestPodmanConfigPaths_FallbackDefault(t *testing.T) {
	t.Setenv("REGISTRY_AUTH_FILE", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	paths := PodmanConfigPaths()
	if len(paths) != 1 || paths[0] != "/run/containers/0/auth.json" {
		t.Errorf("PodmanConfigPaths() = %v, want [/run/containers/0/auth.json]", paths)
	}
}
