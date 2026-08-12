package runtime

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// A task must run as the host user: the snapshot belongs to that user on the host,
// and an owner mismatch inside means either the agent cannot write or we cannot
// read what it wrote.
func TestHostUserIsTheCallingUser(t *testing.T) {
	got := HostUser()
	want := strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid())
	if os.Getuid() < 0 {
		if got != "" {
			t.Errorf("HostUser() = %q on a platform without uids", got)
		}
		return
	}
	if got != want {
		t.Errorf("HostUser() = %q, want %q", got, want)
	}
}

func TestLimitsArgs(t *testing.T) {
	got := strings.Join(Limits{CPUs: "1", Memory: "512m", PIDs: 512}.Args(), " ")
	want := "--cpus 1 --memory 512m --pids-limit 512"
	if got != want {
		t.Errorf("Args() = %q, want %q", got, want)
	}
	if n := len(Limits{}.Args()); n != 0 {
		t.Errorf("an empty Limits produced %d flags", n)
	}
	if got := strings.Join(Limits{Memory: "2g"}.Args(), " "); got != "--memory 2g" {
		t.Errorf("partial limits = %q", got)
	}
}
