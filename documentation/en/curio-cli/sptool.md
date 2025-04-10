# sptool
```
NAME:
   sptool - Manage Filecoin Miner Actor

USAGE:
   sptool [global options] command [command options]

VERSION:
   1.25.0

COMMANDS:
   actor    Manage Filecoin Miner Actor Metadata
   info     Print miner actor info
   sectors  interact with sector store
   proving  View proving information
   toolbox  some tools to fix some problems
   help, h  Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --log-level value  (default: "info")
   --actor value      miner actor to manage [$SP_ADDRESS]
   --verbose, --vv    enable verbose logging (default: false)
   --help, -h         show help
   --version, -v      print the version
```

## sptool actor
```
NAME:
   sptool actor - Manage Filecoin Miner Actor Metadata

USAGE:
   sptool actor command [command options]

COMMANDS:
   set-addresses, set-addrs    set addresses that your miner can be publicly dialed on
   withdraw                    withdraw available balance to beneficiary
   repay-debt                  pay down a miner's debt
   set-peer-id                 set the peer id of your miner
   set-owner                   Set owner address (this command should be invoked twice, first with the old owner as the senderAddress, and then with the new owner)
   control                     Manage control addresses
   propose-change-worker       Propose a worker address change
   confirm-change-worker       Confirm a worker address change
   compact-allocated           compact allocated sectors bitfield
   propose-change-beneficiary  Propose a beneficiary address change
   confirm-change-beneficiary  Confirm a beneficiary address change
   new-miner                   Initializes a new miner actor
   help, h                     Shows a list of commands or help for one command

OPTIONS:
   --help, -h  show help
```

### sptool actor set-addresses
```
NAME:
   sptool actor set-addresses - set addresses that your miner can be publicly dialed on

USAGE:
   sptool actor set-addresses [command options] <multiaddrs>

OPTIONS:
   --from value       optionally specify the account to send the message from
   --gas-limit value  set gas limit (default: 0)
   --unset            unset address (default: false)
   --help, -h         show help
```

### sptool actor withdraw
```
NAME:
   sptool actor withdraw - withdraw available balance to beneficiary

USAGE:
   sptool actor withdraw [command options] [amount (FIL)]

OPTIONS:
   --confidence value  number of block confirmations to wait for (default: 5)
   --beneficiary       send withdraw message from the beneficiary address (default: false)
   --help, -h          show help
```

### sptool actor repay-debt
```
NAME:
   sptool actor repay-debt - pay down a miner's debt

USAGE:
   sptool actor repay-debt [command options] [amount (FIL)]

OPTIONS:
   --from value  optionally specify the account to send funds from
   --help, -h    show help
```

### sptool actor set-peer-id
```
NAME:
   sptool actor set-peer-id - set the peer id of your miner

USAGE:
   sptool actor set-peer-id [command options] <peer id>

OPTIONS:
   --gas-limit value  set gas limit (default: 0)
   --help, -h         show help
```

### sptool actor set-owner
```
NAME:
   sptool actor set-owner - Set owner address (this command should be invoked twice, first with the old owner as the senderAddress, and then with the new owner)

USAGE:
   sptool actor set-owner [command options] [newOwnerAddress senderAddress]

OPTIONS:
   --really-do-it  Actually send transaction performing the action (default: false)
   --help, -h      show help
```

### sptool actor control
```
NAME:
   sptool actor control - Manage control addresses

USAGE:
   sptool actor control command [command options]

COMMANDS:
   list     Get currently set control addresses. Note: This excludes most roles as they are not known to the immediate chain state.
   set      Set control address(-es)
   help, h  Shows a list of commands or help for one command

OPTIONS:
   --help, -h  show help
```

#### sptool actor control list
```
NAME:
   sptool actor control list - Get currently set control addresses. Note: This excludes most roles as they are not known to the immediate chain state.

USAGE:
   sptool actor control list [command options]

OPTIONS:
   --verbose   (default: false)
   --help, -h  show help
```

#### sptool actor control set
```
NAME:
   sptool actor control set - Set control address(-es)

USAGE:
   sptool actor control set [command options] [...address]

OPTIONS:
   --really-do-it  Actually send transaction performing the action (default: false)
   --help, -h      show help
```

### sptool actor propose-change-worker
```
NAME:
   sptool actor propose-change-worker - Propose a worker address change

USAGE:
   sptool actor propose-change-worker [command options] [address]

OPTIONS:
   --really-do-it  Actually send transaction performing the action (default: false)
   --help, -h      show help
```

