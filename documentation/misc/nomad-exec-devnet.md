# Running a custom devnet with Nomad's exec driver

This example assumes you have built Lotus with 512MiB sectors, a 1 epoch
wait-seed delay and a 14 second block time. The helper script
`scripts/fetch-and-preseal.sh` can fetch the required proof parameters and
pre-seal 16 sectors in advance.

```bash
./scripts/fetch-and-preseal.sh
```

The following Nomad job file runs the daemon and miner tasks using the `exec`
driver. Adjust the `source` paths for the `host` volumes to match your
environment.

```hcl
job "lotus-devnet" {
  datacenters = ["dc1"]

  group "lotus" {
    task "daemon" {
      driver = "exec"
      config {
        command = "/usr/local/bin/lotus"
        args = [
          "daemon",
          "--lotus-make-genesis=/config/dev.gen",
          "--genesis-template=/config/devnet.json",
          "--bootstrap=false"
        ]
      }
      env {
        LOTUS_PATH   = "/var/lib/lotus"
        TRUST_PARAMS = "1"
      }
      volume_mount { volume = "lotus-data"  destination = "/var/lib/lotus" }
      volume_mount { volume = "presealed"   destination = "/presealed" }
    }

    task "miner" {
      driver = "exec"
      config {
        command = "/usr/local/bin/lotus-miner"
        args    = ["run"]
      }
      env {
        LOTUS_PATH       = "/var/lib/lotus"
        LOTUS_MINER_PATH = "/var/lib/lotus-miner"
      }
      volume_mount { volume = "lotus-data"  destination = "/var/lib/lotus" }
      volume_mount { volume = "presealed"   destination = "/presealed" }
      volume_mount { volume = "miner-data" destination = "/var/lib/lotus-miner" }
    }
  }

  volume "lotus-data" { type = "host" source = "lotus-data" }
  volume "miner-data" { type = "host" source = "miner-data" }
  volume "presealed"  { type = "host" source = "presealed"  }
}
```

Submit the job with:

```bash
nomad job run lotus-devnet.nomad
```
