package app

import "testing"

func TestIsDark(t *testing.T) {
	cases := map[string]bool{"#1e1e2e": true, "#000000": true, "#ffffff": false, "#eff1f5": false, "": true, "nope": true}
	for in, want := range cases {
		if got := isDark(in); got != want {
			t.Errorf("isDark(%q) = %v, want %v", in, got, want)
		}
	}
}