### sptool actor confirm-change-worker
```
NAME:
   sptool actor confirm-change-worker - Confirm a worker address change

USAGE:
   sptool actor confirm-change-worker [command options] [address]

OPTIONS:
   --really-do-it  Actually send transaction performing the action (default: false)
   --help, -h      show help
```

### sptool actor compact-allocated
```
NAME:
   sptool actor compact-allocated - compact allocated sectors bitfield

USAGE:
   sptool actor compact-allocated [command options]

OPTIONS:
   --mask-last-offset value  Mask sector IDs from 0 to 'highest_allocated - offset' (default: 0)
   --mask-upto-n value       Mask sector IDs from 0 to 'n' (default: 0)
   --really-do-it            Actually send transaction performing the action (default: false)
   --help, -h                show help
```

### sptool actor propose-change-beneficiary
```
NAME:
   sptool actor propose-change-beneficiary - Propose a beneficiary address change

USAGE:
   sptool actor propose-change-beneficiary [command options] [beneficiaryAddress quota expiration]

OPTIONS:
   --really-do-it              Actually send transaction performing the action (default: false)
   --overwrite-pending-change  Overwrite the current beneficiary change proposal (default: false)
   --actor value               specify the address of miner actor
   --help, -h                  show help
```

### sptool actor confirm-change-beneficiary
```
NAME:
   sptool actor confirm-change-beneficiary - Confirm a beneficiary address change

USAGE:
   sptool actor confirm-change-beneficiary [command options] [minerID]

OPTIONS:
   --really-do-it          Actually send transaction performing the action (default: false)
   --existing-beneficiary  send confirmation from the existing beneficiary address (default: false)
   --new-beneficiary       send confirmation from the new beneficiary address (default: false)
   --help, -h              show help
```

### sptool actor new-miner
```
NAME:
   sptool actor new-miner - Initializes a new miner actor

USAGE:
   sptool actor new-miner [command options]

OPTIONS:
   --worker value, -w value  worker key to use for new miner initialisation
   --owner value, -o value   owner key to use for new miner initialisation
   --from value, -f value    address to send actor(miner) creation message from
   --sector-size value       specify sector size to use for new miner initialisation
   --confidence value        number of block confirmations to wait for (default: 5)
   --help, -h                show help
```

## sptool info
```
NAME:
   sptool info - Print miner actor info

USAGE:
   sptool info [command options]

OPTIONS:
   --help, -h  show help
```

## sptool sectors
```
NAME:
   sptool sectors - interact with sector store

USAGE:
   sptool sectors command [command options]

COMMANDS:
   status              Get the seal status of a sector by its number
   list                List sectors
   precommits          Print on-chain precommit info
   check-expire        Inspect expiring sectors
   expired             Get or cleanup expired sectors
   extend              Extend expiring sectors while not exceeding each sector's max life
   terminate           Forcefully terminate a sector (WARNING: This means losing power and pay a one-time termination penalty(including collateral) for the terminated sector)
   compact-partitions  removes dead sectors from partitions and reduces the number of partitions used if possible
   help, h             Shows a list of commands or help for one command

OPTIONS:
   --help, -h  show help
```

### sptool sectors status
```
NAME:
   sptool sectors status - Get the seal status of a sector by its number

USAGE:
   sptool sectors status [command options] <sectorNum>

OPTIONS:
   --log, -l             display event log (default: false)
   --on-chain-info, -c   show sector on chain info (default: false)
   --partition-info, -p  show partition related info (default: false)
   --proof               print snark proof bytes as hex (default: false)
   --help, -h            show help
```

### sptool sectors list
```
NAME:
   sptool sectors list - List sectors

USAGE:
   sptool sectors list [command options]

OPTIONS:
   --help, -h  show help
```

### sptool sectors precommits
```
NAME:
   sptool sectors precommits - Print on-chain precommit info

USAGE:
   sptool sectors precommits [command options]

OPTIONS:
   --help, -h  show help
```

### sptool sectors check-expire
```
NAME:
   sptool sectors check-expire - Inspect expiring sectors

USAGE:
   sptool sectors check-expire [command options]

OPTIONS:
   --cutoff value  skip sectors whose current expiration is more than <cutoff> epochs from now, defaults to 60 days (default: 172800)
   --help, -h      show help
```

