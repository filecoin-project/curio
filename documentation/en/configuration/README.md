---
description: How to edit and manage configuration for a Curio cluster
---

# Configuration

## Configuration

The configuration for Curio is stored in the HarmonyDB in a table called `harmony_config`. When a Curio node is started, one or more layer names are supplied to get the desired configuration for the node.

### Configuration Layers

Configuration layers provide a set of configuration parameters that determine how a system will operate. These layers can be defined at different levels, meaning that higher layer can override the lower layer, and the system will behave based on the final stacked output.

Configuration layers can be arranged in a hierarchy, often from `base`(most general) to most specific. The `base` layer defines default configuration values. More specific layers override these defaults with more targeted configurations.

For example, in a simple two-layer configuration system, layers could be organized in the following order: Base Layer - This is the most general layer. It is always included, so add any modifications to defaults here. If you include all your miner IDs here (defined in the addresses section) all hardware will be used for all miner needs. Task Layer - This layer enables SDR tasks. Consider the included layers (below).

If a Curio node is started with above 2 layers then it will perform SDR tasks for all miner IDs and will use default values of any other configuration parameter.

```
Example Use of Layers:

curio run --layers=post
```

{% hint style="warning" %}
`base` layer is always applied by default when a Curio node is started.
{% endhint %}

#### Advantages of Configuration Layers

Flexibility: Configuration layers allow different parts of the system or different users to behave differently according to predefined settings. Scalability: By separating concerns and allowing for specific configuration, the systems become easier to manage as they scale. Maintainability: Changes to configuration can be made on an appropriate layer without affecting the entire system.

#### Layer Stacking

The configuration layers are stack in the supplied order. The `base` layer is always applied by default so you don't need to specify it.

For example, if a Curio node is started with the following layers:

```
--layers miner1,sdr,wdPost,pricing
```

These layer will be stacked on top of each other to create the final configuration for the node. The order of stacking will base > miner1 > sdr > wdPost > pricing. If a configuration parameter is defined in multiple layers then the final layer value will be used.

#### Working with layers

Curio allows you to manage node configurations using layers. Each layer can be applied or modified independently, with the ‘base’ layer being essential at startup.

**Print default configuration**

The default configuration is used in base layer by default.

```shell
curio config default
```

**Adding a New Layer**

To add a new configuration layer or update an existing one, you can provide a filename or input directly via stdin.

```shell
curio config set --title <stdin/Filename>
```

**List all layers**

List all configuration layers present in the database.

```shell
curio config ls
```

**Editing a Layer**

Directly edit a configuration layer.

*   Edit with `vim` editor\\

    ```shell
    curio config edit --editor vim <layer name>
    ```
*   Edit with a different editor like nano\\

    ```shell
    curio config edit --editor nano <layer name>
    ```

**Interpreting Stacked Layers**

Interpret and view the combined effect of all applied configuration layers, including system-generated comments.

```shell
curio config view --layers [layer1,layers2]
```

**Removing a Layer**

Remove a specific configuration layer by name.

```shell
curio config rm <layer name>
```

#### Pre-built Layers

When the first Curio miner is initialized or when the first Lotus-Miner is migrated to Curio, the process creates some layers by default for the users. These layers mostly define if a particular task should be picked by the machine or not.

**post**

```toml

[Subsystems]
EnableWindowPost = true
EnableWinningPost = true
```

**sdr**

```toml
[Subsystems]
EnableSealSDR = true
```

**seal**

```toml
[Subsystems]
EnableSealSDR = true
EnableSealSDRTrees = true
EnableSendPrecommitMsg = true
EnablePoRepProof = true
EnableSendCommitMsg = true
EnableMoveStorage = true
```

**seal-gpu**

```toml
[Subsystems]
EnableSealSDRTrees = true
EnableSendPrecommitMsg = true
```

**seal-snark**

```toml
[Subsystems]
EnablePoRepProof = true
EnableSendCommitMsg = true
```

**gui**

```toml
[Subsystems]
EnableWebGui = true
```

### Configuration management in UI

The Curio GUI provides a user-friendly interface for managing configurations. To access this feature, navigate to the “Configurations” page from the UI menu. On this page, all available layers in the database are listed. Users can edit each layer by clicking on it.

<figure><img src="../../zh/.gitbook/assets/config.png" alt="Configurations"><figcaption><p>Curio GUI configuration page</p></figcaption></figure>

To update a configuration field, users must first enable it by checking the corresponding box. After enabling, the field value can be populated. To comment out the field, simply uncheck the box.

<figure><img src="../../zh/.gitbook/assets/config-edit.png" alt="Configuration edit"><figcaption><p>Curio GUI configuration editor</p></figcaption></figure>

### Dynamic Configuration Updates

Historically, **all configuration changes required a full server restart**, impacting uptime and SLA.\
This is no longer the case — **a growing number of settings can now be updated on the fly**, without restarting any node. More dynamic options will be added over time.

**Identifying Hot-Reloadable Settings**

In the UI and documentation, these settings are clearly marked with:

> **“Updates will affect running instances”**

When updated (via UI, CLI, or direct SQL), they are applied automatically within **\~30 seconds**.

#### **Behaviour & Caveats**

* **Invalid values apply immediately** (e.g., unparsable or out-of-range values) and may cause system-wide disruption.\
  They can be corrected by pushing a valid update, but there is **no safety delay or rollback**.
* A few configuration changes still **require a restart** — typically structural or first-time operations\
  (e.g., adding the first miner address while Market 2 is active).\
  In these cases the system will **clearly return an error stating that a restart is required**.

#### **Quick Reference**

| Type of Change                                          | Restart Needed?  | Notes                                       |
| ------------------------------------------------------- | ---------------- | ------------------------------------------- |
| Marked with **“Updates will affect running instances”** | ❌ No             | Takes effect in \~30s                       |
| Bad or invalid value                                    | ❌ No, but unsafe | Applies instantly, may break services       |
| Structural / unsupported change (rare)                  | ✅ Yes            | System explicitly errors and refuses update |

This gradual shift to dynamic configuration reduces downtime, shortens maintenance windows, and improves operational flexibility.
