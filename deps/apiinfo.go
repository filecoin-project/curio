package deps

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/curio/api"

	"github.com/filecoin-project/lotus/chain/types"
	cliutil "github.com/filecoin-project/lotus/cli/util"
)

func GetFullNodeAPIV1Curio(ctx *cli.Context, ainfoCfg []string) (api.Chain, jsonrpc.ClientCloser, error) {
	if tn, ok := ctx.App.Metadata["testnode-full"]; ok {
		return tn.(api.Chain), func() {}, nil
	}
	var httpHeads []httpHead
	version := "v1"
	{
		if len(ainfoCfg) == 0 {
			return nil, nil, xerrors.Errorf("could not get API info: none configured. \nConsider getting base.toml with './curio config get base >/tmp/base.toml' \nthen adding   \n[APIs] \n ChainApiInfo = [\" result_from lotus auth api-info --perm=admin \"]\n  and updating it with './curio config set /tmp/base.toml'")
		}
		for _, i := range ainfoCfg {
			ainfo := cliutil.ParseApiInfo(i)
			addr, err := ainfo.DialArgs(version)
			if err != nil {
				return nil, nil, xerrors.Errorf("could not get DialArgs: %w", err)
			}
			httpHeads = append(httpHeads, httpHead{addr: addr, header: ainfo.AuthHeader()})
		}
	}

	if cliutil.IsVeryVerbose {
		_, _ = fmt.Fprintln(ctx.App.Writer, "using full node API v1 endpoint:", httpHeads[0].addr)
	}

	var fullNodes []api.Chain
	var closers []jsonrpc.ClientCloser

	for _, head := range httpHeads {
		v1api, closer, err := newChainNodeRPCV1(ctx.Context, head.addr, head.header)
		if err != nil {
			log.Warnf("Not able to establish connection to node with addr: %s, Reason: %s", head.addr, err.Error())
			continue
		}
		fullNodes = append(fullNodes, v1api)
		closers = append(closers, closer)
	}

	if len(fullNodes) == 0 {
		return nil, nil, xerrors.Errorf("failed to establish connection with all nodes")
	}

	finalCloser := func() {
		for _, c := range closers {
			c()
		}
	}

	var v1API api.ChainStruct
	FullNodeProxy(fullNodes, &v1API)

	return &v1API, finalCloser, nil
}

type contextKey string

// Not thread safe
func OnSingleNode(ctx context.Context) context.Context {
	return context.WithValue(ctx, contextKey("retry-node"), new(*int))
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
		api.GetInternalStructs(&res), requestHeader, append([]jsonrpc.Option{jsonrpc.WithErrors(RPCErrors)}, opts...)...)

	return &res, closer, err
}

const initialBackoff = time.Second
const maxRetryAttempts = 5
const maxBehindBestHealthy = 1

var errorsToRetry = []error{&jsonrpc.RPCConnectionError{}, &jsonrpc.ErrClient{}}

const preferredAllBad = -1

// FullNodeProxy creates a proxy for the Chain API
func FullNodeProxy[T api.Chain](ins []T, outstr *api.ChainStruct) {
	providerCount := len(ins)

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
		if len(ins) == 1 {
			// not like we have any onter node to go to..
			return
		}

		// don't bother for short-running commands
		time.Sleep(250 * time.Millisecond)

		var bestKnownTipset, nextBestKnownTipset *types.TipSet

		for {
			var wg sync.WaitGroup
			wg.Add(providerCount)

			for i := 0; i < providerCount; i++ {
				go func(i int) {
					defer wg.Done()

					toctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // todo better timeout
					ch, err := ins[i].ChainHead(toctx)
					cancel()

					// error is definitely not healthy
					if err != nil {
						healthyLk.Lock()
						unhealthyProviders[i] = true
						healthyLk.Unlock()

						log.Errorw("rpc check chain head call failed", "fail_type", "rpc_error", "provider", i, "error", err)
						return
					}

					healthyLk.Lock()
					// maybe set best next
					if nextBestKnownTipset == nil || big.Cmp(ch.ParentWeight(), nextBestKnownTipset.ParentWeight()) > 0 || len(ch.Blocks()) > len(nextBestKnownTipset.Blocks()) {
						nextBestKnownTipset = ch
					}

					if bestKnownTipset != nil {
						// if we're behind the best tipset, mark as unhealthy
						unhealthyProviders[i] = ch.Height() < bestKnownTipset.Height()-maxBehindBestHealthy
						if unhealthyProviders[i] {
							log.Errorw("rpc check chain head call failed", "fail_type", "behind_best", "provider", i, "height", ch.Height(), "best_height", bestKnownTipset.Height())
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

	outs := api.GetInternalStructs(outstr)

	var apiProviders []reflect.Value
	for _, in := range ins {
		apiProviders = append(apiProviders, reflect.ValueOf(in))
	}

	for _, out := range outs {
		rOutStruct := reflect.ValueOf(out).Elem()

		for f := 0; f < rOutStruct.NumField(); f++ {
			field := rOutStruct.Type().Field(f)

			var providerFuncs []reflect.Value
			for _, rin := range apiProviders {
				mv := rin.MethodByName(field.Name)
				if !mv.IsValid() {
					continue
				}
				providerFuncs = append(providerFuncs, mv)
			}

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
				key := contextKey("retry-node")
				if ctx.Value(key) != nil {
					if (*ctx.Value(key).(**int)) == nil {
						*ctx.Value(key).(**int) = preferredProvider
					} else {
						preferredProvider = *ctx.Value(key).(**int)
					}
				}

				result, _ := Retry(ctx, maxRetryAttempts, initialBackoff, errorsToRetry, func(isRetry bool) ([]reflect.Value, error) {
					if isRetry {
						pp := nextHealthyProvider(*preferredProvider + 1)
						if pp == -1 {
							return nil, xerrors.Errorf("no healthy providers")
						}
						*preferredProvider = pp
					}

					result := providerFuncs[*preferredProvider].Call(args)
					if result[len(result)-1].IsNil() {
						return result, nil
					}
					e := result[len(result)-1].Interface().(error)
					return result, e
				})
				return result
			}))
		}
	}
}

func Retry[T any](ctx context.Context, attempts int, initialBackoff time.Duration, errorTypes []error, f func(isRetry bool) (T, error)) (result T, err error) {
	for i := 0; i < attempts; i++ {
		if i > 0 {
			log.Info("Retrying after error:", err)
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
	log.Errorf("Failed after %d attempts, last error: %s", attempts, err)
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
