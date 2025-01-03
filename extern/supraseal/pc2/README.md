# Pre-Commit 2

The Pre-Commit 2 (PC2) phase generates the Poseidon based merkle trees over the columns in the graph and the replica. 

## Intended Usage

The SupraSeal PC2 functions generate Tree C files, Tree R files, Comm R, and the Replica. The Tree C files are always generated in parallel using the graph layers in NVME. If the sectors are CC sectors, then everything else is generated locally as well using the graph layers. There are options the application can utilize depending on if non-CC sectors have the data local to the sealing operation or are stored remotely.

```mermaid
flowchart TD;
    spc2[Start PC2] --> isCC{is CC?};
    spc2 --> TC;
    isCC --> |False| chkL{Data is Local?};
    chkL --> |False| SR;
    isCC --> |True| LCC;
    chkL --> |True| ER;

    subgraph SupraSeal PC2 Local;
    LCC[Local CC] --> WL[Write Last Layer to Filesystem];
    LCC[Local CC] --> TR;
    TC[Build Parallel Tree C] --> CR;
    TR[Build Parallel Tree R] --> CR;
    WR[Write Replica to Filesystem];
    CR[Calculate Comm R];
    ER[Encode Replica K+D] --> WR;
    SR[Send to Remote];
    WT --> CR;
    ER --> TR;
    end;

    ERL --> RR[Replica on Remote Filesystem];
    STR --> rtrf[Tree R File on Remote Filesystem];
    SL --> WT;
    TR --> trf[Tree R Files];
    CR --> paux[Write paux to Filesystem];
    TC --> tcf[Tree C Files];
    WR --> Repdisk[Replica on Filesystem];
    WL --> Repdisk;

    subgraph SupraSeal PC2 Remote;
    SR --> ERL[Encode Replica K+D];
    ERL --> STR[Build Tree R];
    STR --> SL[Send Root to Local];
    end;
    
    SR --> WT[Wait for Tree R Root];
```
