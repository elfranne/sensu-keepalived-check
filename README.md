[![Sensu Bonsai Asset](https://img.shields.io/badge/Bonsai-Download%20Me-brightgreen.svg?colorB=89C967&logo=sensu)](https://bonsai.sensu.io/assets/elfranne/sensu-keepalived-check)
![Go Test](https://github.com/elfranne/sensu-keepalived-check/workflows/Go%20Test/badge.svg)
![goreleaser](https://github.com/elfranne/sensu-keepalived-check/workflows/goreleaser/badge.svg)

# sensu-keepalived-check

## Table of Contents

- [Overview](#overview)
- [Usage examples](#usage-examples)
- [Configuration](#configuration)
  - [Asset registration](#asset-registration)
  - [Check definition](#check-definition)
- [Installation from source](#installation-from-source)
- [Additional notes](#additional-notes)
- [Contributing](#contributing)

## Overview

The sensu-keepalived-check is a [Sensu Check][6] that queries [keepalived][11] over the system
D-Bus and reports the current state of a VRRP instance. The check goes critical when the instance
is not in the expected state (for example, a node expected to be `Master` that has fallen back to
`Backup` or `Fault`), which is useful for catching failovers and split-brain situations.

Keepalived must be started with its D-Bus interface enabled (`--dbus` / `enable_dbus` in the
global configuration) for this check to work.

## Usage examples

```text
Usage:
  sensu-keepalived-check [flags]

Flags:
  -h, --help               help for sensu-keepalived-check
      --interface string   Interface of the VRRP instance
      --router-id string   Virtual router id of the VRRP instance
      --state string       Expected (OK) state of the VRRP instance (default "Master")
      --data-file string   Path of the data file written by keepalived PrintData (default "/tmp/keepalived.data")
      --family string      Address family of the VRRP instance (IPv4 or IPv6) (default "IPv4")
      --warning int        Seconds in an unexpected state before WARNING (default 0: immediately)
      --critical int       Seconds in an unexpected state before CRITICAL (default 0: immediately)
```

- `--interface` and `--router-id` identify the VRRP instance and are required. They correspond
  to the interface and virtual router id configured in your `vrrp_instance`, and together with
  `--family` form the D-Bus object path
  `/org/keepalived/Vrrp1/Instance/<interface>/<router-id>/<family>`.
- `--family` selects the address family of the VRRP instance, `IPv4` (default) or `IPv6`.
- `--data-file` is the path of the state dump keepalived writes when asked for the last
  transition timestamp. Only change it if your keepalived writes its data file somewhere
  other than `/tmp/keepalived.data`.
- `--state` is the state the instance is expected to be in: `Master`, `Backup`, or `Fault`.
  The comparison is case-insensitive but must match the full state name. Defaults to `Master`.
- `--warning` and `--critical` grant a grace period before an unexpected state alerts: the
  check stays OK until the instance has been in the unexpected state for `--warning` seconds,
  reports WARNING until `--critical` seconds, and CRITICAL after that. With the defaults (0)
  an unexpected state is CRITICAL immediately. If the last transition timestamp cannot be
  read, the time in state is unknown and an unexpected state escalates to CRITICAL right away.

## Configuration

### Asset registration

[Sensu Assets][10] are the best way to make use of this plugin. If you're not using an asset, please
consider doing so! If you're using sensuctl 5.13 with Sensu Backend 5.13 or later, you can use the
following command to add the asset:

```sh
sensuctl asset add elfranne/sensu-keepalived-check
```

If you're using an earlier version of sensuctl, you can find the asset on the [Bonsai Asset Index][8].

### Check definition

```yml
---
type: CheckConfig
api_version: core/v2
metadata:
  name: sensu-keepalived-check
  namespace: default
spec:
  command: sensu-keepalived-check --interface ens256 --router-id 61 --state Master
  output_metric_format: nagios_perfdata
  subscriptions:
  - keepalived
  runtime_assets:
  - elfranne/sensu-keepalived-check
```

The check emits [nagios perfdata][12] after the status summary: `exit_code` (0 when the
instance is in the expected state, 2 otherwise) and, when the last transition timestamp is
available, `seconds_in_state` (how long the instance has been in its current state, as of
the check run) and `last_transition` (unix timestamp of the last state change). Use
`seconds_in_state` for alert thresholds; for dashboards, compute `now() - last_transition`
to display the live duration with second precision regardless of the check interval. Setting
`output_metric_format: nagios_perfdata` makes Sensu extract them as metrics; add
`output_metric_handlers` to forward them to your metrics pipeline.

## Installation from source

The preferred way of installing and deploying this plugin is to use it as an Asset. If you would
like to compile and install the plugin from source or contribute to it, download the latest version
or create an executable script from this source.

From the local path of the sensu-keepalived-check repository:

```sh
go build
```

## Additional notes

The check connects to the system D-Bus, so it must run on the same host as the keepalived
instance being monitored (typically with sufficient privileges to reach the
`org.keepalived.Vrrp1` bus name).

## Contributing

For more information about contributing to this plugin, see [Contributing][1].

[1]: https://github.com/sensu/sensu-go/blob/master/CONTRIBUTING.md
[6]: https://docs.sensu.io/sensu-go/latest/reference/checks/
[8]: https://bonsai.sensu.io/assets/elfranne/sensu-keepalived-check
[10]: https://docs.sensu.io/sensu-go/latest/reference/assets/
[11]: https://keepalived.org/
[12]: https://docs.sensu.io/sensu-go/latest/observability-pipeline/observe-schedule/collect-metrics-with-checks/
