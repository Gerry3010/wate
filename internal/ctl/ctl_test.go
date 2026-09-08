package ctl

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.sock")
	s, err := Listen(path, func(r Request) Response {
		if r.Cmd == "echo" {
			return Response{OK: true, Data: r.Text}
		}
		return Response{Error: "unknown " + r.Cmd}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	resp, err := Send(path, Request{Cmd: "echo", Text: "hi"})
	if err != nil || !resp.OK || resp.Data != "hi" {
		t.Fatalf("resp %+v err %v", resp, err)
	}
	resp, _ = Send(path, Request{Cmd: "nope"})
	if resp.OK || resp.Error == "" {
		t.Fatalf("expected error, got %+v", resp)
	}
	s.Close()
	if _, err := os.Stat(path); err == nil {
		t.Fatal("socket file should be removed on close")
	}
}

func TestFindSocketPrefersEnv(t *testing.T) {
	t.Setenv("YATE_SOCKET", "/tmp/x.sock")
	p, err := FindSocket()
	if err != nil || p != "/tmp/x.sock" {
		t.Fatalf("%q %v", p, err)
	}
}

func TestProcessAlive(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Fatal("own process must be alive")
	}
	if processAlive(999999999) {
		t.Fatal("bogus pid must be dead")
	}
}
