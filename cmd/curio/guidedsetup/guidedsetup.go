// guidedSetup for migration from lotus-miner to Curio
//
//	IF STRINGS CHANGED {
//			follow instructions at ../internal/translations/translations.go
//	}
package guidedsetup

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/charmbracelet/lipgloss"
	"github.com/docker/go-units"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/manifoldco/promptui"
	"github.com/mitchellh/go-homedir"
	"github.com/samber/lo"
	"github.com/snadrus/must"
	"github.com/urfave/cli/v2"
	"golang.org/x/text/message"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/cmd/curio/internal/translations"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/createminer"
	"github.com/filecoin-project/curio/lib/storiface"

	lapi "github.com/filecoin-project/lotus/api"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/filecoin-project/lotus/node/repo"
)

// URL to upload user-selected fields to help direct developer's focus.
const DeveloperFocusRequestURL = "https://curiostorage.org/cgi-bin/savedata.php"

var GuidedsetupCmd = &cli.Command{
	Name:  "guided-setup",
	Usage: "Run the guided setup for migrating from lotus-miner to Curio or Creating a new Curio miner",
	Flags: []cli.Flag{
		&cli.StringFlag{ // for cliutil.GetFullNodeAPI
			Name:    "repo",
			EnvVars: []string{"LOTUS_PATH"},
			Hidden:  true,
			Value:   "~/.lotus",
		},
	},
	Action: func(cctx *cli.Context) (err error) {
		T, say := translations.SetupLanguage()
		setupCtrlC(say)

		// Run the migration steps
		migrationData := MigrationData{
			T:   T,
			say: say,
			selectTemplates: &promptui.SelectTemplates{
				Help: T("Use the arrow keys to navigate: ↓ ↑ → ← "),
			},
			cctx: cctx,
			ctx:  cctx.Context,
		}

		newOrMigrate(&migrationData)
		if migrationData.init {
			say(header, "This interactive tool creates a new miner actor and creates the basic configuration layer for it.")
			say(notice, "This process is partially idempotent. Once a new miner actor has been created and subsequent steps fail, the user need to run 'curio config new-cluster < miner ID >' to finish the configuration.")
			for _, step := range newMinerSteps {
				step(&migrationData)
			}
		} else if migrationData.nonSP {
			say(header, "This interactive tool sets up a non-Storage Provider cluster for protocols like PDP, Snark market, and others.")
			say(notice, "This setup does not create or migrate a Filecoin SP actor.")
			for _, step := range nonSPSteps {
				step(&migrationData)
			}
		} else {
			say(header, "This interactive tool migrates lotus-miner to Curio in 5 minutes.")
			say(notice, "Each step needs your confirmation and can be reversed. Press Ctrl+C to exit at any time.")

			for _, step := range migrationSteps {
				step(&migrationData)
			}
		}

		// Optional steps
		optionalSteps(&migrationData)

		for _, closer := range migrationData.closers {
			closer()
		}
		return nil
	},
}

func setupCtrlC(say func(style lipgloss.Style, key message.Reference, a ...interface{})) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		say(notice, "Ctrl+C pressed in Terminal")
		os.Exit(2)
	}()
}

var (
	header = lipgloss.NewStyle().
		Align(lipgloss.Left).
		Foreground(lipgloss.Color("#00FF00")).
		Background(lipgloss.Color("#242424")).
		BorderStyle(lipgloss.NormalBorder()).
		Width(60).Margin(1)

	notice = lipgloss.NewStyle().
		Align(lipgloss.Left).
		Bold(true).
		Foreground(lipgloss.Color("#CCCCCC")).
		Background(lipgloss.Color("#333300")).MarginBottom(1)

	green = lipgloss.NewStyle().
		Align(lipgloss.Left).
		Foreground(lipgloss.Color("#00FF00")).
		Background(lipgloss.Color("#000000"))

	plain = lipgloss.NewStyle().Align(lipgloss.Left)

	section = lipgloss.NewStyle().
		Align(lipgloss.Left).
		Foreground(lipgloss.Color("#000000")).
		Background(lipgloss.Color("#FFFFFF")).
		Underline(true)

	code = lipgloss.NewStyle().
		Align(lipgloss.Left).
		Foreground(lipgloss.Color("#00FF00")).
		Background(lipgloss.Color("#f8f9fa"))
)

func newOrMigrate(d *MigrationData) {
	i, _, err := (&promptui.Select{
		Label: d.T("I want to:"),
		Items: []string{
			d.T("Migrate from existing Lotus-Miner"),
			d.T("Create a new miner"),
			d.T("Setup non-Storage Provider cluster")},
		Templates: d.selectTemplates,
	}).Run()
	if err != nil {
		d.say(notice, "Aborting remaining steps.", err.Error())
		os.Exit(1)
	}
	switch i {
	case 1:
		d.init = true
	case 2:
		d.nonSP = true
	}
}

type migrationStep func(*MigrationData)

var migrationSteps = []migrationStep{
	readMinerConfig, // Tells them to be on the miner machine
	yugabyteConnect, // Miner is updated
	configToDB,      // work on base configuration migration.
	doc,
	complete,
	afterRan,
}

