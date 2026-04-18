//go:build tools

// Package main tracks tool dependencies for this project.
// See https://go.dev/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module
package main

import (
	_ "connectrpc.com/connect/cmd/protoc-gen-connect-go"
	_ "cuelang.org/go/cmd/cue"
	_ "filippo.io/mkcert"
	_ "github.com/bufbuild/buf/cmd/buf"
	_ "github.com/fullstorydev/grpcurl/cmd/grpcurl"
	_ "github.com/rogpeppe/go-internal/testscript"
	_ "google.golang.org/protobuf/cmd/protoc-gen-go"
	_ "sigs.k8s.io/controller-tools/cmd/controller-gen"
)
