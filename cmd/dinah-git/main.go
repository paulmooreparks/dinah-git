package main

import (
	"context"
	"encoding/json"
	"fmt"
	extension "github.com/paulmooreparks/dinah-extension-go"
	gitextension "github.com/paulmooreparks/dinah-git"
	"os"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--manifest" {
		data, _ := json.MarshalIndent(gitextension.Manifest(), "", "  ")
		fmt.Println(string(data))
		return
	}
	if err := extension.Serve(context.Background(), gitextension.Manifest(), gitextension.New().Handlers()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
