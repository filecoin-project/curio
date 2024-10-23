# sptool 工具

名称:
   sptool - 管理 Filecoin 矿工参与者

用法:
   sptool [全局选项] 命令 [命令选项] [参数...]

版本:
   1.23.0

命令:
   actor    管理 Filecoin 矿工参与者元数据
   info     打印矿工参与者信息
   sectors  与扇区存储交互
   proving  查看证明信息
   help, h  显示命令列表或某个命令的帮助

全局选项:
   --log-level value  (默认: "info")
   --actor value      要管理的矿工参与者 [$SP_ADDRESS]
   --help, -h         显示帮助
   --version, -v      打印版本

## sptool actor 参与者

名称:
   sptool actor - 管理Filecoin矿工Actor元数据

用法:
   sptool actor 命令 [命令选项] [参数...]

命令:
   set-addresses, set-addrs    设置您的矿工可以公开拨号的地址
   withdraw                    将可用余额提取到受益人
   repay-debt                  偿还矿工的债务
   set-peer-id                 设置您矿工的对等ID
   set-owner                   设置所有者地址（此命令应该被调用两次，首先使用旧所有者作为发送者地址，然后使用新所有者）
   control                     管理控制地址
   propose-change-worker       提议更改工作者地址
   confirm-change-worker       确认工作者地址更改
   compact-allocated           压缩已分配的扇区位域
   propose-change-beneficiary  提议更改受益人地址
   confirm-change-beneficiary  确认受益人地址更改
   new-miner                   初始化新的矿工actor
   help, h                     显示命令列表或某个命令的帮助

选项:
   --help, -h  显示帮助

### sptool actor set-addresses 设置地址

名称:
   sptool actor set-addresses - 设置您的矿工可以公开拨号的地址

用法:
   sptool actor set-addresses [命令选项] <多地址>

选项:
   --from value       可选择指定发送消息的账户
   --gas-limit value  设置燃气限制 (默认: 0)
   --unset            取消设置地址 (默认: false)
   --help, -h         显示帮助

### sptool actor withdraw 提现

名称:
   sptool actor withdraw - 将可用余额提取到受益人

用法:
   sptool actor withdraw [命令选项] [金额 (FIL)]

选项:
   --confidence value  等待的区块确认数 (默认: 5)
   --beneficiary       从受益人地址发送提现消息 (默认: false)
   --help, -h          显示帮助

### sptool actor repay-debt 偿还债务
名称:
   sptool actor repay-debt - 偿还矿工的债务

用法:
   sptool actor repay-debt [命令选项] [金额 (FIL)]

选项:
   --from value  可选择指定发送资金的账户
   --help, -h    显示帮助
### sptool actor set-peer-id 设置对等ID

名称:
   sptool actor set-peer-id - 设置您矿工的对等ID

用法:
   sptool actor set-peer-id [命令选项] <对等ID>

选项:
   --gas-limit value  设置燃气限制 (默认: 0)
   --help, -h         显示帮助


### sptool actor set-owner 设置所有者

名称:
   sptool actor set-owner - 设置所有者地址（此命令应该被调用两次，首先使用旧所有者作为发送者地址，然后使用新所有者）

用法:
   sptool actor set-owner [命令选项] [新所有者地址 发送者地址]

选项:
   --really-do-it  实际发送执行操作的交易 (默认: false)
   --help, -h      显示帮助


### sptool actor control 管理控制

名称:
   sptool actor control - 管理控制地址

用法:
   sptool actor control 命令 [命令选项] [参数...]

命令:
   list     获取当前设置的控制地址。注意：这不包括大多数角色，因为它们在即时链状态中是未知的。
   set      设置控制地址
   help, h  显示命令列表或某个命令的帮助

选项:
   --help, -h  显示帮助


#### sptool actor control list 获取控制地址

名称:
   sptool actor control list - 获取当前设置的控制地址。注意：这不包括大多数角色，因为它们在即时链状态中是未知的。

用法:
   sptool actor control list [命令选项] [参数...]

选项:
   --verbose   (默认: false)
   --help, -h  显示帮助


#### sptool actor control set 设置控制地址

名称:
   sptool actor control set - 设置控制地址

用法:
   sptool actor control set [命令选项] [...地址]

选项:
   --really-do-it  实际发送执行操作的交易 (默认: false)
   --help, -h      显示帮助


### sptool actor propose-change-worker 提议更改工作者

名称:
   sptool actor propose-change-worker - 提议更改工作者地址

用法:
   sptool actor propose-change-worker [命令选项] [地址]

