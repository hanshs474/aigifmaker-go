// Command aigifmaker turns text into an animated GIF on AI GIF Maker
// (https://aigifmaker.ai) and prints the GIF URL.
//
//	aigifmaker "a cat dancing in the rain, pixel art"
//	aigifmaker credits
//
// Set AIGIFMAKER_API_KEY (create a key at https://aigifmaker.ai/settings/apikeys).
// Exit codes: 0 ok, 1 failure, 2 usage, 3 key problem, 4 out of credits.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	aigifmaker "github.com/hanshs474/aigifmaker-go"
)

func main() {
	args := os.Args[1:]
	ctx := context.Background()
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintf(os.Stderr, "usage: aigifmaker \"prompt\"   prints the GIF URL\n       aigifmaker credits    prints the credits left\n       aigifmaker version\n\nNeeds %s (create a key at %s).\n", aigifmaker.EnvKey, aigifmaker.KeysURL)
		os.Exit(2)
	}
	switch {
	case len(args) == 1 && args[0] == "credits":
		n, err := aigifmaker.New("").Credits(ctx)
		exit(err)
		fmt.Println(n)
		return
	case len(args) == 1 && (args[0] == "version" || args[0] == "--version"):
		fmt.Println("aigifmaker", aigifmaker.Version)
		return
	}
	url, err := aigifmaker.Generate(ctx, strings.Join(args, " "))
	exit(err)
	fmt.Println(url)
}

func exit(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, err)
	switch {
	case errors.Is(err, aigifmaker.ErrAuth):
		os.Exit(3)
	case errors.Is(err, aigifmaker.ErrQuota):
		os.Exit(4)
	}
	os.Exit(1)
}
