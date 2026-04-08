package main

import (
	"context"
	"os"

	"github.com/johnsuh/teraslack/server/internal/openapicli"
)

func main() {
	os.Exit(openapicli.RunMCPServer(context.Background(), os.Stdin, os.Stdout, os.Stderr))
}