type newMinerStep func(data *MigrationData)

var newMinerSteps = []newMinerStep{
	stepPresteps,
	stepCreateActor,
	stepNewMinerConfig,
	doc,
	completeInit,
	afterRan,
}

type nonSPStep func(data *MigrationData)

var nonSPSteps = []nonSPStep{
	stepPresteps,
	stepNewMinerConfig,
	doc,
	completeNonSP,
	afterRan,
}

type MigrationData struct {
	T               func(key message.Reference, a ...interface{}) string
	say             func(style lipgloss.Style, key message.Reference, a ...interface{})
	selectTemplates *promptui.SelectTemplates
	MinerConfigPath string
	DB              *harmonydb.DB
	HarmonyCfg      harmonydb.Config
	MinerID         address.Address
	full            api.Chain
	cctx            *cli.Context
	closers         []jsonrpc.ClientCloser
	ctx             context.Context
	owner           address.Address
	worker          address.Address
	sender          address.Address
	ssize           string
	init            bool
	nonSP           bool
}

func complete(d *MigrationData) {
	stepCompleted(d, d.T("Lotus-Miner to Curio Migration."))
}

func afterRan(d *MigrationData) {
	// Write curio.env file for user's reference
	places := []string{"/tmp/curio.env",
		must.One(os.Getwd()) + "/curio.env",
		must.One(os.UserHomeDir()) + "/curio.env"}
saveConfigFile:
	_, where, err := (&promptui.Select{
		Label:     d.T("Where should we save your database config file?"),
		Items:     places,
		Templates: d.selectTemplates,
	}).Run()
	if err != nil {
		d.say(notice, "Aborting migration.", err.Error())
		os.Exit(1)
	}

	lines := []string{
		fmt.Sprintf("export CURIO_DB_HOST=%s", strings.Join(d.HarmonyCfg.Hosts, ",")),
		fmt.Sprintf("export CURIO_DB_USER=%s", d.HarmonyCfg.Username),
		fmt.Sprintf("export CURIO_DB_PASSWORD=%s", d.HarmonyCfg.Password),
		fmt.Sprintf("export CURIO_DB_PORT=%s", d.HarmonyCfg.Port),
		fmt.Sprintf("export CURIO_DB_NAME=%s", d.HarmonyCfg.Database),
	}

	// Write the file
	err = os.WriteFile(where, []byte(strings.Join(lines, "\n")), 0644)
	if err != nil {
		d.say(notice, "Error writing file: %s", err.Error())
		goto saveConfigFile
	}

	d.say(plain, "Try the web interface with %s ", code.Render("curio run --layers=gui"))
	d.say(plain, "For more servers, make /etc/curio.env with the curio.env database env and add the CURIO_LAYERS env to assign purposes.")
	d.say(plain, "You can now migrate your market node (%s), if applicable.", "Boost")
	d.say(plain, "Additional info is at http://docs.curiostorage.org")
}

func completeInit(d *MigrationData) {
	stepCompleted(d, d.T("New Miner initialization complete."))
}

func configToDB(d *MigrationData) {
	d.say(section, "Migrating lotus-miner config.toml to Curio in-database configuration.")

	{
		var closer jsonrpc.ClientCloser
		var err error
		d.full, closer, err = cliutil.GetFullNodeAPIV1(d.cctx)
		d.closers = append(d.closers, closer)
		if err != nil {
			d.say(notice, "Error getting API: %s", err.Error())
			os.Exit(1)
		}
	}
	ainfo, err := cliutil.GetAPIInfo(d.cctx, repo.FullNode)
	if err != nil {
		d.say(notice, "could not get API info for FullNode: %w", err)
		os.Exit(1)
	}
	token, err := d.full.AuthNew(context.Background(), lapi.AllPermissions)
	if err != nil {
		d.say(notice, "Error getting token: %s", err.Error())
		os.Exit(1)
	}

	chainApiInfo := fmt.Sprintf("%s:%s", string(token), ainfo.Addr)

	shouldErrPrompt := func() bool {
		i, _, err := (&promptui.Select{
			Label: d.T("Unmigratable sectors found. Do you want to continue?"),
			Items: []string{
				d.T("Yes, continue"),
				d.T("No, abort")},
			Templates: d.selectTemplates,
		}).Run()
		if err != nil {
			d.say(notice, "Aborting migration.", err.Error())
			os.Exit(1)
		}
		return i == 1
	}

	d.MinerID, err = SaveConfigToLayerMigrateSectors(d.DB, d.MinerConfigPath, chainApiInfo, shouldErrPrompt)
	if err != nil {
		d.say(notice, "Error saving config to layer: %s. Aborting Migration", err.Error())
		os.Exit(1)
	}
}

