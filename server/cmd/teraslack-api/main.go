package main

import (
	"context"
	"os"

	"github.com/suhjohn/teraslack/internal/openapicli"
)

func main() {
	os.Exit(openapicli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
