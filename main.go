package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	corev2 "github.com/sensu/core/v2"
	"github.com/sensu/sensu-plugin-sdk/sensu"
)

// Config represents the check plugin config.
type Config struct {
	sensu.PluginConfig
	State     string
	Interface string
	RouterID  string
	Family    string
	DataFile  string
	Warning   int64
	Critical  int64
}

var (
	plugin = Config{
		PluginConfig: sensu.PluginConfig{
			Name:     "sensu-keepalived-check",
			Short:    "Check keepalived VRRP instance state",
			Keyspace: "sensu.io/plugins/sensu-keepalived-check/config",
		},
	}

	options = []sensu.ConfigOption{
		&sensu.PluginConfigOption[string]{
			Path:     "state",
			Argument: "state",
			Default:  "Master",
			Usage:    "Expected (OK) state of the VRRP instance",
			Value:    &plugin.State,
		},
		&sensu.PluginConfigOption[string]{
			Path:     "interface",
			Argument: "interface",
			Usage:    "Interface of the VRRP instance",
			Value:    &plugin.Interface,
		},
		&sensu.PluginConfigOption[string]{
			Path:     "router-id",
			Argument: "router-id",
			Usage:    "Virtual router id of the VRRP instance",
			Value:    &plugin.RouterID,
		},
		&sensu.PluginConfigOption[string]{
			Path:     "family",
			Argument: "family",
			Default:  "IPv4",
			Usage:    "Address family of the VRRP instance (IPv4 or IPv6)",
			Value:    &plugin.Family,
		},
		&sensu.PluginConfigOption[string]{
			Path:     "data-file",
			Argument: "data-file",
			Default:  "/tmp/keepalived.data",
			Usage:    "Path of the data file written by keepalived PrintData",
			Value:    &plugin.DataFile,
		},
		&sensu.PluginConfigOption[int64]{
			Path:     "warning",
			Argument: "warning",
			Default:  0,
			Usage:    "Seconds in an unexpected state before WARNING (default 0: immediately)",
			Value:    &plugin.Warning,
		},
		&sensu.PluginConfigOption[int64]{
			Path:     "critical",
			Argument: "critical",
			Default:  0,
			Usage:    "Seconds in an unexpected state before CRITICAL (default 0: immediately)",
			Value:    &plugin.Critical,
		},
	}
)

// dataFileTimeout is how long to wait for keepalived to (re)write the
// data file after the PrintData method returns (the write is async).
const dataFileTimeout = 2 * time.Second

func main() {
	check := sensu.NewCheck(&plugin.PluginConfig, options, checkArgs, executeCheck, false)
	check.Execute()
}

func checkArgs(event *corev2.Event) (int, error) {
	// A non-nil error is required to make the SDK abort before
	// executeCheck; the returned status alone is ignored.
	if len(plugin.Interface) == 0 {
		return sensu.CheckStateUnknown, fmt.Errorf("--interface is required")
	}

	if len(plugin.RouterID) == 0 {
		return sensu.CheckStateUnknown, fmt.Errorf("--router-id is required")
	}

	// Normalize to the capitalization used in keepalived's object paths.
	switch strings.ToLower(plugin.Family) {
	case "ipv4":
		plugin.Family = "IPv4"
	case "ipv6":
		plugin.Family = "IPv6"
	default:
		return sensu.CheckStateUnknown, fmt.Errorf("--family must be IPv4 or IPv6, got %q", plugin.Family)
	}

	if plugin.Warning < 0 || plugin.Critical < 0 {
		return sensu.CheckStateUnknown, fmt.Errorf("--warning and --critical must not be negative")
	}
	if plugin.Warning > plugin.Critical {
		return sensu.CheckStateUnknown, fmt.Errorf("--warning (%d) must not exceed --critical (%d)", plugin.Warning, plugin.Critical)
	}
	return sensu.CheckStateOK, nil
}

