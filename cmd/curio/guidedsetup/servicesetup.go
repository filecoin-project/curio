package guidedsetup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/urfave/cli/v2"
)

// This should always match apt/curio.service
var serviceContent = `
[Unit]
Description=Curio
After=network.target

[Service]
ExecStart=/usr/local/bin/curio run
Environment=GOLOG_FILE="/var/log/curio/curio.log"
Environment=GOLOG_LOG_FMT="json"
LimitNOFILE=1000000
Restart=always
RestartSec=10
EnvironmentFile=/etc/curio.env

[Install]
WantedBy=multi-user.target
`

var ServicesetupCmd = &cli.Command{
	Name:  "service-setup",
	Usage: "Run the service setup for a new Curio node. This command will take input from user and create/modify the service files",
	Action: func(cctx *cli.Context) (err error) {
		T, say := SetupLanguage()
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

		say(header, "This interactive tool creates/replace Curio service file and creates the basic env file for it.")
		for _, step := range newServiceSteps {
			step(&migrationData)
		}

		return nil
	},
}

type newServiceStep func(data *MigrationData)

var newServiceSteps = []newServiceStep{
	getDBDetails,
	createServiceFile,
	createEnvFile,
}

func createServiceFile(d *MigrationData) {
	// Define the service file name
	serviceName := "curio.service"

	// Service file paths to check
	servicePaths := []string{
		filepath.Join("/etc/systemd/system/", serviceName),
		filepath.Join("/usr/lib/systemd/system/", serviceName),
	}

	// Check if the service file already exists in any of the standard locations
	for _, servicePath := range servicePaths {
		if _, err := os.Stat(servicePath); err == nil {
			d.say(notice, "Service file %s already exists at %s, no changes made.", serviceName, servicePath)
			return //early return
		} else if !os.IsNotExist(err) {
			d.say(notice, "Error checking for service file at %s: %s", servicePath, err)
			os.Exit(1)
		}
	}

	// If the service file doesn't exist, create it in /etc/systemd/system/
	targetServicePath := filepath.Join("/etc/systemd/system/", serviceName)
	err := os.WriteFile(targetServicePath, []byte(serviceContent), 0644)
	if err != nil {
		d.say(notice, "Error writing service file: %s", err)
		os.Exit(1)
	}

	d.say(notice, "Service file %s created successfully at %s", serviceName, targetServicePath)

	// Reload systemd to recognize the new service
	cmd := exec.Command("systemctl", "daemon-reload")
	if err := cmd.Run(); err != nil {
		d.say(notice, "Error reloading systemd: %s", err)
		os.Exit(1)
	}

	// Enable the service to start on boot
	cmd = exec.Command("systemctl", "enable", serviceName)
	if err := cmd.Run(); err != nil {
		d.say(notice, "Error enabling service: %s", err)
		os.Exit(1)
	}
	d.say(notice, "Service %s enabled.\n", serviceName)
}

func createEnvFile(d *MigrationData) {
	// Define the path to the environment file
	envFilePath := "/etc/curio.env"

	var layers []string
	var repoPath, nodeName string

	// Take user input to remaining env vars
	for {
		i, _, err := (&promptui.Select{
			Label: d.T("Enter the info to configure your Curio node"),
			Items: []string{
				d.T("CURIO_LAYERS: %s", ""),
				d.T("CURIO_REPO_PATH: %s", "~/.curio"),
				d.T("CURIO_NODE_NAME: %s", ""),
				d.T("Continue update the env file.")},
			Size:      6,
			Templates: d.selectTemplates,
		}).Run()
		if err != nil {
			d.say(notice, "Env config error occurred, abandoning: %s ", err.Error())
			os.Exit(1)
		}
		switch i {
		case 0:
			host, err := (&promptui.Prompt{
				Label: d.T("Enter the config layer name to use for this node. Base is already included by default"),
			}).Run()
			if err != nil {
				d.say(notice, "No layer provided")
				continue
			}
			layers = strings.Split(host, ",")
		case 1, 2:
			val, err := (&promptui.Prompt{
				Label: d.T("Enter the %s", []string{"repo path", "node name"}[i-1]),
			}).Run()
			if err != nil {
				d.say(notice, "No value provided")
				continue
			}
			switch i {
			case 1:
				repoPath = val
			case 2:
				nodeName = val
			}
			continue
		case 3:
			// Define the environment variables to be added or updated
			envVars := map[string]string{
				"CURIO_LAYERS": fmt.Sprintf("export CURIO_LAYERS=%s", strings.Join(layers, ",")),
				"CURIO_ALL_REMAINING_FIELDS_ARE_OPTIONAL": "true",
				"CURIO_DB_HOST":                fmt.Sprintf("export CURIO_DB_HOST=%s", strings.Join(d.HarmonyCfg.Hosts, ",")),
				"CURIO_DB_PORT":                fmt.Sprintf("export CURIO_DB_PORT=%s", d.HarmonyCfg.Port),
				"CURIO_DB_NAME":                fmt.Sprintf("export CURIO_DB_HOST=%s", d.HarmonyCfg.Database),
				"CURIO_DB_USER":                fmt.Sprintf("export CURIO_DB_HOST=%s", d.HarmonyCfg.Username),
				"CURIO_DB_PASSWORD":            fmt.Sprintf("export CURIO_DB_HOST=%s", d.HarmonyCfg.Password),
				"CURIO_REPO_PATH":              repoPath,
				"CURIO_NODE_NAME":              nodeName,
				"FIL_PROOFS_USE_MULTICORE_SDR": "1",
			}

			// Open the file with truncation (this clears the file if it exists)
			file, err := os.OpenFile(envFilePath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
			if err != nil {
				d.say(notice, "Error opening or creating file %s: %s", envFilePath, err)
				os.Exit(1)
			}
			defer func() {
				_ = file.Close()
			}()

			// Write the new environment variables to the file
			for key, value := range envVars {
				line := fmt.Sprintf("%s=%s\n", key, value)
				if _, err := file.WriteString(line); err != nil {
					d.say(notice, "Error writing to file %s: %s", envFilePath, err)
					os.Exit(1)
				}
			}
			d.say(notice, "Service env file /etc/curio.env created successfully.")
			return
		}
	}
}
