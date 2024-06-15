package deps

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"time"

	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-jsonrpc"

	"github.com/filecoin-project/curio/api"

	lapi "github.com/filecoin-project/lotus/api"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/filecoin-project/lotus/lib/retry"
)

func getFullNodeAPIV1Curio(ctx *cli.Context, ainfoCfg []string, opts ...cliutil.GetFullNodeOption) (api.Chain, jsonrpc.ClientCloser, error) {
	if tn, ok := ctx.App.Metadata["testnode-full"]; ok {
		return tn.(api.Chain), func() {}, nil
	}

	var options cliutil.GetFullNodeOptions
	for _, opt := range opts {
		opt(&options)
	}

	var rpcOpts []jsonrpc.Option
	if options.EthSubHandler != nil {
		rpcOpts = append(rpcOpts, jsonrpc.WithClientHandler("Filecoin", options.EthSubHandler), jsonrpc.WithClientHandlerAlias("eth_subscription", "Filecoin.EthSubscription"))
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
		v1api, closer, err := newChainNodeRPCV1(ctx.Context, head.addr, head.header, rpcOpts...)
		if err != nil {
			log.Warnf("Not able to establish connection to node with addr: %s, Reason: %s", head.addr, err.Error())
			continue
		}
		fullNodes = append(fullNodes, v1api)
		closers = append(closers, closer)
	}

	// When running in cluster mode and trying to establish connections to multiple nodes, fail
	// if less than 2 lotus nodes are actually running
	if len(httpHeads) > 1 && len(fullNodes) < 2 {
		return nil, nil, xerrors.Errorf("Not able to establish connection to more than a single node")
	}

	finalCloser := func() {
		for _, c := range closers {
			c()
		}
	}

	var v1API api.ChainStruct
	FullNodeProxy(fullNodes, &v1API)

	v, err := v1API.Version(ctx.Context)
	if err != nil {
		return nil, nil, err
	}
	if !v.APIVersion.EqMajorMinor(lapi.FullAPIVersion1) {
		return nil, nil, xerrors.Errorf("Remote API version didn't match (expected %s, remote %s)", lapi.FullAPIVersion1, v.APIVersion)
	}
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

func FullNodeProxy[T api.Chain](ins []T, outstr *api.ChainStruct) {
	outs := api.GetInternalStructs(outstr)

	var rins []reflect.Value
	for _, in := range ins {
		rins = append(rins, reflect.ValueOf(in))
	}

	for _, out := range outs {
		rProxyInternal := reflect.ValueOf(out).Elem()

		for f := 0; f < rProxyInternal.NumField(); f++ {
			field := rProxyInternal.Type().Field(f)

			var fns []reflect.Value
			for _, rin := range rins {
				fns = append(fns, rin.MethodByName(field.Name))
			}

			rProxyInternal.Field(f).Set(reflect.MakeFunc(field.Type, func(args []reflect.Value) (results []reflect.Value) {
				errorsToRetry := []error{&jsonrpc.RPCConnectionError{}, &jsonrpc.ErrClient{}}
				initialBackoff, err := time.ParseDuration("1s")
				if err != nil {
					return nil
				}

				ctx := args[0].Interface().(context.Context)

				curr := -1

				// for calls that need to be performed on the same node
				// primarily for miner when calling create block and submit block subsequently
				key := contextKey("retry-node")
				if ctx.Value(key) != nil {
					if (*ctx.Value(key).(**int)) == nil {
						*ctx.Value(key).(**int) = &curr
					} else {
						curr = **ctx.Value(key).(**int) - 1
					}
				}

				total := len(rins)
				result, _ := retry.Retry(ctx, 5, initialBackoff, errorsToRetry, func() ([]reflect.Value, error) {
					curr = (curr + 1) % total

					result := fns[curr].Call(args)
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