func doc(d *MigrationData) {
	d.say(plain, "Documentation: ")
	d.say(plain, "The '%s' layer stores common configuration. All curio instances can include it in their %s argument.", "base", "--layers")
	d.say(plain, "You can add other layers for per-machine configuration changes.")

	d.say(plain, "Filecoin %s channels: %s and %s", "Slack", "#fil-curio-help", "#fil-curio-dev")

	d.say(plain, "Increase reliability using redundancy: start multiple machines with at-least the post layer: 'curio run --layers=post'")
	//d.say(plain, "Point your browser to your web GUI to complete setup with %s and advanced featues.", "Boost")
	d.say(plain, "One database can serve multiple miner IDs: Run a migration for each lotus-miner.")
}

func yugabyteConnect(d *MigrationData) {

	getDBDetails(d)

	d.say(plain, "Connected to Yugabyte. Schema is current.")
	stepCompleted(d, d.T("Connected to Yugabyte"))
}

func readMinerConfig(d *MigrationData) {
	d.say(plain, "To start, ensure your sealing pipeline is drained and shut-down lotus-miner.")

	verifyPath := func(dir string) error {
		dir, err := homedir.Expand(dir)
		if err != nil {
			return err
		}
		_, err = os.Stat(path.Join(dir, "config.toml"))
		return err
	}

	dirs := map[string]struct{}{"~/.lotusminer": {}, "~/.lotus-miner-local-net": {}}
	if v := os.Getenv("LOTUS_MINER_PATH"); v != "" {
		dirs[v] = struct{}{}
	}
	for dir := range dirs {
		err := verifyPath(dir)
		if err != nil {
			delete(dirs, dir)
		}
		dirs[dir] = struct{}{}
	}

	var otherPath bool
	if len(dirs) > 0 {
		_, str, err := (&promptui.Select{
			Label:     d.T("Select the location of your lotus-miner config directory?"),
			Items:     append(lo.Keys(dirs), d.T("Other")),
			Templates: d.selectTemplates,
		}).Run()
		if err != nil {
			if err.Error() == "^C" {
				os.Exit(1)
			}
			otherPath = true
		} else {
			if str == d.T("Other") {
				otherPath = true
			} else {
				d.MinerConfigPath = str
			}
		}
	}
	if otherPath {
	minerPathEntry:
		str, err := (&promptui.Prompt{
			Label: d.T("Enter the path to the configuration directory used by %s", "lotus-miner"),
		}).Run()
		if err != nil {
			d.say(notice, "No path provided, abandoning migration ")
			os.Exit(1)
		}
		err = verifyPath(str)
		if err != nil {
			d.say(notice, "Cannot read the config.toml file in the provided directory, Error: %s", err.Error())
			goto minerPathEntry
		}
		d.MinerConfigPath = str
	}

	// Try to lock Miner repo to verify that lotus-miner is not running
	{
		r, err := repo.NewFS(d.MinerConfigPath)
		if err != nil {
			d.say(plain, "Could not create repo from directory: %s. Aborting migration", err.Error())
			os.Exit(1)
		}
		lr, err := r.Lock(StorageMiner)
		if err != nil {
			d.say(plain, "Could not lock miner repo. Your miner must be stopped: %s\n Aborting migration", err.Error())
			os.Exit(1)
		}
		_ = lr.Close()
	}

	stepCompleted(d, d.T("Read Miner Config"))
}
func stepCompleted(d *MigrationData, step string) {
	fmt.Print(green.Render("✔ "))
	d.say(plain, "Step Complete: %s\n", step)
}

func stepCreateActor(d *MigrationData) {
	d.say(plain, "Initializing a new miner actor.")

	d.ssize = "32 GiB"
	for {
		i, _, err := (&promptui.Select{
			Label: d.T("Enter the info to create a new miner"),
			Items: []string{
				d.T("Owner Wallet: %s", d.owner.String()),
				d.T("Worker Wallet: %s", d.worker.String()),
				d.T("Sender Wallet: %s", d.sender.String()),
				d.T("Sector Size: %s", d.ssize),
				d.T("Continue to verify the addresses and create a new miner actor.")},
			Size:      6,
			Templates: d.selectTemplates,
		}).Run()
		if err != nil {
			d.say(notice, "Miner creation error occurred: %s ", err.Error())
			os.Exit(1)
		}
		switch i {
		case 0:
			owner, err := (&promptui.Prompt{
				Label: d.T("Enter the owner address"),
			}).Run()
			if err != nil {
				d.say(notice, "No address provided")
				continue
			}
			ownerAddr, err := address.NewFromString(owner)
			if err != nil {
				d.say(notice, "Failed to parse the address: %s", err.Error())
			}
			d.owner = ownerAddr
		case 1, 2:
			val, err := (&promptui.Prompt{
				Label:   d.T("Enter %s address", []string{"worker", "sender"}[i-1]),
				Default: d.owner.String(),
			}).Run()
			if err != nil {
				d.say(notice, err.Error())
				continue
			}
			addr, err := address.NewFromString(val)
			if err != nil {
				d.say(notice, "Failed to parse the address: %s", err.Error())
			}
			switch i {
			case 1:
				d.worker = addr
			case 2:
				d.sender = addr
			}
			continue
		case 3:
			i, _, err := (&promptui.Select{
				Label: d.T("Select the Sector Size"),
				Items: []string{
					d.T("64 GiB"),
					d.T("32 GiB (recommended for mainnet)"),
					d.T("8 MiB"),
					d.T("2 KiB"),
				},
				Size:      4,
				Templates: d.selectTemplates,
			}).Run()
			if err != nil {
				d.say(notice, "Sector selection failed: %s ", err.Error())
				os.Exit(1)
			}

			d.ssize = []string{"64 GiB", "32 GiB", "8 MiB", "2 KiB"}[i]
			continue
		case 4:
			goto minerInit // break out of the for loop once we have all the values
		}
	}

minerInit:
	sectorSize, err := units.RAMInBytes(d.ssize)
	if err != nil {
		d.say(notice, "Failed to parse sector size: %s", err.Error())
		os.Exit(1)
	}
	ss := abi.SectorSize(sectorSize)

	miner, err := createminer.CreateStorageMiner(d.ctx, d.full, d.owner, d.worker, d.sender, ss, CONFIDENCE)
	if err != nil {
		d.say(notice, "Failed to create the miner actor: %s", err.Error())
		os.Exit(1)
	}

	d.MinerID = miner
	stepCompleted(d, d.T("Miner %s created successfully", miner.String()))
}

