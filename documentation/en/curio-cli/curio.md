# curio
```
NAME:
   curio - Filecoin decentralized storage network provider

USAGE:
   curio [global options] command [command options]

VERSION:
   1.24.3

COMMANDS:
   cli           Execute cli commands
   run           Start a Curio process
   config        Manage node config by layers. The layer 'base' will always be applied at Curio start-up.
   test          Utility functions for testing
   web           Start Curio web interface
   guided-setup  Run the guided setup for migrating from lotus-miner to Curio or Creating a new Curio miner
   seal          Manage the sealing pipeline
   unseal        Manage unsealed data
   market        
   fetch-params  Fetch proving parameters
   calc          Math Utils
   help, h       Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --color              use color in display output (default: depends on output being a TTY)
   --db-host value      Command separated list of hostnames for yugabyte cluster (default: "127.0.0.1") [$CURIO_DB_HOST, $CURIO_HARMONYDB_HOSTS]
   --db-name value      (default: "yugabyte") [$CURIO_DB_NAME, $CURIO_HARMONYDB_NAME]
   --db-user value      (default: "yugabyte") [$CURIO_DB_USER, $CURIO_HARMONYDB_USERNAME]
   --db-password value  (default: "yugabyte") [$CURIO_DB_PASSWORD, $CURIO_HARMONYDB_PASSWORD]
   --db-port value      (default: "5433") [$CURIO_DB_PORT, $CURIO_HARMONYDB_PORT]
   --repo-path value    (default: "~/.curio") [$CURIO_REPO_PATH]
   --vv                 enables very verbose mode, useful for debugging the CLI (default: false)
   --help, -h           show help
   --version, -v        print the version
```

## curio cli
```
NAME:
   curio cli - Execute cli commands

USAGE:
   curio cli command [command options]

COMMANDS:
   info      Get Curio node info
   storage   manage sector storage
   log       Manage logging
   wait-api  Wait for Curio api to come online
   stop      Stop a running Curio process
   cordon    Cordon a machine, set it to maintenance mode
   uncordon  Uncordon a machine, resume scheduling
   help, h   Shows a list of commands or help for one command

OPTIONS:
   --machine value  machine host:port (curio run --listen address)
   --help, -h       show help
```

### curio cli info
```
NAME:
   curio cli info - Get Curio node info

USAGE:
   curio cli info [command options]

OPTIONS:
   --help, -h  show help
```

### curio cli storage
```
NAME:
   curio cli storage - manage sector storage

USAGE:
   curio cli storage command [command options]

DESCRIPTION:
   Sectors can be stored across many filesystem paths. These
   commands provide ways to manage the storage a Curio node will use to store sectors
   long term for proving (references as 'store') as well as how sectors will be
   stored while moving through the sealing pipeline (references as 'seal').

COMMANDS:
   attach   attach local storage path
   detach   detach local storage path
   list     list local storage paths
   find     find sector in the storage system
   help, h  Shows a list of commands or help for one command

OPTIONS:
   --help, -h  show help
```

#### curio cli storage attach
```
NAME:
   curio cli storage attach - attach local storage path

USAGE:
   curio cli storage attach [command options] [path]

DESCRIPTION:
   Storage can be attached to a Curio node using this command. The storage volume
   list is stored local to the Curio node in storage.json set in curio run. We do not
   recommend manually modifying this value without further understanding of the
   storage system.

   Each storage volume contains a configuration file which describes the
   capabilities of the volume. When the '--init' flag is provided, this file will
   be created using the additional flags.

   Weight
   A high weight value means data will be more likely to be stored in this path

   Seal
   Data for the sealing process will be stored here

   Store
   Finalized sectors that will be moved here for long term storage and be proven
   over time
      

OPTIONS:
   --init                                       initialize the path first (default: false)
   --weight value                               (for init) path weight (default: 10)
   --seal                                       (for init) use path for sealing (default: false)
   --store                                      (for init) use path for long-term storage (default: false)
   --max-storage value                          (for init) limit storage space for sectors (expensive for very large paths!)
   --groups value [ --groups value ]            path group names
   --allow-to value [ --allow-to value ]        path groups allowed to pull data from this path (allow all if not specified)
   --allow-types value [ --allow-types value ]  file types to allow storing in this path
   --deny-types value [ --deny-types value ]    file types to deny storing in this path
   --help, -h                                   show help
```