func executeCheck(event *corev2.Event) (int, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		fmt.Printf("UNKNOWN: could not connect to system bus: %s\n", err)
		return sensu.CheckStateUnknown, nil
	}

	defer func() {
		_ = conn.Close()
	}()

	path := dbus.ObjectPath("/org/keepalived/Vrrp1/Instance/" + plugin.Interface + "/" + plugin.RouterID + "/" + plugin.Family)
	o := conn.Object("org.keepalived.Vrrp1", path)

	// The State property is a (us) struct of state code and name
	// (e.g. 2, "Master"); godbus decodes it directly.
	var state struct {
		Code uint32
		Name string
	}
	if err := o.StoreProperty("org.keepalived.Vrrp1.Instance.State", &state); err != nil {
		fmt.Printf("UNKNOWN: could not get state: %s\n", err)
		return sensu.CheckStateUnknown, nil
	}

	stateOK := strings.EqualFold(state.Name, plugin.State)

	// The transition timestamp gives an exact time-in-state, independent
	// of how often the check runs. It drives both the metrics and the
	// warning/critical escalation; failure to obtain it degrades the
	// metrics and makes an unexpected state CRITICAL right away — except
	// permission errors (D-Bus policy or data file), which are
	// misconfigurations the operator must fix.
	transition, transitionErr := lastTransitionTime(conn)
	if isDBusAccessDenied(transitionErr) {
		fmt.Printf("UNKNOWN: insufficient D-Bus permissions to call PrintData (see keepalived's D-Bus policy): %s\n", transitionErr)
		return sensu.CheckStateUnknown, nil
	}
	if errors.Is(transitionErr, os.ErrPermission) {
		fmt.Printf("UNKNOWN: cannot read data file (check permissions, e.g. keepalived's umask): %s\n", transitionErr)
		return sensu.CheckStateUnknown, nil
	}

	var secondsInState int64
	if transitionErr == nil {
		secondsInState = time.Now().Unix() - int64(transition)
		if secondsInState < 0 {
			// Clock skew between the check and keepalived's recorded
			// transition time must not slip a bad state past the
			// thresholds.
			secondsInState = 0
		}
	}

	exitCode := severity(stateOK, secondsInState, transitionErr == nil)

	summary := fmt.Sprintf("state is %s", state.Name)
	if !stateOK {
		summary += fmt.Sprintf(" (expected %s)", plugin.State)
	}
	if transitionErr == nil {
		summary += fmt.Sprintf(" since %s", time.Unix(int64(transition), 0).Format(time.RFC3339))
	} else {
		summary += fmt.Sprintf(", last transition unavailable: %s", transitionErr)
	}

	line := fmt.Sprintf("%s: %s", stateLabels[exitCode], summary)
	if perfdata := perfdataLine(stateOK, secondsInState, transitionErr == nil); perfdata != "" {
		line += " | " + perfdata
	}
	fmt.Println(line)
	return exitCode, nil
}

// perfdataLine builds the nagios_perfdata section emitted after the
// summary. It carries a single metric, keepalived_bad_state_seconds: the
// number of seconds the instance has been in a state other than --state (0
// while it matches), so a dashboard can graph time-in-a-bad-state directly
// (flat 0 when in --state, rising otherwise). It is derived from the last
// transition timestamp, so it is empty when that could not be read.
func perfdataLine(stateOK bool, secondsInState int64, transitionKnown bool) string {
	if !transitionKnown {
		return ""
	}
	badStateSeconds := int64(0)
	if !stateOK {
		badStateSeconds = secondsInState
	}
	return fmt.Sprintf("keepalived_bad_state_seconds=%d", badStateSeconds)
}

var stateLabels = map[int]string{
	sensu.CheckStateOK:       "OK",
	sensu.CheckStateWarning:  "WARNING",
	sensu.CheckStateCritical: "CRITICAL",
}

// severity maps the state comparison and the time spent in an unexpected
// state onto the check result. With the default thresholds (0/0) an
// unexpected state is CRITICAL immediately; --warning/--critical grant a
// grace period so a fresh failover can surface gently before it escalates.
// An unknown duration cannot prove the grace period, so it escalates.
func severity(stateOK bool, secondsInState int64, durationKnown bool) int {
	if stateOK {
		return sensu.CheckStateOK
	}
	switch {
	case !durationKnown:
		return sensu.CheckStateCritical
	case secondsInState >= plugin.Critical:
		return sensu.CheckStateCritical
	case secondsInState >= plugin.Warning:
		return sensu.CheckStateWarning
	default:
		return sensu.CheckStateOK
	}
}