const CONFIDENCE = 5

func stepPresteps(d *MigrationData) {

	// Setup and connect to YugabyteDB
	getDBDetails(d)

	// Verify HarmonyDB connection
	var titles []string
	err := d.DB.Select(d.ctx, &titles, `SELECT title FROM harmony_config WHERE LENGTH(config) > 0`)
	if err != nil {
		d.say(notice, "Cannot reach the DB: %s", err.Error())
		os.Exit(1)
	}

	// Get full node API
	full, closer, err := cliutil.GetFullNodeAPIV1(d.cctx)
	if err != nil {
		d.say(notice, "Error connecting to full node API: %s", err.Error())
		os.Exit(1)
	}
	d.full = full
	d.closers = append(d.closers, closer)
	stepCompleted(d, d.T("Pre-initialization steps complete"))
}

func stepNewMinerConfig(d *MigrationData) {
	curioCfg := config.DefaultCurioConfig()

	// Only add miner address for SP setup
	if !d.nonSP {
		curioCfg.Addresses.Set([]config.CurioAddresses{{
			PreCommitControl:      []string{},
			CommitControl:         []string{},
			DealPublishControl:    []string{},
			TerminateControl:      []string{},
			DisableOwnerFallback:  false,
			DisableWorkerFallback: false,
			MinerAddresses:        []string{d.MinerID.String()},
			BalanceManager:        config.DefaultBalanceManager(),
		}})
	}

	sk, err := io.ReadAll(io.LimitReader(rand.Reader, 32))
	if err != nil {
		d.say(notice, "Failed to generate random bytes for secret: %s", err.Error())
		if !d.nonSP {
			d.say(notice, "Please do not run guided-setup again as miner creation is not idempotent. You need to run 'curio config new-cluster %s' to finish the configuration", d.MinerID.String())
		} else {
			d.say(notice, "Please do not run guided-setup again. You need to run 'curio config new-cluster' manually to finish the configuration")
		}
		os.Exit(1)
	}

	curioCfg.Apis.StorageRPCSecret = base64.StdEncoding.EncodeToString(sk)

	ainfo, err := cliutil.GetAPIInfo(d.cctx, repo.FullNode)
	if err != nil {
		d.say(notice, "Failed to get API info for FullNode: %s", err.Error())
		if !d.nonSP {
			d.say(notice, "Please do not run guided-setup again as miner creation is not idempotent. You need to run 'curio config new-cluster %s' to finish the configuration", d.MinerID.String())
		} else {
			d.say(notice, "Please do not run guided-setup again. You need to run 'curio config new-cluster' manually to finish the configuration")
		}
		os.Exit(1)
	}

	token, err := d.full.AuthNew(d.ctx, lapi.AllPermissions)
	if err != nil {
		d.say(notice, "Failed to create auth token: %s", err.Error())
		if !d.nonSP {
			d.say(notice, "Please do not run guided-setup again as miner creation is not idempotent. You need to run 'curio config new-cluster %s' to finish the configuration", d.MinerID.String())
		} else {
			d.say(notice, "Please do not run guided-setup again. You need to run 'curio config new-cluster' manually to finish the configuration")
		}
		os.Exit(1)
	}

	curioCfg.Apis.ChainApiInfo.Set(append(curioCfg.Apis.ChainApiInfo.Get(), fmt.Sprintf("%s:%s", string(token), ainfo.Addr)))

	// write config
	var titles []string
	err = d.DB.Select(d.ctx, &titles, `SELECT title FROM harmony_config WHERE LENGTH(config) > 0`)
	if err != nil {
		d.say(notice, "Cannot reach the DB: %s", err.Error())
		if !d.nonSP {
			d.say(notice, "Please do not run guided-setup again as miner creation is not idempotent. You need to run 'curio config new-cluster %s' to finish the configuration", d.MinerID.String())
		} else {
			d.say(notice, "Please do not run guided-setup again. You need to run 'curio config new-cluster' manually to finish the configuration")
		}
		os.Exit(1)
	}

	// If 'base' layer is not present
	if !lo.Contains(titles, "base") {
		if !d.nonSP {
			curioCfg.Addresses.Set(lo.Filter(curioCfg.Addresses.Get(), func(a config.CurioAddresses, _ int) bool {
				return len(a.MinerAddresses) > 0
			}))
		}
		cb, err := config.ConfigUpdate(curioCfg, config.DefaultCurioConfig(), config.Commented(true), config.DefaultKeepUncommented(), config.NoEnv())
		if err != nil {
			d.say(notice, "Failed to generate default config: %s", err.Error())
			if !d.nonSP {
				d.say(notice, "Please do not run guided-setup again as miner creation is not idempotent. You need to run 'curio config new-cluster %s' to finish the configuration", d.MinerID.String())
			} else {
				d.say(notice, "Please do not run guided-setup again. You need to run 'curio config new-cluster' manually to finish the configuration")
			}
			os.Exit(1)
		}
		_, err = d.DB.Exec(d.ctx, "INSERT INTO harmony_config (title, config) VALUES ('base', $1)", string(cb))
		if err != nil {
			d.say(notice, "Failed to insert config into database: %s", err.Error())
			if !d.nonSP {
				d.say(notice, "Please do not run guided-setup again as miner creation is not idempotent. You need to run 'curio config new-cluster %s' to finish the configuration", d.MinerID.String())
			} else {
				d.say(notice, "Please do not run guided-setup again. You need to run 'curio config new-cluster' manually to finish the configuration")
			}
			os.Exit(1)
		}
	}

	// For non-SP setup, we're done here
	if d.nonSP {
		d.say(green, "Non-SP cluster configuration created successfully")
		stepCompleted(d, d.T("Non-SP cluster configuration complete"))
		return
	}

	// For SP setup, continue with miner-specific configuration
	if !lo.Contains(titles, "base") {
		// We already created the base config above, so just show completion message
		stepCompleted(d, d.T("Configuration 'base' was updated to include this miner's address"))
		return
	}

	// If base layer is already present
	baseCfg := config.DefaultCurioConfig()
	var baseText string

	err = d.DB.QueryRow(d.ctx, "SELECT config FROM harmony_config WHERE title='base'").Scan(&baseText)
	if err != nil {
		d.say(notice, "Failed to load base config from database: %s", err.Error())
		d.say(notice, "Please do not run guided-setup again as miner creation is not idempotent. You need to run 'curio config new-cluster %s' to finish the configuration", d.MinerID.String())
		os.Exit(1)
	}
	_, err = deps.LoadConfigWithUpgrades(baseText, baseCfg)
	if err != nil {
		d.say(notice, "Failed to parse base config: %s", err.Error())
		d.say(notice, "Please do not run guided-setup again as miner creation is not idempotent. You need to run 'curio config new-cluster %s' to finish the configuration", d.MinerID.String())
		os.Exit(1)
	}

	baseCfg.Addresses.Set(append(baseCfg.Addresses.Get(), curioCfg.Addresses.Get()...))
	baseCfg.Addresses.Set(lo.Filter(baseCfg.Addresses.Get(), func(a config.CurioAddresses, _ int) bool {
		return len(a.MinerAddresses) > 0
	}))

	cb, err := config.ConfigUpdate(baseCfg, config.DefaultCurioConfig(), config.Commented(true), config.DefaultKeepUncommented(), config.NoEnv())
	if err != nil {
		d.say(notice, "Failed to regenerate base config: %s", err.Error())
		d.say(notice, "Please do not run guided-setup again as miner creation is not idempotent. You need to run 'curio config new-cluster %s' to finish the configuration", d.MinerID.String())
		os.Exit(1)
	}
	_, err = d.DB.Exec(d.ctx, "UPDATE harmony_config SET config=$1 WHERE title='base'", string(cb))
	if err != nil {
		d.say(notice, "Failed to insert 'base' config layer in database: %s", err.Error())
		d.say(notice, "Please do not run guided-setup again as miner creation is not idempotent. You need to run 'curio config new-cluster %s' to finish the configuration", d.MinerID.String())
		os.Exit(1)
	}

	stepCompleted(d, d.T("Configuration 'base' was updated to include this miner's address"))
}

