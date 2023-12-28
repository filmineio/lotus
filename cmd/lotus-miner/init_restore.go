package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"path/filepath"

	"github.com/docker/go-units"
	"github.com/google/uuid"
	"github.com/ipfs/go-datastore"
	dsq "github.com/ipfs/go-datastore/query"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"
	"gopkg.in/cheggaaa/pb.v1"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-paramfetch"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/lotus/api"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v0api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/lib/backupds"
	"github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/node/repo"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

var restoreCmd = &cli.Command{
	Name:  "restore",
	Usage: "Initialize a lotus miner repo from a backup",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "nosync",
			Usage: "don't check full-node sync status",
		},
		&cli.StringFlag{
			Name:  "config",
			Usage: "config file (config.toml)",
		},
		&cli.StringFlag{
			Name:  "storage-config",
			Usage: "storage paths config (storage.json)",
		},
	},
	ArgsUsage: "[backupFile]",
	Action: func(cctx *cli.Context) error {
		ctx := lcli.ReqContext(cctx)
		log.Info("Initializing lotus miner using a backup")

		var storageCfg *storiface.StorageConfig
		if cctx.IsSet("storage-config") {
			cf, err := homedir.Expand(cctx.String("storage-config"))
			if err != nil {
				return xerrors.Errorf("expanding storage config path: %w", err)
			}

			cfb, err := os.ReadFile(cf)
			if err != nil {
				return xerrors.Errorf("reading storage config: %w", err)
			}

			storageCfg = &storiface.StorageConfig{}
			err = json.Unmarshal(cfb, storageCfg)
			if err != nil {
				return xerrors.Errorf("cannot unmarshal json for storage config: %w", err)
			}
		}

		repoPath := cctx.String(FlagMinerRepo)

		if err := restore(ctx, cctx, repoPath, storageCfg, nil, func(api lapi.FullNode, maddr address.Address, peerid peer.ID, mi api.MinerInfo) error {
			log.Info("Checking proof parameters")

			if err := paramfetch.GetParams(ctx, build.ParametersJSON(), build.SrsJSON(), uint64(mi.SectorSize)); err != nil {
				return xerrors.Errorf("fetching proof parameters: %w", err)
			}

			log.Info("Configuring miner actor")

			if err := configureStorageMiner(ctx, api, maddr, peerid, big.Zero()); err != nil {
				return err
			}

			return nil
		}); err != nil {
			return err
		}

		return nil
	},
}

