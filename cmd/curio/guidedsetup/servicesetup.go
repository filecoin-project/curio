package guidedsetup

import (
	"bufio"
	"errors"
	"fmt"
	"io"
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

	var layers, additionalEnvVars, envVars []string

	repoPath := "~/.curio"
	nodeName := "ChangeME"

	// Take user input to remaining env vars
	for {
		i, _, err := (&promptui.Select{
			Label: d.T("Enter the info to configure your Curio node"),
			Items: []string{
				d.T("CURIO_LAYERS: %s", layers),
				d.T("CURIO_REPO_PATH: %s", repoPath),
				d.T("CURIO_NODE_NAME: %s", nodeName),
				d.T("Add additional variables like FIL_PROOFS_PARAMETER_CACHE: %s", envVars),
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
			// Ask if the user wants to add additional variables
			additionalVars, err := (&promptui.Prompt{
				Label: d.T("Do you want to add additional variables like FIL_PROOFS_PARAMETER_CACHE? (y/n)"),
				Validate: func(input string) error {
					if strings.EqualFold(input, "y") || strings.EqualFold(input, "yes") {
						return nil
					}
					if strings.EqualFold(input, "n") || strings.EqualFold(input, "no") {
						return nil
					}
					return errors.New("incorrect input")
				},
			}).Run()
			if err != nil || strings.Contains(strings.ToLower(additionalVars), "n") {
				d.say(notice, "No additional variables added")
				continue
			}

			// Capture multiline input for additional variables
			d.say(plain, "Start typing your additional environment variables one variable per line. Use Ctrl+D to finish:")
			reader := bufio.NewReader(os.Stdin)

			for {
				text, err := reader.ReadString('\n')
				if err != nil {
					if err == io.EOF {
						break // End of input when Ctrl+D is pressed
					}
					d.say(notice, "Error reading input: %s", err)
					os.Exit(1)
				}
				additionalEnvVars = append(additionalEnvVars, text)
			}

			for _, envVar := range additionalEnvVars {
				parts := strings.SplitN(envVar, "=", 2)
				if len(parts) == 2 {
					envVars = append(envVars, fmt.Sprintf("%s=%s", strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])))
				} else {
					d.say(notice, "Skipping invalid input: %s", envVar)
				}
			}
			continue
		case 4:
			// Define the environment variables to be added or updated
			defenvVars := []string{
				fmt.Sprintf("CURIO_LAYERS=%s", strings.Join(layers, ",")),
				"CURIO_ALL_REMAINING_FIELDS_ARE_OPTIONAL=true",
				fmt.Sprintf("CURIO_DB_HOST=%s", strings.Join(d.HarmonyCfg.Hosts, ",")),
				fmt.Sprintf("CURIO_DB_USER=%s", d.HarmonyCfg.Username),
				fmt.Sprintf("CURIO_DB_PASSWORD=%s", d.HarmonyCfg.Password),
				fmt.Sprintf("CURIO_DB_PORT=%s", d.HarmonyCfg.Port),
				fmt.Sprintf("CURIO_DB_NAME=%s", d.HarmonyCfg.Database),
				fmt.Sprintf("CURIO_REPO_PATH=%s", repoPath),
				fmt.Sprintf("CURIO_NODE_NAME=%s", nodeName),
				"FIL_PROOFS_USE_MULTICORE_SDR=1",
			}
			var w string
			for _, s := range defenvVars {
				w += fmt.Sprintf("%s\n", s)
			}
			for _, s := range envVars {
				w += fmt.Sprintf("%s\n", s)
			}

			// Write the new environment variables to the file
			err = os.WriteFile(envFilePath, []byte(w), 0644)
			if err != nil {
				d.say(notice, "Error writing to file %s: %s", envFilePath, err)
				os.Exit(1)
			}
			d.say(notice, "Service env file /etc/curio.env created successfully.")
			return
		}
	}
}
