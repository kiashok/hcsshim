//go:build windows

package main

import (
	gcontext "context"
	"encoding/json"
	"fmt"
	"os"

	hcstypes "github.com/Microsoft/hcsshim/hcs"
	"github.com/Microsoft/hcsshim/internal/appargs"
	"github.com/urfave/cli"
)

var psCommand = cli.Command{
	Name:      "ps",
	Usage:     "ps displays the processes running inside a container",
	ArgsUsage: `<container-id> [ps options]`,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "format, f",
			Value: "json",
			Usage: `select one of: ` + formatOptions,
		},
	},
	Before: appargs.Validate(argID),
	Action: func(context *cli.Context) error {
		id := context.Args().First()
		container, err := getContainer(id, true)
		if err != nil {
			return err
		}
		defer container.Close()

		props, err := container.hc.Properties(gcontext.Background(), hcstypes.PropertyTypeProcessList)
		if err != nil {
			return err
		}

		var pids []int
		for _, p := range props.ProcessList {
			pids = append(pids, int(p.ProcessId))
		}

		switch context.String("format") {
		case "json":
			return json.NewEncoder(os.Stdout).Encode(pids)
		default:
			return fmt.Errorf("invalid format option")
		}
	},
	SkipArgReorder: true,
}
