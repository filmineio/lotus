package main

import (
	"errors"
	"fmt"

	marketauth "github.com/filecoin-project/lotus/sealing_market/market_auth"
	"github.com/google/uuid"
	"github.com/urfave/cli/v2"
)

const marketURIFlagName = "market-uri"
const remotePr2FlagName = "remotePr2"

var sealingMarketCmd = &cli.Command{
	Name:  "sealing-market",
	Usage: "Commands related to the sealing market",
	Subcommands: []*cli.Command{
		loginToSealingMarketCmd,
	},
}

var loginToSealingMarketCmd = &cli.Command{
	Name:        "register",
	Usage:       "Register sealing worker using Sealing Market's OTP",
	Description: "Used to register lotus-worker with the Sealing Market. You will need OTP code from the Sealing Market to complete the registration.",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    FlagWorkerRepo,
			Aliases: []string{FlagWorkerRepoDeprecation},
			EnvVars: []string{"LOTUS_WORKER_PATH", "WORKER_PATH"},
			Value:   "~/.lotusworker", // TODO: Consider XDG_DATA_HOME
			Usage:   fmt.Sprintf("Specify worker repo path. flag %s and env WORKER_PATH are DEPRECATION, will REMOVE SOON", FlagWorkerRepoDeprecation),
		},
		marketUriFlag,
	},
	Action: func(ctx *cli.Context) error {
		workerRepoPath := ctx.String(FlagWorkerRepo)
		fmt.Printf("Repo: %s\n", workerRepoPath)

		otp, err := findUuid(ctx)
		if err != nil {
			return err
		}

		auth, err := marketauth.New(ctx.String(marketURIFlagName), workerRepoPath)
		if err != nil {
			return fmt.Errorf("market auth: %w", err)
		}
		_, err = auth.Register(otp.String())
		if err != nil {
			return fmt.Errorf("error registering appliance: %w", err)
		}

		fmt.Println("successfully registered as a market appliance. you can now restart in daemon mode.")

		return nil
	},
}

func findUuid(ctx *cli.Context) (uuid.UUID, error) {
	for _, v := range ctx.Args().Slice() {
		uuid, err := uuid.Parse(v)
		if err == nil {
			return uuid, nil
		}
	}

	return uuid.UUID{}, errors.New("could not find OTP in command line arguments. Please try again with a valid OTP")
}

var marketUriFlag = &cli.StringFlag{
	Name:     marketURIFlagName,
	Usage:    "The URL to query the market on",
	Value:    "http://localhost:3000",
	Required: true,
}
