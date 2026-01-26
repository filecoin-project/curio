package deps

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	erpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/websocket"
	logging "github.com/ipfs/go-log/v2"
	"github.com/samber/lo"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-jsonrpc"
	fbig "github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"

	lapi "github.com/filecoin-project/lotus/api"
	ltypes "github.com/filecoin-project/lotus/chain/types"
	cliutil "github.com/filecoin-project/lotus/cli/util"
)

var clog = logging.Logger("curio/chain")

func GetFullNodeAPIV1Curio(ctx *cli.Context, ainfoCfg *config.Dynamic[[]string]) (api.Chain, jsonrpc.ClientCloser, error) {
	if tn, ok := ctx.App.Metadata["testnode-full"]; ok {
		return tn.(api.Chain), func() {}, nil
	}

	connections := map[string]api.Chain{}
	var closers []jsonrpc.ClientCloser
	var existingConnectionsMutex sync.Mutex
	var fullNodes = config.NewDynamic([]api.Chain{})

	var addresses []string
	updateDynamic := func() error {
		existingConnectionsMutex.Lock()
		defer existingConnectionsMutex.Unlock()
		if len(ainfoCfg.Get()) == 0 {
			return fmt.Errorf("could not get API info: none configured. \nConsider getting base.toml with './curio config get base >/tmp/base.toml' \nthen adding   \n[APIs] \n ChainApiInfo = [\" result_from lotus auth api-info --perm=admin \"]\n  and updating it with './curio config set /tmp/base.toml'")
		}

		httpHeads := make(map[string]httpHead)
		version := "v1"
		for _, i := range ainfoCfg.Get() {
			if _, ok := connections[i]; ok {
				continue
			}
			ainfo := cliutil.ParseApiInfo(i)
			addr, err := ainfo.DialArgs(version)
			if err != nil {
				return xerrors.Errorf("could not get DialArgs: %w", err)
			}
			addresses = append(addresses, addr)
			httpHeads[i] = httpHead{addr: addr, header: ainfo.AuthHeader()}
		}

		/// At this point we have a valid, dynamic httpHeads, but we don't want to rebuild existing connections.

		if cliutil.IsVeryVerbose {
			_, _ = fmt.Fprintln(ctx.App.Writer, "using full node API v1 endpoint:", strings.Join(addresses, ", "))
		}

		// Check network compatibility for each node
		for identifier, head := range httpHeads {
			if connections[identifier] != nil {
				continue
			}

			v1api, closer, err := newChainNodeRPCV1(ctx.Context, head.addr, head.header)
			if err != nil {
				clog.Warnf("Not able to establish connection to node with addr: %s, Reason: %s", head.addr, err.Error())
				continue
			}

			// Validate network match
			networkName, err := v1api.StateNetworkName(ctx.Context)
			if err != nil {
				clog.Warnf("Failed to get network name from node %s: %s", head.addr, err.Error())
				closer()
				continue
			}

			// Compare with binary's network using BuildTypeString()
			if !strings.HasPrefix(string(networkName), "test") && !strings.HasPrefix(string(networkName), "local") {
				if networkName == "calibrationnet" {
					networkName = "calibnet"
				}

				if string(networkName) != build.BuildTypeString()[1:] {
					clog.Warnf("Network mismatch for node %s: binary built for %s but node is on %s",
						head.addr, build.BuildTypeString()[1:], networkName)
					closer()
					continue
				}
			}

			connections[identifier] = v1api
			closers = append(closers, closer)
		}
		fullNodes.Set(lo.Map(slices.Collect(maps.Keys(connections)), func(k string, _ int) api.Chain { return connections[k] }))
		return nil
	}

	err := updateDynamic()
	if err != nil {
		return nil, nil, err
	}
	ainfoCfg.OnChange(func() {
		if err := updateDynamic(); err != nil {
			clog.Errorf("failed to update http heads: %s", err)
		}
	})

	var v1API api.ChainStruct
	FullNodeProxy(fullNodes, &v1API)

	finalCloser := func() {
		existingConnectionsMutex.Lock()
		defer existingConnectionsMutex.Unlock()
		for _, c := range closers {
			c()
		}
	}
	return &v1API, finalCloser, nil
}

