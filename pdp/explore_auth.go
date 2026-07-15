package pdp

import (
	"net/http"
	"net/url"
	"strings"
)

const exploreDataSetsPathPrefix = "/explore/data-sets/"

// allowExploreList soft-gates the dataset explorer piece listing.
// Membership is public on-chain and piece bytes are served at /piece/{cid};
// this accepts either a valid service JWT or a Referer from the explorer UI
// so the list endpoint is clearly not meant as a public catalog API.
func (p *PDPService) allowExploreList(r *http.Request) bool {
	if _, err := p.AuthService(r); err == nil {
		return true
	}
	return refererIsExplorePage(r.Header.Get("Referer"))
}

// refererIsExplorePage reports whether referer is an explorer data-set page URL.
func refererIsExplorePage(referer string) bool {
	if referer == "" {
		return false
	}
	u, err := url.Parse(referer)
	if err != nil {
		return false
	}
	path := u.Path
	if path == "" {
		return false
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return strings.HasPrefix(path, exploreDataSetsPathPrefix)
}