func completeNonSP(d *MigrationData) {
	d.say(header, "Non-SP cluster setup complete!")
	d.say(plain, "Your non-SP cluster has been configured successfully.")
	d.say(plain, "You can now start using Curio for protocols like PDP, Snark markets, and others.")
	d.say(plain, "To start the cluster, run: curio run --layers basic-cluster")
}

func getDBDetails(d *MigrationData) {
	harmonyCfg := config.DefaultStorageMiner().HarmonyDB
	for {
		i, _, err := (&promptui.Select{
			Label: d.T("Enter the info to connect to your Yugabyte database installation (https://download.yugabyte.com/)"),
			Items: []string{
				d.T("Host: %s", strings.Join(harmonyCfg.Hosts, ",")),
				d.T("Port: %s", harmonyCfg.Port),
				d.T("Username: %s", harmonyCfg.Username),
				d.T("Password: %s", harmonyCfg.Password),
				d.T("Database: %s", harmonyCfg.Database),
				d.T("Continue to connect and update schema.")},
			Size:      6,
			Templates: d.selectTemplates,
		}).Run()
		if err != nil {
			d.say(notice, "Database config error occurred, abandoning migration: %s ", err.Error())
			os.Exit(1)
		}
		switch i {
		case 0:
			host, err := (&promptui.Prompt{
				Label: d.T("Enter the Yugabyte database host(s)"),
			}).Run()
			if err != nil {
				d.say(notice, "No host provided")
				continue
			}
			harmonyCfg.Hosts = strings.Split(host, ",")
		case 1, 2, 3, 4:
			val, err := (&promptui.Prompt{
				Label: d.T("Enter the Yugabyte database %s", []string{"port", "username", "password", "database"}[i-1]),
			}).Run()
			if err != nil {
				d.say(notice, "No value provided")
				continue
			}
			switch i {
			case 1:
				harmonyCfg.Port = val
			case 2:
				harmonyCfg.Username = val
			case 3:
				harmonyCfg.Password = val
			case 4:
				harmonyCfg.Database = val
			}
			continue
		case 5:
			db, err := harmonydb.NewFromConfig(harmonyCfg)
			if err != nil {
				if err.Error() == "^C" {
					os.Exit(1)
				}
				d.say(notice, "Error connecting to Yugabyte database: %s", err.Error())
				continue
			}
			d.DB = db
			d.HarmonyCfg = harmonyCfg
			return
		}
	}
}