// isDBusAccessDenied reports whether err stems from the bus daemon
// rejecting a call by policy (keepalived's default policy denies
// PrintData to non-root callers).
func isDBusAccessDenied(err error) bool {
	var dbusErr dbus.Error
	return errors.As(err, &dbusErr) && dbusErr.Name == "org.freedesktop.DBus.Error.AccessDenied"
}

// lastTransitionTime asks keepalived to dump its data file, then parses
// the instance's "Last transition" unix timestamp from it.
func lastTransitionTime(conn *dbus.Conn) (float64, error) {
	var prevModTime time.Time
	if fi, err := os.Stat(plugin.DataFile); err == nil {
		prevModTime = fi.ModTime()
	}

	vrrp := conn.Object("org.keepalived.Vrrp1", "/org/keepalived/Vrrp1/Vrrp")
	if call := vrrp.Call("org.keepalived.Vrrp1.Vrrp.PrintData", 0); call.Err != nil {
		return 0, fmt.Errorf("PrintData: %w", call.Err)
	}

	// PrintData only signals the VRRP process; the file is written
	// asynchronously and non-atomically, so an updated mtime does not
	// mean the write is finished. Wait for the mtime to move, then
	// treat a failed parse as a possibly incomplete dump and retry
	// until the deadline.
	deadline := time.Now().Add(dataFileTimeout)
	lastErr := fmt.Errorf("%s not updated within %s", plugin.DataFile, dataFileTimeout)
	for {
		fi, err := os.Stat(plugin.DataFile)
		switch {
		case err != nil && !errors.Is(err, os.ErrNotExist):
			// A file that cannot be stat'ed (e.g. an unsearchable
			// directory) will not become visible by waiting; only a
			// file keepalived has not created yet is worth polling.
			return 0, err
		case err == nil && fi.ModTime().After(prevModTime):
			content, err := os.ReadFile(plugin.DataFile)
			if err != nil {
				return 0, err
			}
			transition, err := lastTransition(string(content), plugin.Interface, plugin.RouterID, plugin.Family)
			if err == nil {
				return transition, nil
			}
			lastErr = err
		}
		if time.Now().After(deadline) {
			return 0, lastErr
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// lastTransition finds the VRRP instance block matching the interface,
// virtual router id, and address family in a keepalived data dump and
// returns its "Last transition" value as a unix timestamp (fractional
// seconds). IPv4 and IPv6 instances may share an interface and router id;
// keepalived marks IPv6 blocks with a "Using Native IPv6" line and leaves
// IPv4 blocks unmarked.
func lastTransition(data, iface, vrid, family string) (float64, error) {
	for _, block := range strings.Split(data, " VRRP Instance = ")[1:] {
		var matchIface, matchVrid bool
		blockFamily := "IPv4"
		var transition string

		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			switch {
			case line == "Interface = "+iface:
				matchIface = true
			case line == "Virtual Router ID = "+vrid:
				matchVrid = true
			case line == "Using Native IPv6":
				blockFamily = "IPv6"
			case strings.HasPrefix(line, "Last transition = "):
				transition = strings.TrimPrefix(line, "Last transition = ")
			}
		}

		if !matchIface || !matchVrid || blockFamily != family {
			continue
		}
		if transition == "" {
			return 0, fmt.Errorf("no Last transition for instance on %s/%s", iface, vrid)
		}

		// "1751961234.567890 (Tue Jul  8 10:33:54 2026)"
		if i := strings.IndexByte(transition, ' '); i >= 0 {
			transition = transition[:i]
		}
		return strconv.ParseFloat(transition, 64)
	}

	return 0, fmt.Errorf("no %s VRRP instance on %s/%s in %s", family, iface, vrid, plugin.DataFile)
}