### sptool sectors expired
```
NAME:
   sptool sectors expired - Get or cleanup expired sectors

USAGE:
   sptool sectors expired [command options]

OPTIONS:
   --expired-epoch value  epoch at which to check sector expirations (default: WinningPoSt lookback epoch)
   --help, -h             show help
```

### sptool sectors extend
```
NAME:
   sptool sectors extend - Extend expiring sectors while not exceeding each sector's max life

USAGE:
   sptool sectors extend [command options] <sectorNumbers...(optional)>

DESCRIPTION:
   NOTE: --new-expiration, --from and --to flags have multiple formats:
     1. Absolute epoch number: <epoch>
     2. Relative epoch number: +<delta>, e.g. +1000, means 1000 epochs from now
     3. Relative day number: +<delta>d, e.g. +10d, means 10 days from now

   The --extension flag has two formats:
     1. Number of epochs to extend by: <epoch>
     2. Number of days to extend by: <delta>d

   Extensions will be clamped at either the maximum sector extension of 3.5 years/1278 days or the sector's maximum lifetime
     which currently is 5 years.



OPTIONS:
   --from value            only consider sectors whose current expiration epoch is in the range of [from, to], <from> defaults to: now + 120 (1 hour) (default: "+120")
   --to value              only consider sectors whose current expiration epoch is in the range of [from, to], <to> defaults to: now + 92160 (32 days) (default: "+92160")
   --sector-file value     provide a file containing one sector number in each line, ignoring above selecting criteria
   --exclude value         optionally provide a file containing excluding sectors
   --extension value       try to extend selected sectors by this number of epochs, defaults to 540 days (default: "540d")
   --new-expiration value  try to extend selected sectors to this epoch, ignoring extension
   --only-cc               only extend CC sectors (useful for making sector ready for snap upgrade) (default: false)
   --no-cc                 don't extend CC sectors (exclusive with --only-cc) (default: false)
   --drop-claims           drop claims for sectors that can be extended, but only by dropping some of their verified power claims (default: false)
   --tolerance value       don't try to extend sectors by fewer than this number of epochs, defaults to 7 days (default: 20160)
   --max-fee value         use up to this amount of FIL for one message. pass this flag to avoid message congestion. (default: "0")
   --max-sectors value     the maximum number of sectors contained in each message (default: 0)
   --really-do-it          pass this flag to really extend sectors, otherwise will only print out json representation of parameters (default: false)
   --help, -h              show help
```

### sptool sectors terminate
```
NAME:
   sptool sectors terminate - Forcefully terminate a sector (WARNING: This means losing power and pay a one-time termination penalty(including collateral) for the terminated sector)

USAGE:
   sptool sectors terminate [command options] [sectorNum1 sectorNum2 ...]

OPTIONS:
   --actor value   specify the address of miner actor
   --really-do-it  pass this flag if you know what you are doing (default: false)
   --from value    specify the address to send the terminate message from
   --help, -h      show help
```

### sptool sectors compact-partitions
```
NAME:
   sptool sectors compact-partitions - removes dead sectors from partitions and reduces the number of partitions used if possible

USAGE:
   sptool sectors compact-partitions [command options]

OPTIONS:
   --deadline value                           the deadline to compact the partitions in (default: 0)
   --partitions value [ --partitions value ]  list of partitions to compact sectors in
   --really-do-it                             Actually send transaction performing the action (default: false)
   --help, -h                                 show help
```

## sptool proving
```
NAME:
   sptool proving - View proving information

USAGE:
   sptool proving command [command options]

COMMANDS:
   info       View current state information
   deadlines  View the current proving period deadlines information
   deadline   View the current proving period deadline information by its index
   faults     View the currently known proving faulty sectors information
   help, h    Shows a list of commands or help for one command

OPTIONS:
   --help, -h  show help
```

### sptool proving info
```
NAME:
   sptool proving info - View current state information

USAGE:
   sptool proving info [command options]

OPTIONS:
   --help, -h  show help
```

### sptool proving deadlines
```
NAME:
   sptool proving deadlines - View the current proving period deadlines information

USAGE:
   sptool proving deadlines [command options]

OPTIONS:
   --all, -a   Count all sectors (only live sectors are counted by default) (default: false)
   --help, -h  show help
```

### sptool proving deadline
```
NAME:
   sptool proving deadline - View the current proving period deadline information by its index

USAGE:
   sptool proving deadline [command options] <deadlineIdx>

OPTIONS:
   --sector-nums, -n  Print sector/fault numbers belonging to this deadline (default: false)
   --bitfield, -b     Print partition bitfield stats (default: false)
   --help, -h         show help
```

