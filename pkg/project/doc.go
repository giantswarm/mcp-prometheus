// Package project exposes the build identifiers stamped at link time: the
// generated Makefile.gen.go.mk and the architect-orb `go-build` job set
// `version`, `gitSHA` and `buildTimestamp` via `-X` ldflags. It has no
// dependencies so it can be safely imported by `main` and any CLI command.
package project