func optionalSteps(d *MigrationData) {
	for {
		i, _, err := (&promptui.Select{
			Label: d.T("Optional setup steps (you can skip these and configure later):"),
			Items: []string{
				d.T("Skip optional steps"),
				d.T("Storage"),
				d.T("PDP")},
			Templates: d.selectTemplates,
		}).Run()
		if err != nil {
			if err.Error() == "^C" {
				os.Exit(1)
			}
			return
		}
		switch i {
		case 0:
			return
		case 1:
			optionalStorageStep(d)
		case 2:
			optionalPDPStep(d)
		}
	}
}

func optionalStorageStep(d *MigrationData) {
	d.say(header, "Storage Configuration")
	d.say(plain, "Manage storage paths for this server.")

	// Get storage.json path
	curioRepoPath := os.Getenv("CURIO_REPO_PATH")
	if curioRepoPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			d.say(notice, "Error getting home directory: %s", err.Error())
			return
		}
		curioRepoPath = path.Join(homeDir, ".curio")
	}
	storageJSONPath := path.Join(curioRepoPath, "storage.json")

	// Read existing storage paths
	localPaths := []string{}
	storageCfg, err := readStorageConfig(storageJSONPath)
	if err == nil {
		for _, p := range storageCfg.StoragePaths {
			localPaths = append(localPaths, p.Path)
		}
	}

	for {
		items := []string{d.T("Go Back")}
		items = append(items, d.T("Add new storage path"))
		for _, p := range localPaths {
			items = append(items, d.T("Delete %s", p))
		}

		i, _, err := (&promptui.Select{
			Label:     d.T("Storage paths for this server:"),
			Items:     items,
			Templates: d.selectTemplates,
		}).Run()
		if err != nil {
			if err.Error() == "^C" {
				os.Exit(1)
			}
			return
		}

		if i == 0 {
			return
		} else if i == 1 {
			// Add new storage path
			pathStr, err := (&promptui.Prompt{
				Label: d.T("Enter storage path to add"),
			}).Run()
			if err != nil {
				d.say(notice, "No path provided")
				continue
			}

			expandedPath, err := homedir.Expand(pathStr)
			if err != nil {
				d.say(notice, "Error expanding path: %s", err.Error())
				continue
			}

			// Check if path already exists
			exists := false
			for _, p := range localPaths {
				if p == expandedPath {
					exists = true
					break
				}
			}
			if exists {
				d.say(notice, "Path already exists")
				continue
			}

			// Ask for storage type
			storageType, _, err := (&promptui.Select{
				Label: d.T("Storage type for %s", expandedPath),
				Items: []string{
					d.T("Seal (fast storage for sealing operations)"),
					d.T("Store (long-term storage for sealed sectors)"),
					d.T("Both (seal and store)")},
				Templates: d.selectTemplates,
			}).Run()
			if err != nil {
				continue
			}

			// Add to storage.json
			if storageCfg == nil {
				storageCfg = &storiface.StorageConfig{StoragePaths: []storiface.LocalPath{}}
			}
			storageCfg.StoragePaths = append(storageCfg.StoragePaths, storiface.LocalPath{Path: expandedPath})
			localPaths = append(localPaths, expandedPath)

			// Write storage.json
			if err := writeStorageConfig(storageJSONPath, *storageCfg); err != nil {
				d.say(notice, "Error writing storage.json: %s", err.Error())
				continue
			}

			storageTypeStr := []string{"seal", "store", "both"}[storageType]
			d.say(plain, "Storage path %s added as %s. You'll need to initialize it with: curio cli storage attach --init --%s %s", expandedPath, storageTypeStr, storageTypeStr, expandedPath)
			stepCompleted(d, d.T("Storage path added"))
		} else {
			// Delete storage path
			pathToDelete := localPaths[i-2]
			i, _, err := (&promptui.Select{
				Label: d.T("Really delete %s?", pathToDelete),
				Items: []string{
					d.T("Yes, delete it"),
					d.T("No, keep it")},
				Templates: d.selectTemplates,
			}).Run()
			if err != nil || i == 1 {
				continue
			}

			// Remove from storage.json
			newPaths := []storiface.LocalPath{}
			for _, p := range storageCfg.StoragePaths {
				if p.Path != pathToDelete {
					newPaths = append(newPaths, p)
				}
			}
			storageCfg.StoragePaths = newPaths

			// Update localPaths list
			newLocalPaths := []string{}
			for _, p := range localPaths {
				if p != pathToDelete {
					newLocalPaths = append(newLocalPaths, p)
				}
			}
			localPaths = newLocalPaths

			// Write storage.json
			if err := writeStorageConfig(storageJSONPath, *storageCfg); err != nil {
				d.say(notice, "Error writing storage.json: %s", err.Error())
				continue
			}

			d.say(plain, "Storage path %s removed from configuration", pathToDelete)
			stepCompleted(d, d.T("Storage path deleted"))
		}
	}
}

