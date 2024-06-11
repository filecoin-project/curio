package config

type Common struct {
	API     API
	Backup  Backup
	Logging Logging
	Libp2p  Libp2p
	Pubsub  Pubsub
}

// API contains configs for API endpoint
type API struct {
	// Binding address for the Binary's API
	ListenAddress       string
	RemoteListenAddress string
	Timeout             Duration
}

// // Common

type Backup struct {
	// When set to true disables metadata log (.lotus/kvlog). This can save disk
	// space by reducing metadata redundancy.
	//
	// Note that in case of metadata corruption it might be much harder to recover
	// your node if metadata log is disabled
	DisableMetadataLog bool
}

// Logging is the logging system config
type Logging struct {
	// SubsystemLevels specify per-subsystem log levels
	SubsystemLevels map[string]string
}

// Libp2p contains configs for libp2p
type Libp2p struct {
	// Binding address for the libp2p host - 0 means random port.
	// Format: multiaddress; see https://multiformats.io/multiaddr/
	ListenAddresses []string
	// Addresses to explicitally announce to other peers. If not specified,
	// all interface addresses are announced
	// Format: multiaddress
	AnnounceAddresses []string
	// Addresses to not announce
	// Format: multiaddress
	NoAnnounceAddresses []string
	BootstrapPeers      []string
	ProtectedPeers      []string

	// When not disabled (default), lotus asks NAT devices (e.g., routers), to
	// open up an external port and forward it to the port lotus is running on.
	// When this works (i.e., when your router supports NAT port forwarding),
	// it makes the local lotus node accessible from the public internet
	DisableNatPortMap bool

	// ConnMgrLow is the number of connections that the basic connection manager
	// will trim down to.
	ConnMgrLow uint
	// ConnMgrHigh is the number of connections that, when exceeded, will trigger
	// a connection GC operation. Note: protected/recently formed connections don't
	// count towards this limit.
	ConnMgrHigh uint
	// ConnMgrGrace is a time duration that new connections are immune from being
	// closed by the connection manager.
	ConnMgrGrace Duration
}

type Pubsub struct {
	// Run the node in bootstrap-node mode
	Bootstrapper bool
	// DirectPeers specifies peers with direct peering agreements. These peers are
	// connected outside of the mesh, with all (valid) message unconditionally
	// forwarded to them. The router will maintain open connections to these peers.
	// Note that the peering agreement should be reciprocal with direct peers
	// symmetrically configured at both ends.
	// Type: Array of multiaddress peerinfo strings, must include peerid (/p2p/12D3K...
	DirectPeers           []string
	IPColocationWhitelist []string
	RemoteTracer          string
	// Path to file that will be used to output tracer content in JSON format.
	// If present tracer will save data to defined file.
	// Format: file path
	JsonTracer string
	// Connection string for elasticsearch instance.
	// If present tracer will save data to elasticsearch.
	// Format: https://<username>:<password>@<elasticsearch_url>:<port>/
	ElasticSearchTracer string
	// Name of elasticsearch index that will be used to save tracer data.
	// This property is used only if ElasticSearchTracer propery is set.
	ElasticSearchIndex string
	// Auth token that will be passed with logs to elasticsearch - used for weighted peers score.
	TracerSourceAuth string
}
