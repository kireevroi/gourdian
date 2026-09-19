// Package buildinfo holds values stamped in at build time.
package buildinfo

// Version is set with -ldflags "-X dotatrainer/internal/buildinfo.Version=1.2.3" from the VERSION file.
var Version = "dev"
