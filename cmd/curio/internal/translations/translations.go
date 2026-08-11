// Usage:
// To UPDATE translations:
//
//  1. Add/change strings in cmd/curio that use T() or say().
//
//  2. Run `make gen` - this extracts strings and compiles the English catalog.
//
// Technical note:
// The extraction uses a fast AST-based parser (extract.go) that avoids loading
// heavy CGO dependencies. This makes `make gen` run in ~8s instead of ~3min.
//
// CLI strings are English-only. Non-English locale support has been removed.
package translations

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

//go:generate ./updateLang.sh

var T = setupLang()

func setupLang() func(key message.Reference, a ...any) string {
	lang, _ := SetupLanguage()
	return lang
}

func SetupLanguage() (func(key message.Reference, a ...any) string, func(style lipgloss.Style, key message.Reference, a ...any)) {
	p := message.NewPrinter(language.English)
	return func(key message.Reference, a ...any) string {
			return p.Sprintf(key, a...)
		}, func(sty lipgloss.Style, key message.Reference, a ...any) {
			fmt.Println(sty.Render(p.Sprintf(key, a...)))
		}
}