func restore(ctx context.Context, cctx *cli.Context, targetPath string, strConfig *storiface.StorageConfig, manageConfig func(*config.StorageMiner) error, after func(api lapi.FullNode, addr address.Address, peerid peer.ID, mi api.MinerInfo) error) error {
	if cctx.NArg() != 1 {
		return lcli.IncorrectNumArgs(cctx)
	}

	log.Info("Trying to connect to full node RPC")

	api, closer, err := lcli.GetFullNodeAPIV1(cctx) // TODO: consider storing full node address in config
	if err != nil {
		return err
	}
	defer closer()

	log.Info("Checking full node version")

	v, err := api.Version(ctx)
	if err != nil {
		return err
	}

	if !v.APIVersion.EqMajorMinor(lapi.FullAPIVersion1) {
		return xerrors.Errorf("Remote API version didn't match (expected %s, remote %s)", lapi.FullAPIVersion1, v.APIVersion)
	}

	if !cctx.Bool("nosync") {
		if err := lcli.SyncWait(ctx, &v0api.WrapperV1Full{FullNode: api}, false); err != nil {
			return xerrors.Errorf("sync wait: %w", err)
		}
	}

	bf, err := homedir.Expand(cctx.Args().First())
	if err != nil {
		return xerrors.Errorf("expand backup file path: %w", err)
	}

	st, err := os.Stat(bf)
	if err != nil {
		return xerrors.Errorf("stat backup file (%s): %w", bf, err)
	}

	f, err := os.Open(bf)
	if err != nil {
		return xerrors.Errorf("opening backup file: %w", err)
	}
	defer f.Close() // nolint:errcheck

	log.Info("Checking if repo exists")

	r, err := repo.NewFS(targetPath)
	if err != nil {
		return err
	}

	ok, err := r.Exists()
	if err != nil {
		return err
	}
	if ok {
		return xerrors.Errorf("repo at '%s' is already initialized", cctx.String(FlagMinerRepo))
	}

	log.Info("Initializing repo")

	if err := r.Init(repo.StorageMiner); err != nil {
		return err
	}

	lr, err := r.Lock(repo.StorageMiner)
	if err != nil {
		return err
	}
	defer lr.Close() //nolint:errcheck

	if cctx.IsSet("config") {
		log.Info("Restoring config")

		cf, err := homedir.Expand(cctx.String("config"))
		if err != nil {
			return xerrors.Errorf("expanding config path: %w", err)
		}

		_, err = os.Stat(cf)
		if err != nil {
			return xerrors.Errorf("stat config file (%s): %w", cf, err)
		}

		var cerr error
		err = lr.SetConfig(func(raw interface{}) {
			rcfg, ok := raw.(*config.StorageMiner)
			if !ok {
				cerr = xerrors.New("expected miner config")
				return
			}

			ff, err := config.FromFile(cf, config.SetDefault(func() (interface{}, error) { return rcfg, nil }))
			if err != nil {
				cerr = xerrors.Errorf("loading config: %w", err)
				return
			}

			*rcfg = *ff.(*config.StorageMiner)
			if manageConfig != nil {
				cerr = manageConfig(rcfg)
			}
		})
		if cerr != nil {
			return cerr
		}
		if err != nil {
			return xerrors.Errorf("setting config: %w", err)
		}

	} else {
		log.Warn("--config NOT SET, WILL USE DEFAULT VALUES")
	}

	if strConfig != nil {
		log.Info("Restoring storage path config")

		err = lr.SetStorage(func(scfg *storiface.StorageConfig) {
			*scfg = *strConfig
		})
		if err != nil {
			return xerrors.Errorf("setting storage config: %w", err)
		}
	} else {
		log.Warn("--storage-config NOT SET. NO SECTOR PATHS WILL BE CONFIGURED")
		// setting empty config to allow miner to be started
		if err := lr.SetStorage(func(sc *storiface.StorageConfig) {
			sc.StoragePaths = append(sc.StoragePaths, storiface.LocalPath{})
		}); err != nil {
			return xerrors.Errorf("set storage config: %w", err)
		}
	}

	log.Info("Restoring metadata backup")

	mds, err := lr.Datastore(ctx, "/metadata")
	if err != nil {
		return err
	}

	bar := pb.New64(st.Size())
	br := bar.NewProxyReader(f)
	bar.ShowTimeLeft = true
	bar.ShowPercent = true
	bar.ShowSpeed = true
	bar.Units = pb.U_BYTES

	bar.Start()
	err = backupds.RestoreInto(br, mds)
	bar.Finish()

	if err != nil {
		return xerrors.Errorf("restoring metadata: %w", err)
	}

	log.Info("Checking actor metadata")

	abytes, err := mds.Get(ctx, datastore.NewKey("miner-address"))
	if err != nil {
		return xerrors.Errorf("getting actor address from metadata datastore: %w", err)
	}

	maddr, err := address.NewFromBytes(abytes)
	if err != nil {
		return xerrors.Errorf("parsing actor address: %w", err)
	}

	log.Info("ACTOR ADDRESS: ", maddr.String())

	mi, err := api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("getting miner info: %w", err)
	}

	log.Info("SECTOR SIZE: ", units.BytesSize(float64(mi.SectorSize)))

	wk, err := api.StateAccountKey(ctx, mi.Worker, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("resolving worker key: %w", err)
	}

	has, err := api.WalletHas(ctx, wk)
	if err != nil {
		return xerrors.Errorf("checking worker address: %w", err)
	}

	if !has {
		return xerrors.Errorf("worker address %s for miner actor %s not present in full node wallet", mi.Worker, maddr)
	}

	log.Info("Initializing libp2p identity")

	p2pSk, err := makeHostKey(lr)
	if err != nil {
		return xerrors.Errorf("make host key: %w", err)
	}

	peerid, err := peer.IDFromPrivateKey(p2pSk)
	if err != nil {
		return xerrors.Errorf("peer ID from private key: %w", err)
	}

	return after(api, maddr, peerid, mi)
}

