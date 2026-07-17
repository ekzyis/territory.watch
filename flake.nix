{
  description = "Local dev shell for territory.watch";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAll = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      apps = forAll (pkgs:
        let
          serve = pkgs.writeShellApplication {
            name = "tw-serve";
            runtimeInputs = [ pkgs.caddy ];
            text = ''
              echo "serving ./static on http://localhost:8080"
              exec caddy run --adapter caddyfile --config Caddyfile
            '';
          };
        in
        {
          default = { type = "app"; program = "${serve}/bin/tw-serve"; };
        });

      devShells = forAll (pkgs: {
        default = pkgs.mkShell { packages = [ pkgs.caddy ]; };
      });
    };
}
