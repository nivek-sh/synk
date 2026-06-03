{
  description = "synk - generate OpenSSH config from Bitwarden profiles";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
    in
    {
      packages = forAllSystems (system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        {
          default = pkgs.buildGoModule {
            pname = "synk";
            version = "0.1.0";
            src = ./.;
            vendorHash = "sha256-z7GPdpqPK/DGMzkM6xAWa+NaJMwsDMF0XSWtP8wg5GQ=";
            ldflags = [
              "-s"
              "-w"
              "-X synk/internal/cli.Version=0.1.0"
            ];
          };
        });

      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/synk";
          meta.description = "Generate OpenSSH config from Bitwarden profiles";
        };
      });

      devShells = forAllSystems (system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        {
          default = pkgs.mkShell {
            packages = [
              pkgs.bitwarden-cli
              pkgs.git
              pkgs.go
              pkgs.golangci-lint
              pkgs.gopls
              pkgs.gotools
              pkgs.just
            ];

            shellHook = ''
              repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
              mkdir -p "$repo_root/.nix-dev/bin"

              cat > "$repo_root/.nix-dev/bin/synk" <<EOF
                #!/usr/bin/env sh
                set -eu
                cd "$repo_root"
                exec go run -buildvcs=false ./cmd/synk "\$@"
              EOF

              chmod +x "$repo_root/.nix-dev/bin/synk"
              export PATH="$repo_root/.nix-dev/bin:$PATH"

              echo "synk dev shell ready: synk --help"
            '';
          };
        });

      checks = forAllSystems (system: {
        synk = self.packages.${system}.default;
      });
    };
}
