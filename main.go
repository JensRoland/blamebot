package main

import (
	"fmt"
	"os"

	"github.com/jensroland/git-blamebot/cmd"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		cmd.RunQuery(os.Args[1:])
		return
	}

	switch os.Args[1] {
	case "hook":
		cmd.RunHook(os.Args[2:])
	case "enable":
		cmd.RunEnable(os.Args[2:])
	case "disable":
		cmd.RunDisable(os.Args[2:])
	case "--version":
		fmt.Println("git-blamebot", version)
	default:
		cmd.RunQuery(os.Args[1:])
	}
}
