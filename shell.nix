{ pkgs ? import <nixpkgs> {} }:

let
  pythonEnv = pkgs.python3.withPackages (ps: with ps; [
    virtualenv
    pip
    pytest
    black
    flake8
  ]);
in pkgs.mkShell {
  name = "go-python-dev-shell";
  buildInputs = [
    pkgs.go
    pythonEnv
    pkgs.docker
    pkgs."docker-compose"
  ];
  shellHook = ''
    echo "Entering Go+Python development shell"
    export GOPATH=$PWD/.direnv/gopath
    export PATH=$GOPATH/bin:$PATH
  '';
}
