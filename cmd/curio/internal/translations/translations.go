// Usage:
// To UPDATE translations:
//
//  1. Add/change strings in cmd/curio that use T() or say().
//
//  2. Run `make gen` - this extracts strings and compiles existing translations.
//     The output will show how many strings need translation for each language.
//
//  3. Translate the missing strings:
//     - Check ./locales/{lang}/out.gotext.json for strings with "fuzzy": true
//     - Add translations to ./locales/{lang}/messages.gotext.json
//     - ChatGPT/Claude can help translate - give it out.gotext.json content
//
//  4. Run `make gen` again - translations are compiled into catalog.go.
//
// To ADD a language:
//  1. Add it to the lang list in updateLang.sh
//  2. Run `make gen`
//  3. Translate the new ./locales/{lang}/out.gotext.json
//  4. Code will auto-detect the new language via $LANG env var.
//
// Technical note:
// The extraction uses a fast AST-based parser (extract.go) that avoids loading
// heavy CGO dependencies. This makes `make gen` run in ~8s instead of ~3min.
package translations

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/samber/lo"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

//go:generate ./updateLang.sh

var T = setupLang()

func setupLang() func(key message.Reference, a ...interface{}) string {
	lang, _ := SetupLanguage()
	return lang
}

var notice = lipgloss.NewStyle().
	Align(lipgloss.Left).
	Bold(true).
	Foreground(lipgloss.Color("#CCCCCC")).
	Background(lipgloss.Color("#333300")).MarginBottom(1)

func SetupLanguage() (func(key message.Reference, a ...interface{}) string, func(style lipgloss.Style, key message.Reference, a ...interface{})) {
	langText := "en"
	problem := false
	if len(os.Getenv("LANG")) > 1 {
		langText = os.Getenv("LANG")[:2]
	} else {
		problem = true
	}

	lang, err := language.Parse(langText)
	if err != nil {
		lang = language.English
		problem = true
		fmt.Println("Error parsing language")
	}

	langs := message.DefaultCatalog.Languages()
	have := lo.SliceToMap(langs, func(t language.Tag) (string, bool) { return t.String(), true })
	if _, ok := have[lang.String()]; !ok {
		lang = language.English
		problem = true
	}
	if problem {
		_ = os.Setenv("LANG", "en-US") // for later users of this function
		notice.Copy().AlignHorizontal(lipgloss.Right).
			Render("$LANG=" + langText + " unsupported. Available: " + strings.Join(lo.Keys(have), ", "))
	}
	return func(key message.Reference, a ...interface{}) string {
			return message.NewPrinter(lang).Sprintf(key, a...)
		}, func(sty lipgloss.Style, key message.Reference, a ...interface{}) {
			msg := message.NewPrinter(lang).Sprintf(key, a...)
			fmt.Println(sty.Render(msg))
		}
}