选项:
   --really-do-it  实际发送执行操作的交易 (默认: false)
   --help, -h      显示帮助


### sptool actor confirm-change-worker 确认更改工作者

名称:
   sptool actor confirm-change-worker - 确认工作者地址更改

用法:
   sptool actor confirm-change-worker [命令选项] [地址]

选项:
   --really-do-it  实际发送执行操作的交易 (默认: false)
   --help, -h      显示帮助


### sptool actor compact-allocated 压缩已分配

名称:
   sptool actor compact-allocated - 压缩已分配的扇区位域

用法:
   sptool actor compact-allocated [命令选项] [参数...]

选项:
   --mask-last-offset value  从0到'最高分配 - 偏移量'掩蔽扇区ID (默认: 0)
   --mask-upto-n value       从0到'n'掩蔽扇区ID (默认: 0)
   --really-do-it            实际发送执行操作的交易 (默认: false)
   --help, -h                显示帮助

### sptool actor propose-change-beneficiary 提议更改受益人

名称:
   sptool actor propose-change-beneficiary - 提议更改受益人地址

用法:
   sptool actor propose-change-beneficiary [命令选项] [受益人地址 配额 过期时间]

选项:
   --really-do-it              实际发送执行操作的交易 (默认: false)
   --overwrite-pending-change  覆盖当前的受益人更改提议 (默认: false)
   --actor value               指定矿工角色的地址
   --help, -h                  显示帮助

### sptool actor confirm-change-beneficiary 确认更改受益人

名称:
   sptool actor confirm-change-beneficiary - 确认受益人地址变更

用法:
   sptool actor confirm-change-beneficiary [命令选项] [矿工ID]

选项:
   --really-do-it          实际发送执行操作的交易 (默认: false)
   --existing-beneficiary  从现有受益人地址发送确认 (默认: false)
   --new-beneficiary       从新受益人地址发送确认 (默认: false)
   --help, -h              显示帮助


### sptool actor new-miner 初始化新的矿工

名称:
   sptool actor new-miner - 初始化新的矿工actor

用法:
   sptool actor new-miner [命令选项] [参数...]

选项:
   --worker value, -w value  用于新矿工初始化的worker密钥
   --owner value, -o value   用于新矿工初始化的owner密钥
   --from value, -f value    发送actor(矿工)创建消息的地址
   --sector-size value       指定用于新矿工初始化的扇区大小
   --confidence value        等待的区块确认数 (默认: 5)
   --help, -h                显示帮助


## sptool info 打印矿工信息

名称:
   sptool info - 打印矿工actor信息

用法:
   sptool info [命令选项] [参数...]

选项:
   --help, -h  显示帮助


## sptool sectors 与扇区存储交互

名称:
   sptool sectors - 与扇区存储交互

用法:
   sptool sectors 命令 [命令选项] [参数...]

命令:
   status              通过扇区编号获取扇区的密封状态
   list                列出扇区
   precommits          打印链上预提交信息
   check-expire        检查即将到期的扇区
   expired             获取或清理已过期的扇区
   extend              延长即将到期的扇区，但不超过每个扇区的最大生命周期
   terminate           强制终止扇区（警告：这意味着失去算力并为终止的扇区支付一次性终止罚金（包括抵押品））
   compact-partitions  从分区中移除死亡扇区，并尽可能减少使用的分区数量
   help, h             显示命令列表或某个命令的帮助

选项:
   --help, -h  显示帮助


### sptool sectors status 获取扇区状态

名称:
   sptool sectors status - 通过扇区编号获取扇区的密封状态

用法:
   sptool sectors status [命令选项] <扇区编号>

选项:
   --log, -l             显示事件日志 (默认: false)
   --on-chain-info, -c   显示扇区的链上信息 (默认: false)
   --partition-info, -p  显示分区相关信息 (默认: false)
   --proof               以十六进制打印snark证明字节 (默认: false)
   --help, -h            显示帮助


### sptool sectors list 列出扇区

名称:
   sptool sectors list - 列出扇区

用法:
   sptool sectors list [命令选项] [参数...]

选项:
   --help, -h  显示帮助


### sptool sectors precommits 打印预提交信息

名称:
   sptool sectors precommits - 打印链上预提交信息

用法:
   sptool sectors precommits [命令选项] [参数...]

选项:
   --help, -h  显示帮助


### sptool sectors check-expire 检查扇区到期

名称:
   sptool sectors check-expire - 检查即将到期的扇区

用法:
   sptool sectors check-expire [命令选项] [参数...]

选项:
   --cutoff value  跳过当前到期时间距离现在超过<cutoff>个纪元的扇区，默认为60天 (默认: 172800)
   --help, -h      显示帮助