#### curio cli storage detach
```
NAME:
   curio cli storage detach - detach local storage path

USAGE:
   curio cli storage detach [command options] [path]

OPTIONS:
   --really-do-it  (default: false)
   --help, -h      show help
```

#### curio cli storage list
```
NAME:
   curio cli storage list - list local storage paths

USAGE:
   curio cli storage list [command options]

OPTIONS:
   --local     only list local storage paths (default: false)
   --help, -h  show help
```

#### curio cli storage find
```
NAME:
   curio cli storage find - find sector in the storage system

USAGE:
   curio cli storage find [command options] [miner address] [sector number]

OPTIONS:
   --help, -h  show help
```

### curio cli log
```
NAME:
   curio cli log - Manage logging

USAGE:
   curio cli log command [command options]

COMMANDS:
   list       List log systems
   set-level  Set log level
   help, h    Shows a list of commands or help for one command

OPTIONS:
   --help, -h  show help
```

#### curio cli log list
```
NAME:
   curio cli log list - List log systems

USAGE:
   curio cli log list [command options]

OPTIONS:
   --help, -h  show help
```

#### curio cli log set-level
```
NAME:
   curio cli log set-level - Set log level

USAGE:
   curio cli log set-level [command options] [level]

DESCRIPTION:
   Set the log level for logging systems:

      The system flag can be specified multiple times.

      eg) log set-level --system chain --system chainxchg debug

      Available Levels:
      debug
      info
      warn
      error

      Environment Variables:
      GOLOG_LOG_LEVEL - Default log level for all log systems
      GOLOG_LOG_FMT   - Change output log format (json, nocolor)
      GOLOG_FILE      - Write logs to file
      GOLOG_OUTPUT    - Specify whether to output to file, stderr, stdout or a combination, i.e. file+stderr


OPTIONS:
   --system value [ --system value ]  limit to log system
   --help, -h                         show help
```

### curio cli wait-api
```
NAME:
   curio cli wait-api - Wait for Curio api to come online

USAGE:
   curio cli wait-api [command options]

OPTIONS:
   --timeout value  duration to wait till fail (default: 30s)
   --help, -h       show help
```

### curio cli stop
```
NAME:
   curio cli stop - Stop a running Curio process

USAGE:
   curio cli stop [command options]

OPTIONS:
   --help, -h  show help
```

### curio cli cordon
```
NAME:
   curio cli cordon - Cordon a machine, set it to maintenance mode

USAGE:
   curio cli cordon [command options]

OPTIONS:
   --help, -h  show help
```

### curio cli uncordon
```
NAME:
   curio cli uncordon - Uncordon a machine, resume scheduling

USAGE:
   curio cli uncordon [command options]

OPTIONS:
   --help, -h  show help
```

## curio run
```
NAME:
   curio run - Start a Curio process

USAGE:
   curio run [command options]

OPTIONS:
   --listen value                                                                       host address and port the worker api will listen on (default: "0.0.0.0:12300") [$CURIO_LISTEN]
   --nosync                                                                             don't check full-node sync status (default: false)
   --manage-fdlimit                                                                     manage open file limit (default: true)
   --layers value, -l value, --layer value [ --layers value, -l value, --layer value ]  list of layers to be interpreted (atop defaults). Default: base [$CURIO_LAYERS]
   --name value                                                                         custom node name [$CURIO_NODE_NAME]
   --help, -h                                                                           show help
```

## curio config
```
NAME:
   curio config - Manage node config by layers. The layer 'base' will always be applied at Curio start-up.

USAGE:
   curio config command [command options]

COMMANDS:
   default, defaults                Print default node config
   set, add, update, create         Set a config layer or the base by providing a filename or stdin.
   get, cat, show                   Get a config layer by name. You may want to pipe the output to a file, or use 'less'
   list, ls                         List config layers present in the DB.
   interpret, view, stacked, stack  Interpret stacked config layers by this version of curio, with system-generated comments.
   remove, rm, del, delete          Remove a named config layer.
   edit                             edit a config layer
   new-cluster                      Create new configuration for a new cluster
   help, h                          Shows a list of commands or help for one command

OPTIONS:
   --help, -h  show help
```

