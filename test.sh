#!/bin/sh

set -e

# Setup local bin
mkdir -p bin
export GOBIN=$PWD/bin
export PATH=$GOBIN:$PATH

# Build `openapi-cli-generator`
go install .

# Generate our test example app
cd example-cli
rm -rf main.go internal go.mod go.sum
go mod init example-cli
openapi-cli-generator init example-cli
openapi-cli-generator generate openapi.yaml
sed -i'' -e 's/\/\/ TODO: Add register commands here./openapiRegister(false)/' main.go
# Add necessary dependencies for the example app
go mod tidy
go install .
cd ..

# Run all the tests!
go test "$@" ./...
