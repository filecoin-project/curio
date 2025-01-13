# curio
NAME:
   curio - Filecoin 去中心化存储网络提供商

USAGE:
   curio [全局选项] 命令 [命令选项] [参数...]

VERSION:
   1.23.0

COMMANDS:
   cli           执行 CLI 命令
   run           启动 Curio 进程
   config        按层管理节点配置。'base' 层将始终在 Curio 启动时应用。
   test          测试的实用功能
   web           启动 Curio 网页界面
   guided-setup  运行引导式设置，用于从 lotus-miner 迁移到 Curio 或创建新的 Curio 矿工
   seal          管理封装流程
   market        
   fetch-params  获取证明参数
   calc          数学工具
   help, h       显示命令列表或某个命令的帮助

GLOBAL OPTIONS:
   --color              在显示输出中使用颜色（默认：取决于输出是否为 TTY）
   --db-host value      Yugabyte 集群的主机名列表，用逗号分隔（默认："127.0.0.1"）[$CURIO_DB_HOST, $CURIO_HARMONYDB_HOSTS]
   --db-name value      （默认："yugabyte"）[$CURIO_DB_NAME, $CURIO_HARMONYDB_NAME]
   --db-user value      （默认："yugabyte"）[$CURIO_DB_USER, $CURIO_HARMONYDB_USERNAME]
   --db-password value  （默认："yugabyte"）[$CURIO_DB_PASSWORD, $CURIO_HARMONYDB_PASSWORD]
   --db-port value      （默认："5433"）[$CURIO_DB_PORT, $CURIO_HARMONYDB_PORT]
   --repo-path value    （默认："~/.curio"）[$CURIO_REPO_PATH]
   --vv                 启用非常详细的模式，用于调试 CLI（默认：false）
   --help, -h           显示帮助
   --version, -v        打印版本



## curio cli


NAME:
   curio cli - 执行 CLI 命令

USAGE:
   curio cli 命令 [命令选项] [参数...]

COMMANDS:
   storage   管理扇区存储
   log       管理日志
   wait-api  等待 Curio API 上线
   stop      停止正在运行的 Curio 进程
   help, h   显示命令列表或某个命令的帮助

OPTIONS:
   --machine value  机器主机:端口（curio run --listen 地址）
   --help, -h       显示帮助


### curio cli storage

NAME:
   curio cli storage - 管理扇区存储

USAGE:
   curio cli storage 命令 [命令选项] [参数...]

DESCRIPTION:
   扇区可以存储在多个文件系统路径中。这些
   命令提供了管理 Curio 节点用于长期存储扇区以进行证明的存储（称为 'store'）
   以及扇区在封装流程中如何存储（称为 'seal'）的方法。

COMMANDS:
   attach   附加本地存储路径
   detach   分离本地存储路径
   list     列出本地存储路径
   find     在存储系统中查找扇区
   help, h  显示命令列表或某个命令的帮助

OPTIONS:
   --help, -h  显示帮助


#### curio cli storage attach

NAME:
   curio cli storage attach - 附加本地存储路径

USAGE:
   curio cli storage attach [命令选项] [路径]

DESCRIPTION:
   可以使用此命令将存储附加到 Curio 节点。存储卷
   列表存储在 curio run 中设置的 storage.json 中，位于 Curio 节点本地。我们不
   建议在不进一步了解存储系统的情况下手动修改此值。

   每个存储卷都包含一个描述卷
   功能的配置文件。当提供 '--init' 标志时，将使用
   附加标志创建此文件。

   权重
   较高的权重值意味着数据更有可能存储在此路径中

   封装
   封装过程的数据将存储在这里

   存储
   最终确定的扇区将被移动到这里进行长期存储，并随时间
   进行证明
      