### sptool proving faults
```
NAME:
   sptool proving faults - View the currently known proving faulty sectors information

USAGE:
   sptool proving faults [command options]

OPTIONS:
   --help, -h  show help
```

## sptool toolbox
```
NAME:
   sptool toolbox - some tools to fix some problems

USAGE:
   sptool toolbox command [command options]

COMMANDS:
   spark        Manage Smart Contract PeerID used by Spark
   mk12-client  mk12 client for Curio
   help, h      Shows a list of commands or help for one command

OPTIONS:
   --help, -h  show help
```

### sptool toolbox spark
```
NAME:
   sptool toolbox spark - Manage Smart Contract PeerID used by Spark

USAGE:
   sptool toolbox spark command [command options]

COMMANDS:
   delete-peer  Delete PeerID from Spark Smart Contract
   help, h      Shows a list of commands or help for one command

OPTIONS:
   --help, -h  show help
```

#### sptool toolbox spark delete-peer
```
NAME:
   sptool toolbox spark delete-peer - Delete PeerID from Spark Smart Contract

USAGE:
   sptool toolbox spark delete-peer [command options] <Miner ID>

OPTIONS:
   --really-do-it  Send the message to the smart contract (default: false)
   --help, -h      show help
```

### sptool toolbox mk12-client
```
NAME:
   sptool toolbox mk12-client - mk12 client for Curio

USAGE:
   sptool toolbox mk12-client command [command options]

COMMANDS:
   init               Initialise curio mk12 client repo
   deal               Make an online deal with Curio
   deal-status        
   offline-deal       Make an offline deal with Curio
   allocate           Create new allocation[s] for verified deals
   list-allocations   Lists all allocations for a client address(wallet)
   market-add         Add funds to the Storage Market actor
   market-withdraw    Withdraw funds from the Storage Market actor
   commp              
   generate-rand-car  creates a randomly generated dense car
   wallet             Manage mk12 client wallets
   help, h            Shows a list of commands or help for one command

OPTIONS:
   --mk12-client-repo value  repo directory for mk12 client (default: "~/.curio-client") [$CURIO_MK12_CLIENT_REPO]
   --help, -h                show help
```

#### sptool toolbox mk12-client init
```
NAME:
   sptool toolbox mk12-client init - Initialise curio mk12 client repo

USAGE:
   sptool toolbox mk12-client init [command options]

OPTIONS:
   --help, -h  show help
```

#### sptool toolbox mk12-client deal
```
NAME:
   sptool toolbox mk12-client deal - Make an online deal with Curio

USAGE:
   sptool toolbox mk12-client deal [command options]

OPTIONS:
   --http-url value                               http url to CAR file
   --http-headers value [ --http-headers value ]  http headers to be passed with the request (e.g key=value)
   --car-size value                               size of the CAR file: required for online deals (default: 0)
   --provider value                               storage provider on-chain address
   --commp value                                  commp of the CAR file
   --piece-size value                             size of the CAR file as a padded piece (default: 0)
   --payload-cid value                            root CID of the CAR file
   --start-epoch-head-offset value                start epoch by when the deal should be proved by provider on-chain after current chain head (default: 0)
   --start-epoch value                            start epoch by when the deal should be proved by provider on-chain (default: 0)
   --duration value                               duration of the deal in epochs (default: 518400)
   --provider-collateral value                    deal collateral that storage miner must put in escrow; if empty, the min collateral for the given piece size will be used (default: 0)
   --storage-price value                          storage price in attoFIL per epoch per GiB (default: 1)
   --verified                                     whether the deal funds should come from verified client data-cap (default: false)
   --remove-unsealed-copy                         indicates that an unsealed copy of the sector in not required for fast retrieval (default: false)
   --wallet value                                 wallet address to be used to initiate the deal
   --skip-ipni-announce                           indicates that deal index should not be announced to the IPNI(Network Indexer) (default: false)
   --http                                         make the deal over HTTP instead of libp2p (default: false)
   --help, -h                                     show help
```

#### sptool toolbox mk12-client deal-status
```
NAME:
   sptool toolbox mk12-client deal-status

USAGE:
   sptool toolbox mk12-client deal-status [command options]

OPTIONS:
   --provider value   storage provider on-chain address
   --deal-uuid value  
   --wallet value     the wallet address that was used to sign the deal proposal
   --http             make the deal over HTTP instead of libp2p (default: false)
   --help, -h         show help
```