### curio config default
```
NAME:
   curio config default - Print default node config

USAGE:
   curio config default [command options]

OPTIONS:
   --no-comment  don't comment default values (default: false)
   --help, -h    show help
```

### curio config set
```
NAME:
   curio config set - Set a config layer or the base by providing a filename or stdin.

USAGE:
   curio config set [command options] a layer's file name

OPTIONS:
   --title value  title of the config layer (req'd for stdin)
   --help, -h     show help
```

### curio config get
```
NAME:
   curio config get - Get a config layer by name. You may want to pipe the output to a file, or use 'less'

USAGE:
   curio config get [command options] layer name

OPTIONS:
   --help, -h  show help
```

### curio config list
```
NAME:
   curio config list - List config layers present in the DB.

USAGE:
   curio config list [command options]

OPTIONS:
   --help, -h  show help
```

### curio config interpret
```
NAME:
   curio config interpret - Interpret stacked config layers by this version of curio, with system-generated comments.

USAGE:
   curio config interpret [command options] a list of layers to be interpreted as the final config

OPTIONS:
   --layers value [ --layers value ]  comma or space separated list of layers to be interpreted (base is always applied)
   --help, -h                         show help
```

### curio config remove
```
NAME:
   curio config remove - Remove a named config layer.

USAGE:
   curio config remove [command options]

OPTIONS:
   --help, -h  show help
```

### curio config edit
```
NAME:
   curio config edit - edit a config layer

USAGE:
   curio config edit [command options] [layer name]

OPTIONS:
   --editor value         editor to use (default: "vim") [$EDITOR]
   --source value         source config layer (default: <edited layer>)
   --allow-overwrite      allow overwrite of existing layer if source is a different layer (default: false)
   --no-source-diff       save the whole config into the layer, not just the diff (default: false)
   --no-interpret-source  do not interpret source layer (default: true if --source is set)
   --help, -h             show help
```

### curio config new-cluster
```
NAME:
   curio config new-cluster - Create new configuration for a new cluster

USAGE:
   curio config new-cluster [command options] [SP actor address...]

OPTIONS:
   --help, -h  show help
```

## curio test
```
NAME:
   curio test - Utility functions for testing

USAGE:
   curio test command [command options]

COMMANDS:
   window-post, wd, windowpost, wdpost  Compute a proof-of-spacetime for a sector (requires the sector to be pre-sealed). These will not send to the chain.
   debug                                Collection of debugging utilities
   help, h                              Shows a list of commands or help for one command

OPTIONS:
   --help, -h  show help
```

### curio test window-post
```
NAME:
   curio test window-post - Compute a proof-of-spacetime for a sector (requires the sector to be pre-sealed). These will not send to the chain.

USAGE:
   curio test window-post command [command options]

COMMANDS:
   here, cli                                       Compute WindowPoSt for performance and configuration testing.
   task, scheduled, schedule, async, asynchronous  Test the windowpost scheduler by running it on the next available curio. If tasks fail all retries, you will need to ctrl+c to exit.
   help, h                                         Shows a list of commands or help for one command

OPTIONS:
   --help, -h  show help
```

#### curio test window-post here
```
NAME:
   curio test window-post here - Compute WindowPoSt for performance and configuration testing.

USAGE:
   curio test window-post here [command options] [deadline index]

DESCRIPTION:
   Note: This command is intended to be used to verify PoSt compute performance.
   It will not send any messages to the chain. Since it can compute any deadline, output may be incorrectly timed for the chain.

OPTIONS:
   --deadline value                   deadline to compute WindowPoSt for  (default: 0)
   --layers value [ --layers value ]  list of layers to be interpreted (atop defaults). Default: base
   --storage-json value               path to json file containing storage config (default: "~/.curio/storage.json")
   --partition value                  partition to compute WindowPoSt for (default: 0)
   --help, -h                         show help
```

#### curio test window-post task
```
NAME:
   curio test window-post task - Test the windowpost scheduler by running it on the next available curio. If tasks fail all retries, you will need to ctrl+c to exit.

USAGE:
   curio test window-post task [command options]

OPTIONS:
   --deadline value                   deadline to compute WindowPoSt for  (default: 0)
   --layers value [ --layers value ]  list of layers to be interpreted (atop defaults). Default: base
   --help, -h                         show help
```

