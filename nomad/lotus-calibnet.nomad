job "lotus-calibnet" {
  namespace   = "ict-staging"
  datacenters = ["hetzner"]
  type        = "service"

  constraint {
    attribute = "${node.unique.name}"
    operator  = "="
    value     = "hetzner-staging-calibnet-node"
  }

  group "lotus" {
    count = 1
    network {
      port "api" {
        static = 1234
      }
      port "p2p" {
        to = 4012
      }
    }

    volume "lotus-data" {
      type      = "host"
      read_only = false
      source    = "lotus-data"
    }

    volume "proof-params" {
      type      = "host"
      read_only = false
      source    = "proof-params"
    }

    task "daemon" {
      driver = "raw_exec"

      config {
        command = "/usr/local/bin/lotus"
        args    = [
          "daemon",
          "--api",   "0.0.0.0:${NOMAD_PORT_api}",
          "--libp2p", "/ip4/0.0.0.0/tcp/${NOMAD_PORT_p2p}"
        ]
      }

      env {
        LOTUS_PATH                 = "/opt/lotus"
        FIL_PROOFS_PARAMETER_CACHE = "/var/tmp/filecoin-proof-parameters"
        LOTUS_NETWORK              = "calibration"
        LOTUS_FD_MAX               = "1048576"
      }

      volume_mount {
        volume      = "lotus-data"
        destination = "/opt/lotus"
        read_only   = false
      }

      volume_mount {
        volume      = "proof-params"
        destination = "/var/tmp/filecoin-proof-parameters"
        read_only   = false
      }

      resources {
        # Allocate 30 CPU cores and 100 GiB of RAM
        cpu    = 30000
        memory = 102400
      }

      service {
        name = "lotus-calib-api"
        port = "api"
        tags = ["filecoin", "lotus", "calibnet"]

        check {
          type     = "tcp"
          interval = "30s"
          timeout  = "5s"
        }
      }

      restart {
        attempts = 10
        interval = "5m"
        delay    = "30s"
        mode     = "delay"
      }
    }
  }
}
