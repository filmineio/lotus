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
