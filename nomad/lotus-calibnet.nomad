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
    network {
      port "api" { static = 1234 }
      port "p2p" { to = 4012 }
    }

    task "daemon" {
      driver = "raw_exec"

      config {
        command = "/usr/local/bin/nomad-lotus-entrypoint.sh"
        args    = [
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
      resources {
        # needs 30 vCPU on the node
        cpu    = 30000
        # 100 GiB
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
