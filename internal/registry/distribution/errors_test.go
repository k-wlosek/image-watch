package distribution

import (
	"errors"
	"testing"
)

func TestError_MessageAndUnwrap(t *testing.T) {
	inner := errors.New("boom")
	e := newError(ErrClassRateLimit, "acme/foo", "rate limited", 429, inner)

	if e.Error() == "" || e.Unwrap() != inner {
		t.Errorf("Error() = %q, Unwrap() = %v", e.Error(), e.Unwrap())
	}
	if got := e.Error(); got != "registry: rate_limit: rate limited (repository=acme/foo, status=429)" {
		t.Errorf("unexpected error message: %s", got)
	}

	noStatus := newError(ErrClassNetwork, "acme/foo", "no route", 0, nil)
	want := "registry: network_failure: no route (repository=acme/foo)"
	if noStatus.Error() != want {
		t.Errorf("message without status = %q, want %q", noStatus.Error(), want)
	}
}

func TestError_IsTransient(t *testing.T) {
	transient := []ErrorClass{ErrClassNetwork, ErrClassAuthentication, ErrClassAuthorization, ErrClassRateLimit, ErrClassRegistry}
	for _, class := range transient {
		if !(&Error{Class: class}).IsTransient() {
			t.Errorf("%s should be transient", class)
		}
	}
	for _, class := range []ErrorClass{ErrClassRepoNotFound, ErrClassManifestNotFound, ErrClassUnsupportedMediaType, ErrClassInvalidReference} {
		if (&Error{Class: class}).IsTransient() {
			t.Errorf("%s should not be transient", class)
		}
	}
}

func TestClassifyStatus(t *testing.T) {
	cases := []struct {
		status int
		want   ErrorClass
	}{
		{401, ErrClassAuthentication},
		{403, ErrClassAuthorization},
		{404, ErrClassRepoNotFound},
		{429, ErrClassRateLimit},
		{406, ErrClassUnsupportedMediaType},
		{415, ErrClassUnsupportedMediaType},
		{500, ErrClassRegistry},
		{200, ErrClassRegistry},
	}
	for _, tc := range cases {
		if got := classifyStatus(tc.status); got != tc.want {
			t.Errorf("classifyStatus(%d) = %q, want %q", tc.status, got, tc.want)
		}
	}
}