func optionalPDPStep(d *MigrationData) {
	d.say(header, "PDP (Proof of Data Possession) Configuration")
	d.say(plain, "This will configure PDP settings for your Curio cluster.")
	d.say(plain, "For detailed documentation, see: https://docs.curiostorage.org/experimental-features/enable-pdp")

	// Check if PDP layer already exists
	var existingPDPConfig string
	err := d.DB.QueryRow(d.ctx, "SELECT config FROM harmony_config WHERE title='pdp'").Scan(&existingPDPConfig)
	hasPDPLayer := err == nil

	if hasPDPLayer {
		i, _, err := (&promptui.Select{
			Label: d.T("PDP layer already exists. What would you like to do?"),
			Items: []string{
				d.T("Reconfigure PDP"),
				d.T("Skip PDP setup")},
			Templates: d.selectTemplates,
		}).Run()
		if err != nil || i == 1 {
			return
		}
	}

	// Step 1: Configure PDP layer
	d.say(plain, "Creating PDP configuration layer...")

	curioCfg := config.DefaultCurioConfig()
	if hasPDPLayer {
		_, err = deps.LoadConfigWithUpgrades(existingPDPConfig, curioCfg)
		if err != nil {
			d.say(notice, "Error loading existing PDP config: %s", err.Error())
			return
		}
	}

	// Enable PDP subsystems
	curioCfg.Subsystems.EnablePDP = true
	curioCfg.Subsystems.EnableParkPiece = true
	curioCfg.Subsystems.EnableCommP = true
	curioCfg.Subsystems.EnableMoveStorage = true
	curioCfg.Subsystems.NoUnsealedDecode = true

	// Configure HTTP
	d.say(plain, "Configuring HTTP settings for PDP...")

	domain, err := (&promptui.Prompt{
		Label:   d.T("Enter your domain name (e.g., market.mydomain.com)"),
		Default: "market.mydomain.com",
	}).Run()
	if err != nil {
		d.say(notice, "No domain provided, skipping HTTP configuration")
		return
	}

	listenAddr, err := (&promptui.Prompt{
		Label:   d.T("Listen address for HTTP server"),
		Default: "0.0.0.0:443",
	}).Run()
	if err != nil {
		listenAddr = "0.0.0.0:443"
	}

	curioCfg.HTTP.Enable = true
	curioCfg.HTTP.DomainName = domain
	curioCfg.HTTP.ListenAddress = listenAddr

	// Generate PDP config TOML
	cb, err := config.ConfigUpdate(curioCfg, config.DefaultCurioConfig(), config.Commented(true), config.DefaultKeepUncommented(), config.NoEnv())
	if err != nil {
		d.say(notice, "Error generating PDP config: %s", err.Error())
		return
	}

	// Save to database
	_, err = d.DB.Exec(d.ctx, `INSERT INTO harmony_config (title, config) VALUES ('pdp', $1) ON CONFLICT (title) DO UPDATE SET config = $1`, string(cb))
	if err != nil {
		d.say(notice, "Error saving PDP config: %s", err.Error())
		return
	}

	stepCompleted(d, d.T("PDP configuration layer created"))

	// Step 2: Import wallet private key
	d.say(plain, "Setting up PDP wallet...")
	d.say(plain, "You need a delegated Filecoin wallet address to use with PDP.")

	// Check if PDP key already exists
	var existingKeyAddress string
	err = d.DB.QueryRow(d.ctx, `SELECT address FROM eth_keys WHERE role = 'pdp'`).Scan(&existingKeyAddress)
	hasExistingKey := err == nil

	// Build menu items
	menuItems := []string{}
	useExistingIndex := -1

	if hasExistingKey {
		// Show last 8 characters of address for identification
		addressSuffix := existingKeyAddress
		if len(addressSuffix) > 8 {
			addressSuffix = "..." + addressSuffix[len(addressSuffix)-8:]
		}
		useExistingIndex = len(menuItems)
		menuItems = append(menuItems, d.T("Use existing key, ending in %s", addressSuffix))
	}

	importKeyIndex := len(menuItems)
	menuItems = append(menuItems, d.T("Import delegated wallet private key"))
	skipIndex := len(menuItems)
	menuItems = append(menuItems, d.T("Skip wallet setup for now"))

	i, _, err := (&promptui.Select{
		Label:     d.T("How would you like to proceed?"),
		Items:     menuItems,
		Templates: d.selectTemplates,
	}).Run()
	if err != nil || i == skipIndex {
		d.say(plain, "You can set up the wallet later using the Curio GUI or CLI")
		return
	}

	switch i {
	case useExistingIndex:
		// Use existing key
		d.say(plain, "Using existing PDP wallet key: %s", existingKeyAddress)
		stepCompleted(d, d.T("PDP wallet configured"))
	case importKeyIndex:
		// Import or create - show instructions first
		d.say(plain, "You can create a new delegated wallet using Lotus:")
		d.say(code, "lotus wallet new delegated")
		d.say(plain, "   Then export its private key with:")
		d.say(code, "lotus wallet export <address> | xxd -r -p | jq -r '.PrivateKey' | base64 -d | xxd -p -c 32")
		d.say(plain, "")
		d.say(plain, "Enter your delegated wallet private key (hex format):")

		privateKeyHex, err := (&promptui.Prompt{
			Label: d.T("Private key"),
		}).Run()
		if err != nil || privateKeyHex == "" {
			d.say(notice, "No private key provided")
			return
		}
		importPDPPrivateKey(d, privateKeyHex)
	case skipIndex:
		d.say(plain, "You can set up the wallet later using the Curio GUI or CLI")
	}

	d.say(plain, "")
	d.say(plain, "PDP setup complete!")
	d.say(plain, "To start Curio with PDP enabled, run:")
	d.say(code, "curio run --layers=gui,pdp")
	d.say(plain, "Make sure to send FIL/tFIL to your 0x wallet address for PDP operations.")
	d.say(plain, "")
	d.say(notice, "Next steps:")
	if domain != "" {
		d.say(plain, "1. Test your PDP service with: pdptool ping --service-url https://%s --service-name public", domain)
	} else {
		d.say(plain, "1. Test your PDP service with: pdptool ping --service-url https://your-domain.com --service-name public")
	}
	d.say(plain, "2. Register your FWSS node")
	d.say(plain, "3. Explore FWSS & PDP tools at https://www.filecoin.services")
	d.say(plain, "4. Join the community: Filecoin Slack #fil-pdp")
}

