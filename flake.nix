{
  description = "Tailscale systray (standalone flake)";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/249fbde2a178a2ea2638b65b9ecebd531b338cf9";
    flake-utils = {
      url = "github:numtide/flake-utils";
      inputs.nixpkgs.follows = "nixpkgs";
    };
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
          vendorHash = "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";
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
