package registry

import (
	"testing"

	"github.com/k-wlosek/image-watch/internal/image"
)

func TestHasPlatform(t *testing.T) {
	linuxAMD64 := image.Platform{OS: "linux", Architecture: "amd64"}
	linuxARM64 := image.Platform{OS: "linux", Architecture: "arm64"}

	m := ManifestObservation{Platform: &linuxAMD64}
	if !m.HasPlatform(linuxAMD64) {
		t.Errorf("expected direct platform match")
	}
	if m.HasPlatform(linuxARM64) {
		t.Errorf("platform with a direct match must not accept others")
	}

	index := ManifestObservation{AvailablePlatforms: []image.Platform{linuxAMD64, linuxARM64}}
	if !index.HasPlatform(linuxARM64) || !index.HasPlatform(linuxAMD64) {
		t.Errorf("expected index platform match")
	}
	if index.HasPlatform(image.Platform{OS: "windows", Architecture: "amd64"}) {
		t.Errorf("unlisted platform must not match")
	}

	if (ManifestObservation{}).HasPlatform(linuxAMD64) {
		t.Errorf("empty observation must not match any platform")
	}
}