### curio test debug
```
NAME:
   curio test debug - Collection of debugging utilities

USAGE:
   curio test debug command [command options]

COMMANDS:
   ipni-piece-chunks  generate ipni chunks from a file
   help, h            Shows a list of commands or help for one command

OPTIONS:
   --help, -h  show help
```

#### curio test debug ipni-piece-chunks
```
NAME:
   curio test debug ipni-piece-chunks - generate ipni chunks from a file

USAGE:
   curio test debug ipni-piece-chunks [command options]

OPTIONS:
   --help, -h  show help
```

## curio web
```
NAME:
   curio web - Start Curio web interface

USAGE:
   curio web [command options]

DESCRIPTION:
   Start an instance of Curio web interface. 
     This creates the 'web' layer if it does not exist, then calls run with that layer.

OPTIONS:
   --gui-listen value                 Address to listen for the GUI on (default: "0.0.0.0:4701")
   --nosync                           don't check full-node sync status (default: false)
   --layers value [ --layers value ]  list of layers to be interpreted (atop defaults). Default: base
   --help, -h                         show help
```

## curio guided-setup
```
NAME:
   curio guided-setup - Run the guided setup for migrating from lotus-miner to Curio or Creating a new Curio miner

USAGE:
   curio guided-setup [command options]

OPTIONS:
   --help, -h  show help
```

## curio seal
```
NAME:
   curio seal - Manage the sealing pipeline

USAGE:
   curio seal command [command options]

COMMANDS:
   start    Start new sealing operations manually
   events   List pipeline events
   help, h  Shows a list of commands or help for one command

OPTIONS:
   --help, -h  show help
```

### curio seal start
```
NAME:
   curio seal start - Start new sealing operations manually

USAGE:
   curio seal start [command options]

OPTIONS:
   --actor value                      Specify actor address to start sealing sectors for
   --now                              Start sealing sectors for all actors now (not on schedule) (default: false)
   --cc                               Start sealing new CC sectors (default: false)
   --count value                      Number of sectors to start (default: 1)
   --synthetic                        Use synthetic PoRep (default: false)
   --layers value [ --layers value ]  list of layers to be interpreted (atop defaults). Default: base
   --duration-days value, -d value    How long to commit sectors for (default: 1278 (3.5 years))
   --help, -h                         show help
```

### curio seal events
```
NAME:
   curio seal events - List pipeline events

USAGE:
   curio seal events [command options]

OPTIONS:
   --actor value   Filter events by actor address; lists all if not specified
   --sector value  Filter events by sector number; requires --actor to be specified (default: 0)
   --last value    Limit output to the last N events (default: 100)
   --help, -h      show help
```

## curio unseal
```
NAME:
   curio unseal - Manage unsealed data

USAGE:
   curio unseal command [command options]

COMMANDS:
   info              Get information about unsealed data
   list-sectors      List data from the sectors_unseal_pipeline and sectors_meta tables
   set-target-state  Set the target unseal state for a sector
   check             Check data integrity in unsealed sector files
   help, h           Shows a list of commands or help for one command

OPTIONS:
   --help, -h  show help
```

### curio unseal info
```
NAME:
   curio unseal info - Get information about unsealed data

USAGE:
   curio unseal info [command options] [minerAddress] [sectorNumber]

OPTIONS:
   --help, -h  show help
```

### curio unseal list-sectors
```
NAME:
   curio unseal list-sectors - List data from the sectors_unseal_pipeline and sectors_meta tables

USAGE:
   curio unseal list-sectors [command options]

OPTIONS:
   --sp-id value, -s value   Filter by storage provider ID (default: 0)
   --output value, -o value  Output file path (default: stdout)
   --help, -h                show help
```