OPTIONS:
   --init                                 首先初始化路径（默认：false）
   --weight value                         （用于初始化）路径权重（默认：10）
   --seal                                 （用于初始化）将路径用于封装（默认：false）
   --store                                （用于初始化）将路径用于长期存储（默认：false）
   --max-storage value                    （用于初始化）限制扇区的存储空间（对于非常大的路径来说代价很高！）
   --groups value [ --groups value ]      路径组名称
   --allow-to value [ --allow-to value ]  允许从此路径拉取数据的路径组（如果未指定则允许所有）
   --help, -h                             显示帮助


#### curio cli storage detach

NAME:
   curio cli storage detach - 分离本地存储路径

USAGE:
   curio cli storage detach [命令选项] [路径]

OPTIONS:
   --really-do-it  （默认：false）
   --help, -h      显示帮助


#### curio cli storage list

NAME:
   curio cli storage list - 列出本地存储路径

USAGE:
   curio cli storage list [命令选项] [参数...]

OPTIONS:
   --local     仅列出本地存储路径（默认：false）
   --help, -h  显示帮助


#### curio cli storage find

NAME:
   curio cli storage find - 在存储系统中查找扇区

USAGE:
   curio cli storage find [命令选项] [矿工地址] [扇区编号]

OPTIONS:
   --help, -h  显示帮助


### curio cli log

NAME:
   curio cli log - 管理日志

USAGE:
   curio cli log 命令 [命令选项] [参数...]

COMMANDS:
   list       列出日志系统
   set-level  设置日志级别
   help, h    显示命令列表或某个命令的帮助

OPTIONS:
   --help, -h  显示帮助


#### curio cli log list

NAME:
   curio cli log list - 列出日志系统

USAGE:
   curio cli log list [命令选项] [参数...]

OPTIONS:
   --help, -h  显示帮助


#### curio cli log set-level

NAME:
   curio cli log set-level - 设置日志级别

USAGE:
   curio cli log set-level [命令选项] [级别]

DESCRIPTION:
   为日志系统设置日志级别：

     系统标志可以多次指定。

     例如）log set-level --system chain --system chainxchg debug

     可用级别：
     debug
     info
     warn
     error

     环境变量：
     GOLOG_LOG_LEVEL - 所有日志系统的默认日志级别
     GOLOG_LOG_FMT   - 更改输出日志格式（json，nocolor）
     GOLOG_FILE      - 将日志写入文件
     GOLOG_OUTPUT    - 指定是否输出到文件、stderr、stdout 或组合，例如 file+stderr


OPTIONS:
   --system value [ --system value ]  限制到日志系统
   --help, -h                         显示帮助
### curio cli wait-api

NAME:
   curio cli wait-api - 等待 Curio API 上线

USAGE:
   curio cli wait-api [command options] [arguments...]

OPTIONS:
   --timeout value  等待失败的持续时间（默认：30s）
   --help, -h       显示帮助


### curio cli stop

NAME:
   curio cli stop - 停止正在运行的 Curio 进程

USAGE:
   curio cli stop [command options] [arguments...]

OPTIONS:
   --help, -h  显示帮助


## curio run

NAME:
   curio run - 启动 Curio 进程

USAGE:
   curio run [command options] [arguments...]

OPTIONS:
   --listen value                                                                       工作者 API 将监听的主机地址和端口（默认："0.0.0.0:12300"）[$CURIO_LISTEN]
   --nosync                                                                             不检查全节点同步状态（默认：false）
   --manage-fdlimit                                                                     管理打开文件限制（默认：true）
   --layers value, -l value, --layer value [ --layers value, -l value, --layer value ]  要解释的层列表（在默认值之上）。默认：base [$CURIO_LAYERS]
   --name value                                                                         自定义节点名称 [$CURIO_NODE_NAME]
   --help, -h                                                                           显示帮助


## curio config

NAME:
   curio config - 通过层管理节点配置。'base' 层将始终在 Curio 启动时应用。

USAGE:
   curio config command [command options] [arguments...]

