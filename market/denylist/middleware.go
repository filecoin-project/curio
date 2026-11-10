package denylist

import (
	"net/http"
	"strings"

	"github.com/ipfs/go-cid"
)

// Middleware returns an HTTP middleware that checks incoming requests against
// the denylist. It extracts CIDs from /ipfs/{cid}, /public/{cid}, and /piece/{cid} URL paths.
//
// Behavior:
//   - If the denylist has not been loaded yet: responds with 503 Service Unavailable
//   - If the CID is denylisted: responds with 451 Unavailable For Legal Reasons
//   - Otherwise: passes the request through to the next handler
func Middleware(f *Filter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, ok := extractCID(r.URL.Path)
			if !ok {
				// No CID in path, let it through
				next.ServeHTTP(w, r)
				return
			}

			denied, ready := f.IsDenied(c)
			if !ready {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("denylist not yet loaded, please retry shortly"))
				return
			}
			if denied {
				w.WriteHeader(http.StatusUnavailableForLegalReasons)
				_, _ = w.Write([]byte("content blocked by denylist"))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractCID attempts to extract a CID from common retrieval URL paths.
// Supported patterns:
//
//	/ipfs/{cid}[/...][?...]
//	/public/{cid}
//	/piece/{cid}
func extractCID(urlPath string) (cid.Cid, bool) {
	// /ipfs/ paths: CID is right after /ipfs/
	if strings.HasPrefix(urlPath, "/ipfs/") {
		return decodePathCID(urlPath[len("/ipfs/"):], true)
	}

	// /public/ and legacy /piece/ paths: CID is the remaining path segment
	if strings.HasPrefix(urlPath, "/public/") {
		return decodePathCID(urlPath[len("/public/"):], false)
	}
	if strings.HasPrefix(urlPath, "/piece/") {
		return decodePathCID(urlPath[len("/piece/"):], false)
	}

	return cid.Undef, false
}

func decodePathCID(rest string, cutAtSlash bool) (cid.Cid, bool) {
	cidStr := rest
	if cutAtSlash {
		if before, _, ok := strings.Cut(rest, "/"); ok {
			cidStr = before
		}
	}
	if cidStr == "" {
		return cid.Undef, false
	}
	c, err := cid.Decode(cidStr)
	if err != nil {
		return cid.Undef, false
	}
	return c, true
}
