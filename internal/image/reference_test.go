package image

import "testing"

func TestParseReference(t *testing.T) {
	cases := []struct {
		raw            string
		wantRegistry   string
		wantRepository string
		wantTag        string
	}{
		{"nginx", "docker.io", "library/nginx", "latest"},
		{"nginx:1.25", "docker.io", "library/nginx", "1.25"},
		{"ghcr.io/acme/foo:1.2.3", "ghcr.io", "acme/foo", "1.2.3"},
		{"postgres:16", "docker.io", "library/postgres", "16"},
		{"localhost:5000/myimage:latest", "localhost:5000", "myimage", "latest"},
	}
	for _, c := range cases {
		ref, err := ParseReference(c.raw)
		if err != nil {
			t.Fatalf("ParseReference(%q) error: %v", c.raw, err)
		}
		if ref.Registry != c.wantRegistry {
			t.Errorf("ParseReference(%q).Registry = %q, want %q", c.raw, ref.Registry, c.wantRegistry)
		}
		if ref.Repository != c.wantRepository {
			t.Errorf("ParseReference(%q).Repository = %q, want %q", c.raw, ref.Repository, c.wantRepository)
		}
		if ref.TagOrEmpty() != c.wantTag {
			t.Errorf("ParseReference(%q).Tag = %q, want %q", c.raw, ref.TagOrEmpty(), c.wantTag)
		}
	}
}

func TestParseReference_DigestPinned(t *testing.T) {
	ref, err := ParseReference("foo@sha256:abcdef1234567890")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ref.IsDigestPinned() {
		t.Errorf("expected digest-pinned reference")
	}
}
