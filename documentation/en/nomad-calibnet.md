# Running a Lotus full node on Calibnet with Nomad

This guide shows how to compile Lotus for Calibnet and launch it using a Nomad job.

## Build Lotus

Compile Lotus with the `calibnet` build tag and install it:

```bash
make clean calibnet
sudo make install
```

The `lotus` binary will be installed to `/usr/local/bin`.

## Nomad job file

The `nomad/lotus-calibnet.nomad` file starts a single Lotus daemon task using the `raw_exec` driver. Because `raw_exec` cannot mount volumes, the job expects the `/opt/lotus` repo and `/var/tmp/filecoin-proof-parameters` directory to already exist on the host.
The task uses the `nomad-lotus-entrypoint.sh` helper script which optionally imports a snapshot pointed to by the `FILECOIN_SNAPSHOT` environment variable when the node starts for the first time.
Copy this script to `/usr/local/bin/nomad-lotus-entrypoint.sh` on each node before running the job.
The job runs in the `ict-staging` namespace on the `hetzner` datacenter and pins the task to `hetzner-staging-calibnet-node`.
It exposes the API on port `1234` and opens the libp2p swarm on port `4012`.
The task registers a health-checked service and restarts with exponential backoff if it crashes.
The example configuration requests 30 CPU cores and 100&nbsp;GiB of RAM.

```bash
nomad run nomad/lotus-calibnet.nomad
```

Set `FILECOIN_SNAPSHOT` to a local `.car` file or a URL when you want the node
to import state before starting. The import only runs once because the
entrypoint writes a flag file to `$LOTUS_PATH/date_initialized` after a
successful import.

Adjust the datacenter, node constraint, or resource requirements in the job file as needed for your environment.
