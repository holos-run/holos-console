//go:build tools

// Package main tracks tool dependencies for this project.
// See https://go.dev/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module
package main

import (
	_ "filippo.io/mkcert"
	_ "github.com/bufbuild/buf/cmd/buf"
	_ "github.com/fullstorydev/grpcurl/cmd/grpcurl"
	_ "github.com/rogpeppe/go-internal/testscript"
)
