{ pkgs, ... }:

{
  packages = with pkgs; [
    go
    gopls
    golangci-lint
    jq
  ];

  scripts = {
    build.exec = ''
      mkdir -p ./bin
      go build -o ./bin/redeem ./cmd/redeem
    '';
    test.exec = "go test ./...";
    lint.exec = "golangci-lint run ./...";
    install-local.exec = ''
      mkdir -p "$HOME/.local/bin"
      go build -o "$HOME/.local/bin/redeem" ./cmd/redeem
    '';
    uninstall-local.exec = ''
      rm -f "$HOME/.local/bin/redeem"
      echo "removed ~/.local/bin/redeem"
    '';
    run.exec = "go run ./cmd/redeem --help";
  };

  enterShell = ''
    echo "[devenv] terminal-redeemer environment active"
  '';

  enterTest = ''
    go test ./...
  '';
}
