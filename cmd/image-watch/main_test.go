package main

import "testing"

func TestHealthcheckAddr(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{":9090", "127.0.0.1:9090"},
		{"0.0.0.0:9090", "127.0.0.1:9090"},
		{"127.0.0.1:9090", "127.0.0.1:9090"},
		{"localhost:9090", "localhost:9090"},
		{"10.0.0.5:9090", "10.0.0.5:9090"},
		{"9090", "9090"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := healthcheckAddr(tc.in); got != tc.want {
			t.Errorf("healthcheckAddr(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