COMMANDS:
   default, defaults                打印默认节点配置
   set, add, update, create         通过提供文件名或标准输入来设置配置层或基础层。
   get, cat, show                   按名称获取配置层。您可能想将输出管道到文件，或使用 'less'
   list, ls                         列出数据库中存在的配置层。
   interpret, view, stacked, stack  通过此版本的 curio 解释堆叠的配置层，并带有系统生成的注释。
   remove, rm, del, delete          删除指定的配置层。
   edit                             编辑配置层
   new-cluster                      为新集群创建新配置
   help, h                          显示命令列表或某个命令的帮助

OPTIONS:
   --help, -h  显示帮助


### curio config default

NAME:
   curio config default - 打印默认节点配置

USAGE:
   curio config default [command options] [arguments...]

OPTIONS:
   --no-comment  不注释默认值（默认：false）
   --help, -h    显示帮助


### curio config set

NAME:
   curio config set - 通过提供文件名或标准输入来设置配置层或基础层。

USAGE:
   curio config set [command options] 层的文件名

OPTIONS:
   --title value  配置层的标题（对于标准输入是必需的）
   --help, -h     显示帮助


### curio config get

NAME:
   curio config get - 按名称获取配置层。您可能想将输出管道到文件，或使用 'less'

USAGE:
   curio config get [command options] 层名称

OPTIONS:
   --help, -h  显示帮助


### curio config list

NAME:
   curio config list - 列出数据库中存在的配置层。

USAGE:
   curio config list [command options] [arguments...]

OPTIONS:
   --help, -h  显示帮助


### curio config interpret

NAME:
   curio config interpret - 通过此版本的 curio 解释堆叠的配置层，并带有系统生成的注释。

USAGE:
   curio config interpret [command options] 要解释为最终配置的层列表

OPTIONS:
   --layers value [ --layers value ]  要解释的层的逗号或空格分隔列表（base 总是应用）
   --help, -h                         显示帮助


### curio config remove

NAME:
   curio config remove - 删除指定的配置层。

USAGE:
   curio config remove [command options] [arguments...]

OPTIONS:
   --help, -h  显示帮助


### curio config edit

NAME:
   curio config edit - 编辑配置层

USAGE:
   curio config edit [command options] [层名称]

OPTIONS:
   --editor value         要使用的编辑器（默认："vim"）[$EDITOR]
   --source value         源配置层（默认：<编辑的层>）
   --allow-overwrite      如果源是不同的层，允许覆盖现有层（默认：false）
   --no-source-diff       将整个配置保存到层中，而不仅仅是差异（默认：false）
   --no-interpret-source  不解释源层（如果设置了 --source，默认为 true）
   --help, -h             显示帮助


### curio config new-cluster

NAME:
   curio config new-cluster - 为新集群创建新配置

USAGE:
   curio config new-cluster [command options] [SP actor 地址...]

OPTIONS:
   --help, -h  显示帮助


## curio test

NAME:
   curio test - 用于测试的实用功能

USAGE:
   curio test command [command options] [arguments...]

COMMANDS:
   window-post, wd, windowpost, wdpost  为扇区计算时空证明（需要预先密封扇区）。这些不会发送到链上。
   help, h                              显示命令列表或某个命令的帮助

OPTIONS:
   --help, -h  显示帮助


### curio test window-post
```yaml
NAME:
   curio test window-post - Compute a proof-of-spacetime for a sector (requires the sector to be pre-sealed). These will not send to the chain.

USAGE:
   curio test window-post command [command options] [arguments...]

COMMANDS:
   here, cli                                       Compute WindowPoSt for performance and configuration testing.
   task, scheduled, schedule, async, asynchronous  Test the windowpost scheduler by running it on the next available curio. If tasks fail all retries, you will need to ctrl+c to exit.
   help, h                                         Shows a list of commands or help for one command

OPTIONS:
   --help, -h  show help
```

