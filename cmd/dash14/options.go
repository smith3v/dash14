package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
)

// Options holds the parsed startup configuration derived from CLI flags.
// The app supports exactly two startup forms:
//
//	dash14 --config config.yaml
//	dash14 --config config.yaml --import teams.yaml
type Options struct {
	// ConfigPath is the path to the YAML configuration file. Required.
	ConfigPath string

	// ImportPath is the path to the teams YAML file. When non-empty the app
	// runs in import mode: it loads config, opens the database, imports teams,
	// and exits without starting the Telegram bot or overlay server.
	ImportPath string
}

// ImportMode reports whether the options select import mode.
func (o Options) ImportMode() bool {
	return o.ImportPath != ""
}

// parseOptions parses CLI flags from args (os.Args[1:]) and writes usage
// output to w. It returns a non-nil error if required flags are missing or
// unexpected positional arguments are present.
func parseOptions(args []string, w io.Writer) (Options, error) {
	fs := flag.NewFlagSet("dash14", flag.ContinueOnError)
	fs.SetOutput(w)
	fs.Usage = func() {
		fmt.Fprintf(w, "Usage:\n")
		fmt.Fprintf(w, "  dash14 --config <path>                   run the normal runtime\n")
		fmt.Fprintf(w, "  dash14 --config <path> --import <path>   import teams and exit\n\n")
		fmt.Fprintf(w, "Flags:\n")
		fs.PrintDefaults()
	}

	var opts Options
	fs.StringVar(&opts.ConfigPath, "config", "", "path to YAML configuration file (required)")
	fs.StringVar(&opts.ImportPath, "import", "", "path to teams YAML file; when set, runs import mode and exits")

	if err := fs.Parse(args); err != nil {
		// flag.ContinueOnError already wrote the error to w.
		return Options{}, err
	}

	if fs.NArg() > 0 {
		fmt.Fprintf(w, "error: unexpected positional arguments: %v\n", fs.Args())
		fs.Usage()
		return Options{}, errors.New("unexpected positional arguments")
	}

	if opts.ConfigPath == "" {
		fmt.Fprintf(w, "error: --config is required\n")
		fs.Usage()
		return Options{}, errors.New("--config is required")
	}

	return opts, nil
}
