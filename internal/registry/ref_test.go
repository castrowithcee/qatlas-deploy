package registry

import "testing"

func TestParseRepo(t *testing.T) {
	tests := []struct {
		ref      string
		host     string
		name     string
		wantFail bool
	}{
		{"nginx", "registry-1.docker.io", "library/nginx", false},
		{"docker.io/nginx", "registry-1.docker.io", "library/nginx", false},
		{"index.docker.io/nginx", "registry-1.docker.io", "library/nginx", false},
		{"docker.io/team/app", "registry-1.docker.io", "team/app", false},
		{"team/app", "registry-1.docker.io", "team/app", false},
		{"ghcr.io/team/app", "ghcr.io", "team/app", false},
		{"localhost:5000/app", "localhost:5000", "app", false},
		{"registry.example.test/a/b/c", "registry.example.test", "a/b/c", false},
		{"nginx:1.2.3", "", "", true},
		{"team/app@sha256:abc", "", "", true},
		{"team/../app", "", "", true},
		{"team\\app", "", "", true},
		{"", "", "", true},
	}
	for _, tc := range tests {
		got, err := ParseRepo(tc.ref)
		if (err != nil) != tc.wantFail {
			t.Errorf("%q: err = %v", tc.ref, err)
			continue
		}
		if err == nil && (got.Host != tc.host || got.Name != tc.name) {
			t.Errorf("%q = %+v, want %s %s", tc.ref, got, tc.host, tc.name)
		}
	}
}