type contextKey string

var retryNodeKey = contextKey("retry-node")

// OnSingleNode returns a new context that will try to perform all calls on the same node.
// If the backing node fails, the calls will be retried on a different node, and further calls will be made on that node.
// Not thread safe
func OnSingleNode(ctx context.Context) context.Context {
	return context.WithValue(ctx, retryNodeKey, new(*int))
}

type httpHead struct {
	addr   string
	header http.Header
}

var RPCErrors = jsonrpc.NewErrors()

// newChainNodeRPCV1 creates a new http jsonrpc client.
func newChainNodeRPCV1(ctx context.Context, addr string, requestHeader http.Header, opts ...jsonrpc.Option) (api.Chain, jsonrpc.ClientCloser, error) {
	var res api.ChainStruct
	closer, err := jsonrpc.NewMergeClient(ctx, addr, "Filecoin",
		api.GetInternalStructs(&res), requestHeader, append([]jsonrpc.Option{jsonrpc.WithErrors(lapi.RPCErrors)}, opts...)...)

	return &res, closer, err
}

const initialBackoff = time.Second
const maxRetryAttempts = 5
const maxBehindBestHealthy = 1

var errorsToRetry = []error{&jsonrpc.RPCConnectionError{}, &jsonrpc.ErrClient{}}

const preferredAllBad = -1

// FullNodeProxy creates a proxy for the Chain API
func FullNodeProxy[T api.Chain](ins *config.Dynamic[[]T], outstr *api.ChainStruct) {
	providerCount := len(ins.Get())

	var healthyLk sync.Mutex
	unhealthyProviders := make([]bool, providerCount)

	nextHealthyProvider := func(start int) int {
		healthyLk.Lock()
		defer healthyLk.Unlock()

		for i := 0; i < providerCount; i++ {
			idx := (start + i) % providerCount
			if !unhealthyProviders[idx] {
				return idx
			}
		}
		return preferredAllBad
	}

	// watch provider health
	startWatch := func() {
		if len(ins.Get()) == 1 {
			// not like we have any onter node to go to..
			return
		}

		// don't bother for short-running commands
		time.Sleep(250 * time.Millisecond)

		var bestKnownTipset, nextBestKnownTipset *ltypes.TipSet

		for {
			var wg sync.WaitGroup
			wg.Add(providerCount)

			for i := 0; i < providerCount; i++ {
				go func(i int) {
					defer wg.Done()

					toctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // todo better timeout
					ch, err := ins.Get()[i].ChainHead(toctx)
					cancel()

					// error is definitely not healthy
					if err != nil {
						healthyLk.Lock()
						unhealthyProviders[i] = true
						healthyLk.Unlock()

						clog.Debugw("rpc check chain head call failed", "fail_type", "rpc_error", "provider", i, "error", err)
						return
					}

					healthyLk.Lock()
					// maybe set best next
					if nextBestKnownTipset == nil || fbig.Cmp(ch.ParentWeight(), nextBestKnownTipset.ParentWeight()) > 0 || len(ch.Blocks()) > len(nextBestKnownTipset.Blocks()) {
						nextBestKnownTipset = ch
					}

					if bestKnownTipset != nil {
						// if we're behind the best tipset, mark as unhealthy
						unhealthyProviders[i] = ch.Height() < bestKnownTipset.Height()-maxBehindBestHealthy
						if unhealthyProviders[i] {
							clog.Debugw("rpc check chain head call failed", "fail_type", "behind_best", "provider", i, "height", ch.Height(), "best_height", bestKnownTipset.Height())
						}
					}
					healthyLk.Unlock()
				}(i)
			}

			wg.Wait()
			bestKnownTipset = nextBestKnownTipset

			time.Sleep(5 * time.Second)
		}
	}
	var starWatchOnce sync.Once

	// populate output api proxy
	populateProxyMethods(outstr, ins, nextHealthyProvider, startWatch, &starWatchOnce, providerCount)
}