### sptool sectors expired 获取或清理已过期扇区

名称:
   sptool sectors expired - 获取或清理已过期的扇区

用法:
   sptool sectors expired [命令选项] [参数...]

选项:
   --expired-epoch value  检查扇区到期的纪元 (默认: WinningPoSt回溯纪元)
   --help, -h             显示帮助


### sptool sectors extend 延长扇区

名称:
   sptool sectors extend - 延长即将到期的扇区，但不超过每个扇区的最大生命周期

用法:
   sptool sectors extend [命令选项] <扇区编号...(可选)>

选项:
   --from value            仅考虑当前到期纪元在[from, to]范围内的扇区，<from>默认为：现在 + 120 (1小时) (默认: 0)
   --to value              仅考虑当前到期纪元在[from, to]范围内的扇区，<to>默认为：现在 + 92160 (32天) (默认: 0)
   --sector-file value     提供一个文件，每行包含一个扇区编号，忽略上述选择标准
   --exclude value         可选提供一个包含要排除的扇区的文件
   --extension value       尝试将选定的扇区延长这个纪元数，默认为540天 (默认: 1555200)
   --new-expiration value  尝试将选定的扇区延长到这个纪元，忽略extension (默认: 0)
   --only-cc               仅延长CC扇区（对于准备扇区进行快照升级很有用） (默认: false)
   --drop-claims           为可以延长但只能通过放弃一些验证算力声明的扇区放弃声明 (默认: false)
   --tolerance value       不尝试延长少于这个纪元数的扇区，默认为7天 (默认: 20160)
   --max-fee value         为一条消息最多使用这么多FIL。传递此标志以避免消息拥堵。 (默认: "0")
   --max-sectors value     每条消息包含的最大扇区数 (默认: 0)
   --really-do-it          传递此标志以真正延长扇区，否则只会打印参数的json表示 (默认: false)
   --help, -h              显示帮助

### sptool sectors terminate 强制终止扇区

名称:
   sptool sectors terminate - 强制终止扇区（警告：这意味着失去算力并为终止的扇区支付一次性终止罚金（包括抵押品））

用法:
   sptool sectors terminate [命令选项] [扇区编号1 扇区编号2 ...]

选项:
   --actor value   指定矿工actor的地址
   --really-do-it  如果你知道你在做什么，请传递此标志 (默认: false)
   --from value    指定发送终止消息的地址
   --help, -h      显示帮助

### sptool sectors compact-partitions 压缩分区

名称:
   sptool sectors compact-partitions - 从分区中移除死亡扇区，并尽可能减少使用的分区数量

用法:
   sptool sectors compact-partitions [命令选项] [参数...]

选项:
   --deadline value                           要压缩分区的截止时间 (默认: 0)
   --partitions value [ --partitions value ]  要压缩扇区的分区列表
   --really-do-it                             实际发送执行操作的交易 (默认: false)
   --help, -h                                 显示帮助

## sptool proving 查看证明信息

名称:
   sptool proving - 查看证明信息

用法:
   sptool proving 命令 [命令选项] [参数...]

命令:
   info       查看当前状态信息
   deadlines  查看当前证明期限的截止时间信息
   deadline   通过索引查看当前证明期限的截止时间信息
   faults     查看当前已知的证明故障扇区信息
   help, h    显示命令列表或某个命令的帮助

选项:
   --help, -h  显示帮助

### sptool proving info 查看状态信息

名称:
   sptool proving info - 查看当前状态信息

用法:
   sptool proving info [命令选项] [参数...]

选项:
   --help, -h  显示帮助

### sptool proving deadlines 查看证明期限

名称:
   sptool proving deadlines - 查看当前证明期限的截止时间信息

用法:
   sptool proving deadlines [命令选项] [参数...]

选项:
   --all, -a   计算所有扇区（默认只计算活跃扇区） (默认: false)
   --help, -h  显示帮助

### sptool proving deadline 查看证明期限索引

名称:
   sptool proving deadline - 通过索引查看当前证明期限的截止时间信息

用法:
   sptool proving deadline [命令选项] <截止时间索引>

选项:
   --sector-nums, -n  打印属于此截止时间的扇区/故障编号 (默认: false)
   --bitfield, -b     打印分区位域统计信息 (默认: false)
   --help, -h         显示帮助

### sptool proving faults 查看证明故障

名称:
   sptool proving faults - 查看当前已知的证明故障扇区信息

用法:
   sptool proving faults [命令选项] [参数...]

选项:
   --help, -h  显示帮助