#### sptool toolbox mk12-client offline-deal
```
NAME:
   sptool toolbox mk12-client offline-deal - Make an offline deal with Curio

USAGE:
   sptool toolbox mk12-client offline-deal [command options]

OPTIONS:
   --provider value                 storage provider on-chain address
   --commp value                    commp of the CAR file
   --piece-size value               size of the CAR file as a padded piece (default: 0)
   --payload-cid value              root CID of the CAR file
   --start-epoch-head-offset value  start epoch by when the deal should be proved by provider on-chain after current chain head (default: 0)
   --start-epoch value              start epoch by when the deal should be proved by provider on-chain (default: 0)
   --duration value                 duration of the deal in epochs (default: 518400)
   --provider-collateral value      deal collateral that storage miner must put in escrow; if empty, the min collateral for the given piece size will be used (default: 0)
   --storage-price value            storage price in attoFIL per epoch per GiB (default: 1)
   --verified                       whether the deal funds should come from verified client data-cap (default: false)
   --remove-unsealed-copy           indicates that an unsealed copy of the sector in not required for fast retrieval (default: false)
   --wallet value                   wallet address to be used to initiate the deal
   --skip-ipni-announce             indicates that deal index should not be announced to the IPNI(Network Indexer) (default: false)
   --http                           make the deal over HTTP instead of libp2p (default: false)
   --help, -h                       show help
```

#### sptool toolbox mk12-client allocate
```
NAME:
   sptool toolbox mk12-client allocate - Create new allocation[s] for verified deals

USAGE:
   sptool toolbox mk12-client allocate [command options]

DESCRIPTION:
   The command can accept a CSV formatted file in the format 'pieceCid,pieceSize,miner,tmin,tmax,expiration'

OPTIONS:
   --miner value, -m value, --provider value, -p value [ --miner value, -m value, --provider value, -p value ]  storage provider address[es]
   --piece-cid value, --piece value                                                                             data piece-cid to create the allocation
   --piece-size value, --size value                                                                             piece size to create the allocation (default: 0)
   --wallet value                                                                                               the wallet address that will used create the allocation
   --quiet                                                                                                      do not print the allocation list (default: false)
   --term-min value, --tmin value                                                                               The minimum duration which the provider must commit to storing the piece to avoid early-termination penalties (epochs).
      Default is 180 days. (default: 518400)
   --term-max value, --tmax value  The maximum period for which a provider can earn quality-adjusted power for the piece (epochs).
      Default is 5 years. (default: 5256000)
   --expiration value  The latest epoch by which a provider must commit data before the allocation expires (epochs).
      Default is 60 days. (default: 172800)
   --piece-file value, --pf value  file containing piece information to create the allocation. Each line in the file should be in the format 'pieceCid,pieceSize,miner,tmin,tmax,expiration'
   --batch-size value              number of extend requests per batch. If set incorrectly, this will lead to out of gas error (default: 500)
   --confidence value              number of block confirmations to wait for (default: 5)
   --assume-yes, -y, --yes         automatic yes to prompts; assume 'yes' as answer to all prompts and run non-interactively (default: false)
   --evm-client-contract value     f4 address of EVM contract to spend DataCap from
   --json, -j                      print output in JSON format (default: false)
   --help, -h                      show help
```

#### sptool toolbox mk12-client list-allocations
```
NAME:
   sptool toolbox mk12-client list-allocations - Lists all allocations for a client address(wallet)

USAGE:
   sptool toolbox mk12-client list-allocations [command options]

OPTIONS:
   --miner value, -m value, --provider value, -p value  Storage provider address. If provided, only allocations against this minerID will be printed
   --wallet value                                       the wallet address that will used create the allocation
   --json, -j                                           print output in JSON format (default: false)
   --help, -h                                           show help
```

#### sptool toolbox mk12-client market-add
```
NAME:
   sptool toolbox mk12-client market-add - Add funds to the Storage Market actor

USAGE:
   sptool toolbox mk12-client market-add [command options] <amount>

DESCRIPTION:
   Send signed message to add funds for the default wallet to the Storage Market actor. Uses 2x current BaseFee and a maximum fee of 1 nFIL. This is an experimental utility, do not use in production.

OPTIONS:
   --assume-yes, -y, --yes  automatic yes to prompts; assume 'yes' as answer to all prompts and run non-interactively (default: false)
   --wallet value           move balance from this wallet address to its market actor
   --help, -h               show help
```

