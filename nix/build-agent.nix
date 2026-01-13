# NixOS module for deploying the build-agent worker
#
# Example usage in your NixOS configuration:
#
#   imports = [ ./path/to/build-agent.nix ];
#
#   services.build-agent = {
#     enable = true;
#     serverUrl = "wss://coordinator.example.com:8080/ws";
#     package = pkgs.build-agent;  # Your build-agent package
#     maxJobs = 4;
#   };
#
{ config, lib, pkgs, ... }:

with lib;

let
  cfg = config.services.build-agent;

  configFile = pkgs.writeText "build-agent.toml" ''
    [server]
    url = "${cfg.serverUrl}"

    [worker]
    id = "${cfg.workerId}"
    max_jobs = ${toString cfg.maxJobs}

    [storage]
    git_cache_dir = "${cfg.gitCacheDir}"
    worktree_dir = "${cfg.worktreeDir}"
  '';
in {
  options.services.build-agent = {
    enable = mkEnableOption "build agent worker";

    serverUrl = mkOption {
      type = types.str;
      description = "WebSocket URL of the coordinator";
      example = "wss://central-server:8080/ws";
    };

    workerId = mkOption {
      type = types.str;
      default = config.networking.hostName;
      description = "Unique worker identifier";
    };

    maxJobs = mkOption {
      type = types.int;
      default = 4;
      description = "Maximum concurrent jobs";
    };

    gitCacheDir = mkOption {
      type = types.str;
      default = "/var/cache/build-agent/repos";
      description = "Directory for git cache";
    };

    worktreeDir = mkOption {
      type = types.str;
      default = "/tmp/build-agent/jobs";
      description = "Directory for job worktrees";
    };

    package = mkOption {
      type = types.package;
      description = "build-agent package to use";
    };
  };

  config = mkIf cfg.enable {
    systemd.services.build-agent = {
      description = "Build Agent Worker";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];

      serviceConfig = {
        Type = "simple";
        ExecStart = "${cfg.package}/bin/build-agent --config ${configFile}";
        Restart = "always";
        RestartSec = 10;

        # Security hardening
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ReadWritePaths = [ cfg.gitCacheDir cfg.worktreeDir ];

        # Create directories
        StateDirectory = "build-agent";
        CacheDirectory = "build-agent";
      };
    };

    # Ensure directories exist
    systemd.tmpfiles.rules = [
      "d ${cfg.gitCacheDir} 0755 root root -"
      "d ${cfg.worktreeDir} 0755 root root -"
    ];

    # Ensure nix with flakes is available
    nix.settings.experimental-features = [ "nix-command" "flakes" ];
  };
}
