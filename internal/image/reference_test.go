package image

import "testing"

func TestTagOrEmpty(t *testing.T) {
	tag := "1.2.3"
	if got := (Reference{Tag: &tag}).TagOrEmpty(); got != "1.2.3" {
		t.Errorf("TagOrEmpty with tag = %q", got)
	}
	if got := (Reference{}).TagOrEmpty(); got != "" {
		t.Errorf("TagOrEmpty without tag = %q, want empty", got)
	}
}

func TestReferenceString(t *testing.T) {
	tag := "1.2.3"
	digest := "sha256:abc"
	cases := []struct {
		ref  Reference
		want string
	}{
		{Reference{Registry: "ghcr.io", Repository: "acme/foo", Tag: &tag}, "ghcr.io/acme/foo:1.2.3"},
		{Reference{Registry: "ghcr.io", Repository: "acme/foo", Digest: &digest}, "ghcr.io/acme/foo@sha256:abc"},
		{Reference{Registry: "ghcr.io", Repository: "acme/foo"}, "ghcr.io/acme/foo"},
	}
	for _, tc := range cases {
		if got := tc.ref.String(); got != tc.want {
			t.Errorf("Reference.String() = %q, want %q", got, tc.want)
		}
	}
}

func TestParseReference_EmptyErrors(t *testing.T) {
	if _, err := ParseReference(""); err == nil {
		t.Error("expected an error for an empty raw reference")
	}
}

func TestIsDigestPinned(t *testing.T) {
	tag := "1.2.3"
	digest := "sha256:abc"
	if !(Reference{Digest: &digest}).IsDigestPinned() {
		t.Error("digest-only reference must be pinned")
	}
	if (Reference{Tag: &tag}).IsDigestPinned() {
		t.Error("tagged reference must not be pinned")
	}
}

func TestParseReference(t *testing.T) {
	cases := []struct {
		raw        string
		registry   string
		repository string
		tag        string
		digest     string
	}{
		{"nginx", "docker.io", "library/nginx", "latest", ""},
		{"nginx:1.25", "docker.io", "library/nginx", "1.25", ""},
		{"foo/bar", "docker.io", "foo/bar", "latest", ""},
		{"ghcr.io/acme/foo:1.2.3", "ghcr.io", "acme/foo", "1.2.3", ""},
		{"localhost:5000/foo:v1", "localhost:5000", "foo", "v1", ""},
		{"registry.example.com/foo", "registry.example.com", "foo", "latest", ""},
		{"ghcr.io/acme/foo@sha256:abc", "ghcr.io", "acme/foo", "", "sha256:abc"},
		{"foo/bar@sha256:abc", "docker.io", "foo/bar", "", "sha256:abc"},
	}
	for _, tc := range cases {
		ref, err := ParseReference(tc.raw)
		if err != nil {
			t.Errorf("ParseReference(%q) error: %v", tc.raw, err)
			continue
		}
		if ref.Registry != tc.registry || ref.Repository != tc.repository {
			t.Errorf("ParseReference(%q) = %s/%s, want %s/%s", tc.raw, ref.Registry, ref.Repository, tc.registry, tc.repository)
			continue
		}
		if got := ref.TagOrEmpty(); got != tc.tag {
			t.Errorf("ParseReference(%q) tag = %q, want %q", tc.raw, got, tc.tag)
		}
		if ref.Digest != nil && *ref.Digest != tc.digest {
			t.Errorf("ParseReference(%q) digest = %q, want %q", tc.raw, *ref.Digest, tc.digest)
		}
		if tc.digest == "" && ref.Digest != nil {
			t.Errorf("ParseReference(%q) unexpectedly parsed a digest", tc.raw)
		}
		if tc.digest != "" && ref.Tag != nil {
			t.Errorf("ParseReference(%q) unexpectedly parsed a tag alongside a digest", tc.raw)
		}
	}
}

func TestParseReference_NoRepositoryErrors(t *testing.T) {
	if _, err := ParseReference("ghcr.io/"); err == nil {
		t.Error("expected an error when no repository can be determined")
	}
}
