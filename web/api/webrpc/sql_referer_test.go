package webrpc

import "testing"

func TestIsSQLConsoleReferer(t *testing.T) {
	const host = "localhost:4701"

	cases := []struct {
		name    string
		referer string
		host    string
		want    bool
	}{
		{"exact", "http://localhost:4701/pages/sql", host, true},
		{"trailing slash", "http://localhost:4701/pages/sql/", host, true},
		{"index", "http://localhost:4701/pages/sql/index.html", host, true},
		{"https", "https://localhost:4701/pages/sql/", host, true},
		{"other page", "http://localhost:4701/pages/tasks/", host, false},
		{"prefix trap", "http://localhost:4701/pages/sql-evil", host, false},
		{"host mismatch", "http://evil.example/pages/sql/", host, false},
		{"empty", "", host, false},
		{"no host check", "http://localhost:4701/pages/sql/", "", true},
		{"relative", "/pages/sql/", host, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isSQLConsoleReferer(tc.referer, tc.host)
			if got != tc.want {
				t.Fatalf("isSQLConsoleReferer(%q, %q) = %v, want %v", tc.referer, tc.host, got, tc.want)
			}
		})
	}
}
