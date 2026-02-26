package format

import (
	"os"

	"golang.org/x/term"
)

var (
	Reset   = "\033[0m"
	Bold    = "\033[1m"
	Dim     = "\033[2m"
	Yellow  = "\033[33m"
	Cyan    = "\033[36m"
	Green   = "\033[32m"
	Magenta = "\033[35m"
	Blue    = "\033[34m"
	Red     = "\033[31m"
)

func init() {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		disableColors()
	} else if !term.IsTerminal(int(os.Stdout.Fd())) {
		disableColors()
	}
}

func disableColors() {
	Reset, Bold, Dim = "", "", ""
	Yellow, Cyan, Green, Magenta, Blue, Red = "", "", "", "", "", ""
}

// TermWidth returns the terminal width, defaulting to 80.
func TermWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 80
	}
	return w
}
