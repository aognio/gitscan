package cmd

// Version is the gitscan release version. Overridden at build time with:
//
//	go build -ldflags "-X github.com/aognio/gitscan/cmd.Version=v0.1.0" ./cmd/gitscan
//
// or via Makefile:
//
//	make build VERSION=v0.1.0
var Version = "v0.1.0"
