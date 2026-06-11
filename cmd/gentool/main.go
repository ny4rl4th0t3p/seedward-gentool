package main

import (
	"os"

	"github.com/ny4rl4th0t3p/cosmos-genesis-tool/pkg/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}