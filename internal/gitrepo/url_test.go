package gitrepo

import "testing"

func TestValidateURL(t *testing.T) {
	good := []string{
		"git@git.example.test:stacks.git",
		"git.example.test:stacks.git",
		"git@git.example.test:team/stacks.git",
		"git@git.example.test:/srv/stacks.git",
		"ssh://git@git.example.test/team/stacks.git",
		"ssh://git.example.test:2222/team/stacks.git",
		"ssh://git@[2001:db8::1]:2222/team/stacks.git",
	}
	for _, u := range good {
		if err := ValidateURL(u); err != nil {
			t.Errorf("ValidateURL(%q) = %v", u, err)
		}
	}
	bad := []string{
		"", "-oProxyCommand=id", "--upload-pack=id", "-git@git.example.test:x.git",
		"git@-evil:x.git", "git@git.example.test:-x.git", "ssh://-oProxyCommand=id@host/x", "ssh://git@-evil/x.git",
		"ssh://git@git.example.test/x.git -o x", "git@git.example.test:x.git\n", "git@git.example.test:x\ty.git",
		"ext::sh -c id", "ext::id", "fd::3", "foo::bar",
		"/srv/stacks.git", "./stacks.git", "../stacks.git", "file:///srv/stacks.git", "stacks.git", "~/stacks.git",
		"https://git.example.test/stacks.git", "http://git.example.test/stacks.git", "git://git.example.test/stacks.git",
		"ssh://", "ssh:///x.git", "ssh://git@git.example.test", "ssh://git@git.example.test/",
		"ssh://git:pw@git.example.test/x.git", "ssh://git@git.example.test:port/x.git", "ssh://git@git.example.test/x.git?a=b",
		"ssh://git@git.example.test/x.git#f", "ssh://git@git.example.test/%2e%2e", "ssh://git@host name/x.git",
		"git@git.example.test:x\\y.git", "git@git.example.test:$(id)", "git@git.example.test:`id`",
		"gité@git.example.test:x.git", "git@git.example.test:x\x00.git", "a:b:c",
	}
	for _, u := range bad {
		if err := ValidateURL(u); err == nil {
			t.Errorf("ValidateURL(%q) accepted", u)
		}
	}
}

func TestValidateLocalURLStillRejectsInjection(t *testing.T) {
	for _, u := range []string{"/srv/x.git", "file:///srv/x.git", "git@git.example.test:x.git"} {
		if err := validateLocalURL(u); err != nil {
			t.Errorf("validateLocalURL(%q) = %v", u, err)
		}
	}
	for _, u := range []string{"-x", "ext::id", "/srv/x y", "relative.git", "https://h/x"} {
		if err := validateLocalURL(u); err == nil {
			t.Errorf("validateLocalURL(%q) accepted", u)
		}
	}
}
