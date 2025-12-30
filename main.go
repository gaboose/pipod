package main

import (
	"github.com/alecthomas/kong"
)

type CLI struct {
	Container ContainerCmd `cmd:"" help:"Manage container images"`
	Disk      DiskCmd      `cmd:"" help:"Manage disk images"`
	Sync      SyncCmd      `cmd:"" help:"Sync a disk image from a tar stream, a container image or another disk image"`
}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli, kong.Name("pipod"))
	err := ctx.Run()
	ctx.FatalIfErrorf(err)
}
