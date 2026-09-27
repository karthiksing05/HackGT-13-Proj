#!/bin/bash

# Build script for Debian 12 (amd64)
# Builds into Events/bin wherever it is run from (deploy.sh calls it by path).
cd "$(dirname "${BASH_SOURCE[0]}")" || exit 1
echo "Building Events for Debian 12 (amd64)..."

# Set target OS to Linux and architecture to amd64
export GOOS=linux
export GOARCH=amd64

# Build the binary
go build -o bin/events-server main.go

if [ $? -eq 0 ]; then
    echo "Build successful! Binary is located at bin/events-server"
else
    echo "Build failed!"
    exit 1
fi
