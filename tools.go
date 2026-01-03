//go:build tools

// Package main tracks tool dependencies for this project.
// See https://go.dev/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module
package main

import (
	_ "github.com/bufbuild/buf/cmd/buf"
)
