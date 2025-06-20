{
  description = "Tailscale systray (standalone flake)";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
      in {
        packages.default = pkgs.buildGoModule {
          pname = "tailscale-systray";
          version = "unstable";
          src = ./.;
          vendorHash = null;
          nativeBuildInputs = [ pkgs.pkg-config ];
          buildInputs = [ pkgs.gtk3 pkgs.libayatana-appindicator ];
          proxyVendor = true;
          meta = with pkgs.lib; {
            description = "Tailscale systray";
            homepage = "https://github.com/mattn/tailscale-systray";
            license = licenses.mit;
            mainProgram = "tailscale-systray";
          };
        };
        devShells.default = pkgs.mkShell {
          nativeBuildInputs = [ pkgs.pkg-config ];
          buildInputs = [ pkgs.gtk3 pkgs.libayatana-appindicator ];
        };
      }
    );
}
