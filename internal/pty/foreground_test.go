package pty

import "testing"

func TestSSHTarget(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"ssh", "io-main"}, "io-main"},
		{[]string{"ssh", "-p", "2222", "gerry@example.com"}, "example.com"},
		{[]string{"ssh", "-vvp", "22", "io-main", "uptime"}, "io-main"},
		{[]string{"ssh", "-i", "~/.ssh/id_ed25519", "-o", "StrictHostKeyChecking=no", "box"}, "box"},
		{[]string{"ssh", "-l", "gerry", "io-main"}, "io-main"},
		{[]string{"ssh", "ssh://gerry@io-main:2222/"}, "io-main"},
		{[]string{"ssh", "-4", "-t", "io-main"}, "io-main"},
		{[]string{"ssh", "[2001:db8::1]:22"}, "2001:db8::1"},
		{[]string{"ssh", "--", "io-main"}, "io-main"},
		{[]string{"ssh"}, ""},
		{[]string{"ssh", "-p"}, ""},
	}
	for _, c := range cases {
		if got := SSHTarget(c.args); got != c.want {
			t.Errorf("SSHTarget(%q) = %q, want %q", c.args, got, c.want)
		}
	}
}

func TestForegroundIsShellAtPrompt(t *testing.T) {
	s, err := NewManager().Spawn(SpawnOptions{Command: []string{"/bin/sh"}, Cols: 80, Rows: 24})
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	defer s.Kill()
	fg, ok := s.Foreground()
	if !ok {
		t.Fatal("no foreground group")
	}
	if fg.Pid != s.Pid() || fg.Name != "" {
		t.Fatalf("foreground = %+v, want the shell (%d) with no command", fg, s.Pid())
	}
}