// populateProxyMethods sets up the proxy methods for the API struct with retry and health monitoring
func populateProxyMethods[T, U any](outstr U, ins *config.Dynamic[[]T], nextHealthyProvider func(int) int, startWatch func(), starWatchOnce *sync.Once, providerCount int) {
	outs := api.GetInternalStructs(outstr)

	var apiProviders []reflect.Value
	apiProvidersMx := sync.Mutex{}
	setupProviders := func() {
		for _, in := range ins.Get() {
			apiProviders = append(apiProviders, reflect.ValueOf(in))
		}
	}
	setupProviders()
	ins.OnChange(func() {
		apiProvidersMx.Lock()
		apiProviders = nil
		setupProviders()
		apiProvidersMx.Unlock()
	})

	providerFuncs := make([][][]reflect.Value, len(outs))
	setProviderFuncs := func() {
		for outIdx, out := range outs {
			rOutStruct := reflect.ValueOf(out).Elem()
			providerFuncs[outIdx] = make([][]reflect.Value, rOutStruct.NumField())

			for f := 0; f < rOutStruct.NumField(); f++ {
				field := rOutStruct.Type().Field(f)

				var p []reflect.Value
				apiProvidersMx.Lock()
				p = apiProviders
				apiProvidersMx.Unlock()

				providerFuncs[outIdx][f] = make([]reflect.Value, len(p))
				for pIdx, rin := range p {
					mv := rin.MethodByName(field.Name)
					if !mv.IsValid() {
						continue
					}
					providerFuncs[outIdx][f][pIdx] = mv
				}
			}
		}
	}
	setProviderFuncs()
	ins.OnChange(func() {
		apiProvidersMx.Lock()
		apiProviders = nil
		setProviderFuncs()
		apiProvidersMx.Unlock()
	})
	for outIdx, out := range outs {
		rOutStruct := reflect.ValueOf(out).Elem()
		for f := 0; f < rOutStruct.NumField(); f++ {
			field := rOutStruct.Type().Field(f)
			rOutStruct.Field(f).Set(reflect.MakeFunc(field.Type, func(args []reflect.Value) (results []reflect.Value) {
				starWatchOnce.Do(func() {
					go startWatch()
				})

				ctx := args[0].Interface().(context.Context)

				preferredProvider := new(int)
				*preferredProvider = nextHealthyProvider(0)
				if *preferredProvider == preferredAllBad {
					// select at random, retry will do it's best
					*preferredProvider = rand.Intn(providerCount)
				}

				// for calls that need to be performed on the same node
				// primarily for miner when calling create block and submit block subsequently
				if ctx.Value(retryNodeKey) != nil {
					if (*ctx.Value(retryNodeKey).(**int)) == nil {
						*ctx.Value(retryNodeKey).(**int) = preferredProvider
					} else {
						preferredProvider = *ctx.Value(retryNodeKey).(**int)
					}
				}

				result, rerr := Retry(ctx, maxRetryAttempts, initialBackoff, errorsToRetry, func(isRetry bool) ([]reflect.Value, error) {
					if isRetry {
						pp := nextHealthyProvider(*preferredProvider + 1)
						if pp == -1 {
							return nil, xerrors.Errorf("no healthy providers")
						}
						*preferredProvider = pp
					}

					apiProvidersMx.Lock()
					fn := providerFuncs[outIdx][f][*preferredProvider]
					apiProvidersMx.Unlock()

					result := fn.Call(args)
					if result[len(result)-1].IsNil() {
						return result, nil
					}
					e := result[len(result)-1].Interface().(error)
					return result, e
				})
				if rerr != nil && field.Type.NumOut() != len(result) {
					clog.Errorw("retry rpc call error", "error", rerr, "result", result)

					var out []reflect.Value
					for i := 0; i < field.Type.NumOut(); i++ {
						out = append(out, reflect.Zero(field.Type.Out(i)))
					}
					// last value is always an error.. set it to the error
					out[len(out)-1] = reflect.ValueOf(xerrors.Errorf("retry rpc call error: %w", rerr))
					return out
				}

				return result
			}))
		}
	}
}