func importPDPPrivateKey(d *MigrationData, hexPrivateKey string) {
	hexPrivateKey = strings.TrimSpace(hexPrivateKey)
	hexPrivateKey = strings.TrimPrefix(hexPrivateKey, "0x")
	hexPrivateKey = strings.TrimPrefix(hexPrivateKey, "0X")

	if hexPrivateKey == "" {
		d.say(notice, "Private key cannot be empty")
		return
	}

	// Decode hex private key
	privateKeyBytes, err := hex.DecodeString(hexPrivateKey)
	if err != nil {
		d.say(notice, "Failed to decode private key: %s", err.Error())
		return
	}

	// Convert to ECDSA and derive address
	privateKey, err := crypto.ToECDSA(privateKeyBytes)
	if err != nil {
		d.say(notice, "Invalid private key: %s", err.Error())
		return
	}

	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()

	// Insert or update eth_keys table
	_, err = d.DB.BeginTransaction(d.ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Insert the new PDP key
		_, err = tx.Exec(`INSERT INTO eth_keys (address, private_key, role) VALUES ($1, $2, 'pdp')`, address, privateKeyBytes)
		if err != nil {
			return false, fmt.Errorf("failed to insert PDP key: %v", err)
		}
		return true, nil
	})
	if err != nil {
		d.say(notice, "Failed to import PDP key: %s", err.Error())
		return
	}

	d.say(plain, "PDP wallet imported successfully!")
	d.say(plain, "Ethereum address (0x): %s", address)
	stepCompleted(d, d.T("PDP wallet imported"))
}

func readStorageConfig(path string) (s *storiface.StorageConfig, errOut error) {
	expandedPath, err := homedir.Expand(path)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(expandedPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		err := file.Close()
		if err != nil {
			errOut = err
		}
	}()

	var cfg storiface.StorageConfig
	err = json.NewDecoder(file).Decode(&cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

func writeStorageConfig(path string, cfg storiface.StorageConfig) error {
	expandedPath, err := homedir.Expand(path)
	if err != nil {
		return err
	}

	// Ensure directory exists
	dir := filepath.Dir(expandedPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(expandedPath, b, 0644)
}