#### curio test window-post here
```yaml
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
```yaml
NAME:
   curio test window-post task - Test the windowpost scheduler by running it on the next available curio. If tasks fail all retries, you will need to ctrl+c to exit.

USAGE:
   curio test window-post task [command options] [arguments...]

OPTIONS:
   --deadline value                   deadline to compute WindowPoSt for  (default: 0)
   --layers value [ --layers value ]  list of layers to be interpreted (atop defaults). Default: base
   --help, -h                         show help
```

## curio web
```yaml
NAME:
   curio web - Start Curio web interface

USAGE:
   curio web [command options] [arguments...]

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
```yaml
NAME:
   curio guided-setup - Run the guided setup for migrating from lotus-miner to Curio or Creating a new Curio miner

USAGE:
   curio guided-setup [command options] [arguments...]

OPTIONS:
   --help, -h  show help
```

## curio seal
```yaml
NAME:
   curio seal - Manage the sealing pipeline

USAGE:
   curio seal command [command options] [arguments...]

COMMANDS:
   start    Start new sealing operations manually
   help, h  Shows a list of commands or help for one command

OPTIONS:
   --help, -h  show help
```

### curio seal start
```yaml
NAME:
   curio seal start - Start new sealing operations manually

USAGE:
   curio seal start [command options] [arguments...]

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

## curio market
```yaml
NAME:
   curio market

USAGE:
   curio market command [command options] [arguments...]

COMMANDS:
   rpc-info  
   seal      start sealing a deal sector early
   help, h   Shows a list of commands or help for one command

OPTIONS:
   --help, -h  show help
```

### curio market rpc-info
```yaml
NAME:
   curio market rpc-info

USAGE:
   curio market rpc-info [command options] [arguments...]

OPTIONS:
   --layers value [ --layers value ]  list of layers to be interpreted (atop defaults). Default: base
   --help, -h                         show help
```

### curio market seal
```yaml
NAME:
   curio market seal - start sealing a deal sector early

USAGE:
   curio market seal [command options] <sector>

OPTIONS:
   --actor value  Specify actor address to start sealing sectors for
   --synthetic    Use synthetic PoRep (default: false)
   --help, -h     show help
```

## curio fetch-params
```yaml
NAME:
   curio fetch-params - Fetch proving parameters

USAGE:
   curio fetch-params [command options] [sectorSize]

OPTIONS:
   --help, -h  show help
```

## curio calc
```yaml
NAME:
   curio calc - Math Utils

USAGE:
   curio calc command [command options] [arguments...]

COMMANDS:
   batch-cpu         Analyze and display the layout of batch sealer threads
   supraseal-config  Generate a supra_seal configuration
   help, h           Shows a list of commands or help for one command

OPTIONS:
   --actor value  
   --help, -h     show help
```

### curio calc batch-cpu
```yaml
NAME:
   curio calc batch-cpu - Analyze and display the layout of batch sealer threads

USAGE:
   curio calc batch-cpu [command options] [arguments...]

DESCRIPTION:
   Analyze and display the layout of batch sealer threads on your CPU.

   It provides detailed information about CPU utilization for batch sealing operations, including core allocation, thread
   distribution for different batch sizes.

OPTIONS:
   --dual-hashers  (default: true)
   --help, -h      show help
```

### curio calc supraseal-config
```yaml
NAME:
   curio calc supraseal-config - Generate a supra_seal configuration

USAGE:
   curio calc supraseal-config [command options] [arguments...]

DESCRIPTION:
   Generate a supra_seal configuration for a given batch size.

   This command outputs a configuration expected by SupraSeal. Main purpose of this command is for debugging and testing.
   The config can be used directly with SupraSeal binaries to test it without involving Curio.

OPTIONS:
   --dual-hashers                Zen3 and later supports two sectors per thread, set to false for older CPUs (default: true)
   --batch-size value, -b value  (default: 0)
   --help, -h                    show help
```