func Retry[T any](ctx context.Context, attempts int, initialBackoff time.Duration, errorTypes []error, f func(isRetry bool) (T, error)) (result T, err error) {
	for i := 0; i < attempts; i++ {
		if i > 0 {
			clog.Debugw("Retrying after error:", err)
			time.Sleep(initialBackoff)
			initialBackoff *= 2
		}
		result, err = f(i > 0)
		if err == nil || !ErrorIsIn(err, errorTypes) {
			return result, err
		}
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
	}
	clog.Errorf("Failed after %d attempts, last error: %s", attempts, err)
	return result, err
}

func ErrorIsIn(err error, errorTypes []error) bool {
	for _, etype := range errorTypes {
		tmp := reflect.New(reflect.PointerTo(reflect.ValueOf(etype).Elem().Type())).Interface()
		if errors.As(err, &tmp) {
			return true
		}
	}
	return false
}

func GetEthClient(cctx *cli.Context, ainfoCfg *config.Dynamic[[]string]) (api.EthClientInterface, error) {
	version := "v1"
	var ethClientDynamic = config.NewDynamic([]*ethclient.Client{})
	updateDynamic := func() error {
		if len(ainfoCfg.Get()) == 0 {
			return xerrors.Errorf("could not get API info: none configured. \nConsider getting base.toml with './curio config get base >/tmp/base.toml' \nthen adding   \n[APIs] \n ChainApiInfo = [\" result_from lotus auth api-info --perm=admin \"]\n  and updating it with './curio config set /tmp/base.toml'")
		}
		var httpHeads []httpHead
		for _, i := range ainfoCfg.Get() {
			ainfo := cliutil.ParseApiInfo(i)
			addr, err := ainfo.DialArgs(version)
			if err != nil {
				return xerrors.Errorf("could not get eth DialArgs: %w", err)
			}
			httpHeads = append(httpHeads, httpHead{addr: addr, header: ainfo.AuthHeader()})
		}

		var clients []*ethclient.Client
		for _, head := range httpHeads {
			if cliutil.IsVeryVerbose {
				_, _ = fmt.Fprintln(cctx.App.Writer, "using eth client endpoint:", head.addr)
			}

			d := websocket.Dialer{
				HandshakeTimeout: 10 * time.Second,
				ReadBufferSize:   4096,
				WriteBufferSize:  4096,
			}

			wopts := erpc.WithWebsocketDialer(d)
			hopts := erpc.WithHeaders(head.header)

			rpcClient, err := erpc.DialOptions(cctx.Context, head.addr, wopts, hopts)
			if err != nil {
				log.Warnf("failed to dial eth client: %s", err)
				continue
			}
			client := ethclient.NewClient(rpcClient)
			_, err = client.BlockNumber(cctx.Context)
			if err != nil {
				log.Warnf("failed to get eth block number: %s", err)
				continue
			}
			clients = append(clients, client)
		}

		if len(clients) == 0 {
			return errors.New("failed to establish connection with all nodes")
		}

		ethClientDynamic.Set(clients)
		return nil
	}
	if err := updateDynamic(); err != nil {
		return nil, err
	}
	ainfoCfg.OnChange(func() {
		if err := updateDynamic(); err != nil {
			clog.Errorf("failed to update eth client: %s", err)
		}
	})

	var ethClient api.EthClientInterfaceStruct
	EthClientProxy(ethClientDynamic, &ethClient)
	return &ethClient, nil
}

func EthClientProxy(ins *config.Dynamic[[]*ethclient.Client], outstr api.EthClientInterface) {
	providerCount := len(ins.Get())

	var healthyLk sync.Mutex
	unhealthyProviders := make([]bool, providerCount)

	nextHealthyProvider := func(start int) int {
		healthyLk.Lock()
		defer healthyLk.Unlock()

		for i := 0; i < providerCount; i++ {
			idx := (start + i) % providerCount
			if !unhealthyProviders[idx] {
				return idx
			}
		}
		return preferredAllBad
	}

	// Create a no-op start watch function since eth client doesn't need health monitoring like chain
	startWatch := func() {}
	var starWatchOnce sync.Once

	// Use the existing populateProxyMethods function
	populateProxyMethods(outstr, ins, nextHealthyProvider, startWatch, &starWatchOnce, providerCount)
}
