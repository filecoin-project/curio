package openpieces

import "testing"

func TestPlacementDecision(t *testing.T) {
	tests := []struct {
		name                  string
		forceACL, forceOpen   bool
		anySecured, anyPublic bool
		wantACL, wantOpen     bool
		stay                  bool
	}{
		{name: "upload before a data set claims it", stay: true},
		{name: "public data set", anyPublic: true, wantOpen: true},
		{name: "restricted data set", anySecured: true, wantACL: true},
		{name: "restricted and public data sets", anySecured: true, anyPublic: true, wantACL: true, wantOpen: true},
		{name: "create-and-add restricted before the row exists", forceACL: true, wantACL: true},
		{name: "create-and-add public before the row exists", forceOpen: true, wantOpen: true},
		{name: "restricted claim of a piece that is also public", forceACL: true, anyPublic: true, wantACL: true, wantOpen: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotACL, gotOpen, gotStay := placementDecision(tt.forceACL, tt.forceOpen, tt.anySecured, tt.anyPublic)
			if gotACL != tt.wantACL || gotOpen != tt.wantOpen || gotStay != tt.stay {
				t.Fatalf("placementDecision(%v, %v, %v, %v) = (%v, %v, %v), want (%v, %v, %v)",
					tt.forceACL, tt.forceOpen, tt.anySecured, tt.anyPublic,
					gotACL, gotOpen, gotStay, tt.wantACL, tt.wantOpen, tt.stay)
			}
		})
	}
}