var restoreMinerUsingChainInfoCmd = &cli.Command{
	Name:      "restore-from-chain",
	Usage:     "Initialize a lotus miner repo using on-chain info",
	UsageText: "lotus-miner restore-from-chain [arguments...]\n\nExample: \n  lotus-miner init restore-from-chain --nosync --skip-p2p-publish f01004",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "nosync",
			Usage: "don't check full-node sync status",
		},
		&cli.StringFlag{
			Name:  "config",
			Usage: "config file (config.toml)",
		},
		&cli.StringFlag{
			Name:  "storage-config",
			Usage: "storage paths config (storage.json)",
		},
		&cli.BoolFlag{
			Name:     "skip-p2p-publish",
			Usage:    "If set to true, new p2p address won't be published on chain",
			Value:    false,
			Required: false,
		},
	},
	ArgsUsage: "[newRepoPath]",
	Action: func(cctx *cli.Context) error {
		ctx := lcli.ReqContext(cctx)
		log.Info("Initializing lotus miner using on-chain info")

		var storageCfg *storiface.StorageConfig
		if cctx.IsSet("storage-config") {
			cf, err := homedir.Expand(cctx.String("storage-config"))
			if err != nil {
				return xerrors.Errorf("expanding storage config path: %w", err)
			}

			cfb, err := os.ReadFile(cf)
			if err != nil {
				return xerrors.Errorf("reading storage config: %w", err)
			}

			storageCfg = &storiface.StorageConfig{}
			err = json.Unmarshal(cfb, storageCfg)
			if err != nil {
				return xerrors.Errorf("cannot unmarshal json for storage config: %w", err)
			}
		} else {
			log.Info("A new storage configuration will be initialized using default values;")
		}

		repoPath := cctx.String(FlagMinerRepo)

		if err := restoreFromChain(ctx, cctx, repoPath, storageCfg, nil, func(api lapi.FullNode, maddr address.Address, peerid peer.ID, mi api.MinerInfo) error {
			skipP2pPublish := cctx.Bool("skip-p2p-publish")

			if !skipP2pPublish {
				log.Info("Repo initialization complete. Doing final checks before you can start the miner..")
				log.Info("Checking proof parameters..")
				if err := paramfetch.GetParams(ctx, build.ParametersJSON(), build.SrsJSON(), uint64(mi.SectorSize)); err != nil {
					return xerrors.Errorf("fetching proof parameters: %w", err)
				}

				log.Infof("Configuring miner actor - publishing new p2p address (%v)...", peerid)

				if err := configureStorageMiner(ctx, api, maddr, peerid, big.Zero()); err != nil {
					return err
				}
			} else {
				log.Info("Repo initialization complete. Publishing p2p ID skipped;")
			}

			return nil
		}); err != nil {
			log.Errorf("Error initializing node: %s", err)
			return err
		}

		log.Infof("Repo initialization complete; Storage miner repo '%s' is ready to be used.", repoPath)
		log.Info("Sector metada is not restored yet. You can eather restore it from another repository or re-import sectors.")
		return nil
	},
}

