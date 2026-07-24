package main

import (
	"fmt"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/sensu/sensu-plugin-sdk/sensu"
)

const sampleData = `------< VRRP Topology >------
 VRRP Instance = VI_1
   VRRP Version = 2
   State = MASTER
   Flags: none
   Wantstate = MASTER
   Last transition = 1751961234.567890 (Tue Jul  8 10:33:54 2026)
   Interface = eth0
   Using src_ip = 192.168.1.10
   Virtual Router ID = 51
   Priority = 150
 VRRP Instance = VI_2
   VRRP Version = 2
   State = BACKUP
   Last transition = 1751900000 (Mon Jul  7 17:33:20 2026)
   Interface = eth1
   Virtual Router ID = 52
   Priority = 100
`

const dualStackData = ` VRRP Instance = VI_4
   VRRP Version = 2
   State = MASTER
   Last transition = 1751961234.567890 (Tue Jul  8 10:33:54 2026)
   Interface = eth0
   Virtual Router ID = 51
 VRRP Instance = VI_6
   VRRP Version = 3
   Using Native IPv6
   State = BACKUP
   Last transition = 1751900000.000000 (Mon Jul  7 17:33:20 2026)
   Interface = eth0
   Virtual Router ID = 51
`

func TestLastTransition(t *testing.T) {
	got, err := lastTransition(sampleData, "eth0", "51", "IPv4")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if got != 1751961234.567890 {
		t.Errorf("got %f, want 1751961234.567890", got)
	}
}

func TestLastTransitionNoFraction(t *testing.T) {
	// Older keepalived versions print whole seconds only.
	got, err := lastTransition(sampleData, "eth1", "52", "IPv4")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if got != 1751900000 {
		t.Errorf("got %f, want 1751900000", got)
	}
}

func TestLastTransitionFamily(t *testing.T) {
	// IPv4 and IPv6 instances may share interface and router id; the
	// family must disambiguate them.
	got, err := lastTransition(dualStackData, "eth0", "51", "IPv4")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if got != 1751961234.567890 {
		t.Errorf("IPv4: got %f, want 1751961234.567890", got)
	}

	got, err = lastTransition(dualStackData, "eth0", "51", "IPv6")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if got != 1751900000 {
		t.Errorf("IPv6: got %f, want 1751900000", got)
	}

	if _, err := lastTransition(sampleData, "eth0", "51", "IPv6"); err == nil {
		t.Error("expected error for IPv6 lookup of an IPv4-only instance, got nil")
	}
}

func TestLastTransitionNoMatch(t *testing.T) {
	if _, err := lastTransition(sampleData, "eth0", "99", "IPv4"); err == nil {
		t.Error("expected error for unknown vrid, got nil")
	}
	if _, err := lastTransition("", "eth0", "51", "IPv4"); err == nil {
		t.Error("expected error for empty data, got nil")
	}
}

func TestSeverity(t *testing.T) {
	cases := []struct {
		name              string
		warning, critical int64
		stateOK           bool
		seconds           int64
		durationKnown     bool
		want              int
	}{
		{"expected state is OK", 60, 300, true, 9999, true, sensu.CheckStateOK},
		{"default thresholds escalate immediately", 0, 0, false, 0, true, sensu.CheckStateCritical},
		{"within grace period", 60, 300, false, 59, true, sensu.CheckStateOK},
		{"warning threshold reached", 60, 300, false, 60, true, sensu.CheckStateWarning},
		{"between thresholds", 60, 300, false, 299, true, sensu.CheckStateWarning},
		{"critical threshold reached", 60, 300, false, 300, true, sensu.CheckStateCritical},
		{"unknown duration escalates", 60, 300, false, 0, false, sensu.CheckStateCritical},
		{"warning only at zero", 0, 300, false, 10, true, sensu.CheckStateWarning},
	}

	defer func() { plugin.Warning, plugin.Critical = 0, 0 }()
	for _, c := range cases {
		plugin.Warning, plugin.Critical = c.warning, c.critical
		if got := severity(c.stateOK, c.seconds, c.durationKnown); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestPerfdataLine(t *testing.T) {
	cases := []struct {
		name            string
		stateOK         bool
		secondsInState  int64
		transitionKnown bool
		want            string
	}{
		{
			// Expected state: the metric is 0 even though the instance has
			// been in the (correct) state for a while.
			"state matches --state",
			true, 42, true,
			"keepalived_bad_state_seconds=0",
		},
		{
			// Wrong state: the metric tracks seconds_in_state.
			"state does not match --state",
			false, 10, true,
			"keepalived_bad_state_seconds=10",
		},
		{
			// The transition timestamp is unreadable, so the metric is
			// unknown and no perfdata is emitted.
			"transition unavailable emits no metric",
			false, 0, false,
			"",
		},
	}

	for _, c := range cases {
		if got := perfdataLine(c.stateOK, c.secondsInState, c.transitionKnown); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestIsDBusAccessDenied(t *testing.T) {
	denied := dbus.Error{Name: "org.freedesktop.DBus.Error.AccessDenied"}
	if !isDBusAccessDenied(fmt.Errorf("PrintData: %w", denied)) {
		t.Error("expected true for wrapped AccessDenied error")
	}

	if isDBusAccessDenied(dbus.Error{Name: "org.freedesktop.DBus.Error.NoReply"}) {
		t.Error("expected false for other dbus error")
	}
	if isDBusAccessDenied(fmt.Errorf("plain error")) {
		t.Error("expected false for non-dbus error")
	}
	if isDBusAccessDenied(nil) {
		t.Error("expected false for nil")
	}
}