### curio unseal set-target-state
```
NAME:
   curio unseal set-target-state - Set the target unseal state for a sector

USAGE:
   curio unseal set-target-state [command options] <miner-id> <sector-number> <target-state>

DESCRIPTION:
   Set the target unseal state for a specific sector.
      <miner-id>: The storage provider ID
      <sector-number>: The sector number
      <target-state>: The target state (true, false, or none)

      The unseal target state indicates to curio how an unsealed copy of the sector should be maintained.
        If the target state is true, curio will ensure that the sector is unsealed.
        If the target state is false, curio will ensure that there is no unsealed copy of the sector.
        If the target state is none, curio will not change the current state of the sector.

      Currently when the curio will only start new unseal processes when the target state changes from another state to true.

      When the target state is false, and an unsealed sector file exists, the GC mark step will create a removal mark
      for the unsealed sector file. The file will only be removed after the removal mark is accepted.


OPTIONS:
   --help, -h  show help
```

### curio unseal check
```
NAME:
   curio unseal check - Check data integrity in unsealed sector files

USAGE:
   curio unseal check [command options] <miner-id> <sector-number>

DESCRIPTION:
   Create a check task for a specific sector, wait for its completion, and output the result.
      <miner-id>: The storage provider ID
      <sector-number>: The sector number

OPTIONS:
   --help, -h  show help
```

## curio market
```
NAME:
   curio market

USAGE:
   curio market command [command options]

COMMANDS:
   seal            start sealing a deal sector early
   add-url         Add URL to fetch data for offline deals
   move-to-escrow  Moves funds from the deal collateral wallet into escrow with the storage market actor
   help, h         Shows a list of commands or help for one command

OPTIONS:
   --help, -h  show help
```

### curio market seal
```
NAME:
   curio market seal - start sealing a deal sector early

USAGE:
   curio market seal [command options] <sector>

OPTIONS:
   --actor value  Specify actor address to start sealing sectors for
   --synthetic    Use synthetic PoRep (default: false)
   --help, -h     show help
```

### curio market add-url
```
NAME:
   curio market add-url - Add URL to fetch data for offline deals

USAGE:
   curio market add-url [command options] <deal UUID> <raw size/car size>

OPTIONS:
   --file value                                               CSV file location to use for multiple deal input. Each line in the file should be in the format 'uuid,raw size,url,header1,header2...'
   --header HEADER, -H HEADER [ --header HEADER, -H HEADER ]  Custom HEADER to include in the HTTP request
   --url URL, -u URL                                          URL to send the request to
   --help, -h                                                 show help
```

### curio market move-to-escrow
```
NAME:
   curio market move-to-escrow - Moves funds from the deal collateral wallet into escrow with the storage market actor

USAGE:
   curio market move-to-escrow [command options] <amount>

OPTIONS:
   --actor value    Specify actor address to start sealing sectors for
   --max-fee value  maximum fee in FIL user is willing to pay for this message (default: "0.5")
   --wallet value   Specify wallet address to send the funds from
   --help, -h       show help
```

## curio fetch-params
```
NAME:
   curio fetch-params - Fetch proving parameters

USAGE:
   curio fetch-params [command options] [sectorSize]

OPTIONS:
   --help, -h  show help
```

## curio calc
```
NAME:
   curio calc - Math Utils

USAGE:
   curio calc command [command options]

COMMANDS:
   batch-cpu         Analyze and display the layout of batch sealer threads
   supraseal-config  Generate a supra_seal configuration
   help, h           Shows a list of commands or help for one command

OPTIONS:
   --actor value  
   --help, -h     show help
```

### curio calc batch-cpu
```
NAME:
   curio calc batch-cpu - Analyze and display the layout of batch sealer threads

USAGE:
   curio calc batch-cpu [command options]

DESCRIPTION:
   Analyze and display the layout of batch sealer threads on your CPU.

   It provides detailed information about CPU utilization for batch sealing operations, including core allocation, thread
   distribution for different batch sizes.

OPTIONS:
   --dual-hashers  (default: true)
   --help, -h      show help
```

### curio calc supraseal-config
```
NAME:
   curio calc supraseal-config - Generate a supra_seal configuration

USAGE:
   curio calc supraseal-config [command options]

DESCRIPTION:
   Generate a supra_seal configuration for a given batch size.

   This command outputs a configuration expected by SupraSeal. Main purpose of this command is for debugging and testing.
   The config can be used directly with SupraSeal binaries to test it without involving Curio.

OPTIONS:
   --dual-hashers                Zen3 and later supports two sectors per thread, set to false for older CPUs (default: true)
   --batch-size value, -b value  (default: 0)
   --help, -h                    show help
```
