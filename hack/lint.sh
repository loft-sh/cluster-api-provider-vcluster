#!/usr/bin/env bash

set -e

go build -o "tools/logcheck.so" -buildmode=plugin -mod=vendor ./vendor/sigs.k8s.io/logtools/logcheck/plugin
go build -o "tools/golangci-lint" -mod=vendor ./vendor/github.com/golangci/golangci-lint/cmd/golangci-lint
LOGCHECK_CONFIG="hack/logcheck.conf" tools/golangci-lint run "$@" ./...
