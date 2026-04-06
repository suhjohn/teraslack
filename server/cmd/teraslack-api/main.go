package main

import (
	"context"
	"os"

	"github.com/johnsuh/teraslack/server/internal/openapicli"
)

func main() {
	os.Exit(openapicli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