func restoreFromChain(ctx context.Context, cctx *cli.Context, targetPath string, strConfig *storiface.StorageConfig, manageConfig func(*config.StorageMiner) error, after func(api lapi.FullNode, addr address.Address, peerid peer.ID, mi api.MinerInfo) error) error {
	if cctx.NArg() != 1 {
		log.Error("Miner ID should be passed as argument; E.g. f01004")
		return lcli.IncorrectNumArgs(cctx)
	}

	minerRawAddress := cctx.Args().First()

	addr, err := address.NewFromString(minerRawAddress)
	if err != nil {
		return xerrors.Errorf("error parsing miner address: failed parsing actor flag value (%q): %w", minerRawAddress, err)
	}

	log.Info("Trying to connect to full node RPC")

	api, closer, err := lcli.GetFullNodeAPIV1(cctx) // TODO: consider storing full node address in config
	if err != nil {
		return err
	}
	defer closer()

	log.Info("Checking full node version...")

	// Wrap the context with a timeout
	vestionCtxWithTimeout, cancelVestionCtxWithTimeout := context.WithTimeout(ctx, 30*time.Second)
	defer cancelVestionCtxWithTimeout()

	v, err := api.Version(vestionCtxWithTimeout)
	if err != nil {
		log.Errorf("error checking full node version: %w\n", err)
		return err
	}

	log.Infof("Checking full node version: %s\n", v.Version)

	if !v.APIVersion.EqMajorMinor(lapi.FullAPIVersion1) {
		return xerrors.Errorf("Remote API version didn't match (expected %s, remote %s)", lapi.FullAPIVersion1, v.APIVersion)
	}

	if !cctx.Bool("nosync") {
		log.Infof("Checking full node sync status...\n")
		if err := lcli.SyncWait(ctx, &v0api.WrapperV1Full{FullNode: api}, false); err != nil {
			return xerrors.Errorf("sync wait: %w", err)
		}
	}

	log.Info("Checking if repo exists...")

	r, err := repo.NewFS(targetPath)
	if err != nil {
		return fmt.Errorf("error initializing new repo in path: \"%v\"; Error: %w", targetPath, err)
	}

	ok, err := r.Exists()
	if err != nil {
		return err
	}
	if ok {
		return xerrors.Errorf("repo at '%s' is already initialized", cctx.String(FlagMinerRepo))
	}

	log.Infof("Initializing new repo in path: %v\n", targetPath)

	if err := r.Init(repo.StorageMiner); err != nil {
		log.Errorf("error initializing new repo in path: \"%v\"; Error: %w", targetPath, err)
		return err
	}

	lr, err := r.Lock(repo.StorageMiner)
	if err != nil {
		return err
	}
	defer lr.Close() //nolint:errcheck

	if cctx.IsSet("config") {
		log.Info("Restoring config")

		cf, err := homedir.Expand(cctx.String("config"))
		if err != nil {
			return xerrors.Errorf("expanding config path: %w", err)
		}

		_, err = os.Stat(cf)
		if err != nil {
			return xerrors.Errorf("stat config file (%s): %w", cf, err)
		}

		var cerr error
		err = lr.SetConfig(func(raw interface{}) {
			rcfg, ok := raw.(*config.StorageMiner)
			if !ok {
				cerr = xerrors.New("expected miner config")
				return
			}

			ff, err := config.FromFile(cf, config.SetDefault(func() (interface{}, error) { return rcfg, nil }))
			if err != nil {
				cerr = xerrors.Errorf("loading config: %w", err)
				return
			}

			*rcfg = *ff.(*config.StorageMiner)
			if manageConfig != nil {
				cerr = manageConfig(rcfg)
			}
		})
		if cerr != nil {
			return cerr
		}
		if err != nil {
			return xerrors.Errorf("setting config: %w", err)
		}

	} else {
		log.Warn("--config NOT SET, WILL USE DEFAULT VALUES")
	}

	if strConfig != nil {
		log.Info("Restoring storage path config")

		err = lr.SetStorage(func(scfg *storiface.StorageConfig) {
			*scfg = *strConfig
		})
		if err != nil {
			return xerrors.Errorf("setting storage config: %w", err)
		}
	} else {
		log.Warn("--storage-config NOT SET. LOCAL PATHS WILL BE CONFIGURED")

		b, err := json.MarshalIndent(&storiface.LocalStorageMeta{
			ID:       storiface.ID(uuid.New().String()),
			Weight:   10,
			CanSeal:  true,
			CanStore: true,
		}, "", "  ")
		if err != nil {
			return xerrors.Errorf("marshaling storage config: %w", err)
		}

		if err := os.WriteFile(filepath.Join(lr.Path(), "sectorstore.json"), b, 0644); err != nil {
			return xerrors.Errorf("persisting storage metadata (%s): %w", filepath.Join(lr.Path(), "sectorstore.json"), err)
		}

		var localPaths []storiface.LocalPath

		localPaths = append(localPaths, storiface.LocalPath{
			Path: lr.Path(),
		})

		// setting empty config to allow miner to be started
		if err := lr.SetStorage(func(sc *storiface.StorageConfig) {
			sc.StoragePaths = append(sc.StoragePaths, localPaths...)
		}); err != nil {
			return xerrors.Errorf("set storage config: %w", err)
		}

	}

	log.Info("Restoring metadata backup")

	mds, err := lr.Datastore(ctx, "/metadata")
	if err != nil {
		return err
	}

	addrBytes := addr.Bytes()

	if err := mds.Put(ctx, datastore.NewKey("miner-address"), addrBytes); err != nil {
		return err
	}

	log.Info("Checking actor metadata...")

	abytes, err := mds.Get(ctx, datastore.NewKey("miner-address"))
	if err != nil {
		return xerrors.Errorf("getting actor address from metadata datastore: %w", err)
	}

	maddr, err := address.NewFromBytes(abytes)
	if err != nil {
		return xerrors.Errorf("parsing actor address: %w", err)
	}

	log.Info("Set actor address as: ", maddr.String())

	mi, err := api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("getting miner info: %w", err)
	}

	log.Info("Sector size: ", units.BytesSize(float64(mi.SectorSize)))

	wk, err := api.StateAccountKey(ctx, mi.Worker, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("resolving worker key: %w", err)
	}

	has, err := api.WalletHas(ctx, wk)
	if err != nil {
		return xerrors.Errorf("checking worker address: %w", err)
	}

	if !has {
		return xerrors.Errorf("worker address %s for miner actor %s not present in full node wallet", mi.Worker, maddr)
	}

	log.Info("Initializing libp2p identity")

	p2pSk, err := makeHostKey(lr)
	if err != nil {
		return xerrors.Errorf("make host key: %w", err)
	}

	peerid, err := peer.IDFromPrivateKey(p2pSk)
	if err != nil {
		return xerrors.Errorf("peer ID from private key: %w", err)
	}
	log.Infof("New peer ID: %s\n", peerid)
	return after(api, maddr, peerid, mi)
}

