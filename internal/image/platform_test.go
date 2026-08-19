package image

import "testing"

func TestPlatformString(t *testing.T) {
	cases := []struct {
		p    Platform
		want string
	}{
		{Platform{OS: "linux", Architecture: "amd64"}, "linux/amd64"},
		{Platform{OS: "linux", Architecture: "arm", Variant: "v7"}, "linux/arm/v7"},
	}
	for _, tc := range cases {
		if got := tc.p.String(); got != tc.want {
			t.Errorf("Platform.String() = %q, want %q", got, tc.want)
		}
	}
}

func TestPlatformEqual(t *testing.T) {
	a := Platform{OS: "linux", Architecture: "amd64"}
	if !a.Equal(a) {
		t.Errorf("platform should equal itself")
	}
	if a.Equal(Platform{OS: "linux", Architecture: "arm64"}) {
		t.Errorf("different arch must not be equal")
	}
	if a.Equal(Platform{OS: "linux", Architecture: "amd64", Variant: "v8"}) {
		t.Errorf("different variant must not be equal")
	}
}

func TestPlatformIsZero(t *testing.T) {
	if !(Platform{}).IsZero() {
		t.Errorf("empty platform should be zero")
	}
	if (Platform{OS: "linux"}).IsZero() {
		t.Errorf("a platform with a set OS must not be zero")
	}
	if (Platform{Architecture: "amd64"}).IsZero() {
		t.Errorf("a platform with a set architecture must not be zero")
	}
}