#### sptool toolbox mk12-client market-withdraw
```
NAME:
   sptool toolbox mk12-client market-withdraw - Withdraw funds from the Storage Market actor

USAGE:
   sptool toolbox mk12-client market-withdraw [command options] <amount>

OPTIONS:
   --assume-yes, -y, --yes  automatic yes to prompts; assume 'yes' as answer to all prompts and run non-interactively (default: false)
   --wallet value           move balance to this wallet address from its market actor
   --help, -h               show help
```

#### sptool toolbox mk12-client commp
```
NAME:
   sptool toolbox mk12-client commp

USAGE:
   sptool toolbox mk12-client commp [command options] <inputPath>

OPTIONS:
   --help, -h  show help
```

#### sptool toolbox mk12-client generate-rand-car
```
NAME:
   sptool toolbox mk12-client generate-rand-car - creates a randomly generated dense car

USAGE:
   sptool toolbox mk12-client generate-rand-car [command options] <outputPath>

OPTIONS:
   --size value, -s value       The size of the data to turn into a car (default: 8000000)
   --chunksize value, -c value  Size of chunking that should occur (default: 512)
   --maxlinks value, -l value   Max number of leaves per level (default: 8)
   --help, -h                   show help
```

#### sptool toolbox mk12-client wallet
```
NAME:
   sptool toolbox mk12-client wallet - Manage mk12 client wallets

USAGE:
   sptool toolbox mk12-client wallet command [command options]

COMMANDS:
   new                   Generate a new key of the given type
   list                  List wallet address
   balance               Get account balance
   export                export keys
   import                import keys
   default, get-default  Get default wallet address
   set-default           Set default wallet address
   delete                Delete an account from the wallet
   sign                  Sign a message
   help, h               Shows a list of commands or help for one command

OPTIONS:
   --help, -h  show help
```

##### sptool toolbox mk12-client wallet new
```
NAME:
   sptool toolbox mk12-client wallet new - Generate a new key of the given type

USAGE:
   sptool toolbox mk12-client wallet new [command options] [bls|secp256k1|delegated (default secp256k1)]

OPTIONS:
   --help, -h  show help
```

##### sptool toolbox mk12-client wallet list
```
NAME:
   sptool toolbox mk12-client wallet list - List wallet address

USAGE:
   sptool toolbox mk12-client wallet list [command options]

OPTIONS:
   --addr-only, -a  Only print addresses (default: false)
   --id, -i         Output ID addresses (default: false)
   --help, -h       show help
```

##### sptool toolbox mk12-client wallet balance
```
NAME:
   sptool toolbox mk12-client wallet balance - Get account balance

USAGE:
   sptool toolbox mk12-client wallet balance [command options] [address]

OPTIONS:
   --help, -h  show help
```

##### sptool toolbox mk12-client wallet export
```
NAME:
   sptool toolbox mk12-client wallet export - export keys

USAGE:
   sptool toolbox mk12-client wallet export [command options] [address]

OPTIONS:
   --help, -h  show help
```

##### sptool toolbox mk12-client wallet import
```
NAME:
   sptool toolbox mk12-client wallet import - import keys

USAGE:
   sptool toolbox mk12-client wallet import [command options] [<path> (optional, will read from stdin if omitted)]

OPTIONS:
   --format value  specify input format for key (default: "hex-lotus")
   --as-default    import the given key as your new default key (default: false)
   --help, -h      show help
```

##### sptool toolbox mk12-client wallet default
```
NAME:
   sptool toolbox mk12-client wallet default - Get default wallet address

USAGE:
   sptool toolbox mk12-client wallet default [command options]

OPTIONS:
   --help, -h  show help
```

##### sptool toolbox mk12-client wallet set-default
```
NAME:
   sptool toolbox mk12-client wallet set-default - Set default wallet address

USAGE:
   sptool toolbox mk12-client wallet set-default [command options] [address]

OPTIONS:
   --help, -h  show help
```

##### sptool toolbox mk12-client wallet delete
```
NAME:
   sptool toolbox mk12-client wallet delete - Delete an account from the wallet

USAGE:
   sptool toolbox mk12-client wallet delete [command options] <address> 

OPTIONS:
   --help, -h  show help
```

##### sptool toolbox mk12-client wallet sign
```
NAME:
   sptool toolbox mk12-client wallet sign - Sign a message

USAGE:
   sptool toolbox mk12-client wallet sign [command options] <signing address> <hexMessage>

OPTIONS:
   --help, -h  show help
```