var copySectorMetadataCmd = &cli.Command{
	Name:  "copy-sector-metadata",
	Usage: "Copy sector metadata from one miner repo to another",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "source-repo",
			Usage: "Path to source repo",
		},
		&cli.BoolFlag{
			Name:     "write",
			Usage:    "Write entries to the target repo. If false, only print the keys",
			Value:    true,
			Required: false,
		},
		&cli.StringFlag{
			Name:     "get-enc",
			Usage:    "print values [esc/hex/cbor]",
			Value:    "esc",
			Required: false,
		},
	},
	ArgsUsage: "[newRepoPath]",
	Action: func(cctx *cli.Context) error {
		ctx := lcli.ReqContext(cctx)

		sourceRepoPath := cctx.String("source-repo")
		targrepetPath := cctx.String(FlagMinerRepo)

		log.Info("Coping sectors info from one metadata repo to another; \nSource: %v \nTarget: %v \n", sourceRepoPath, targrepetPath)

		sourceRepo, err := repo.NewFS(sourceRepoPath)
		if err != nil {
			return err
		}

		lockedSourceRepo, err := sourceRepo.Lock(repo.StorageMiner)
		if err != nil {
			return err
		}
		defer lockedSourceRepo.Close() //nolint:errcheck

		dsSource, err := lockedSourceRepo.Datastore(context.Background(), datastore.NewKey("/metadata").String())
		if err != nil {
			return err
		}

		genc := cctx.String("get-enc")

		q, err := dsSource.Query(context.Background(), dsq.Query{
			Prefix:   datastore.NewKey("/sectors").String(),
			KeysOnly: genc == "",
		})

		if err != nil {
			return xerrors.Errorf("datastore query: %w", err)
		}
		defer q.Close() //nolint:errcheck

		targetRepo, err := repo.NewFS(targrepetPath)
		if err != nil {
			return err
		}

		lockedTargetRepo, err := targetRepo.Lock(repo.StorageMiner)
		if err != nil {
			return err
		}
		defer lockedTargetRepo.Close() //nolint:errcheck

		targetMetadata, err := lockedTargetRepo.Datastore(ctx, "/metadata")
		if err != nil {
			return err
		}

		totalSectorCount := 0

		writeChanges := cctx.Bool("write")
		for res := range q.Next() {
			if writeChanges {
				log.Infof("Coping metadata key: %v \n", res.Key)
				if err := targetMetadata.Put(ctx, datastore.NewKey(res.Key), res.Value); err != nil {
					log.Errorf("Error coping metadata key: %v \n", res.Key)
					return err
				}
			} else {
				log.Infof("Found metadata key: %v \n", res.Key)
			}

			totalSectorCount++
		}

		log.Infof("Done; Copied total: %d; Write: %v; \n", totalSectorCount, writeChanges)

		return nil
	},
}
