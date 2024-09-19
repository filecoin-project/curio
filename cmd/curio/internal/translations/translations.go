// Usage:
// To UPDATE translations:
//
//  1. add/change strings in guidedsetup folder that use d.T() or d.say().
//
//  2. run `go generate` in the cmd/curio/internal/translations/ folder.
//
//  3. ChatGPT 3.5 can translate the ./locales/??/out.gotext.json files'
//     which ONLY include the un-translated messages.
//     APPEND to the messages.gotext.json files's array.
//
//     ChatGPT fuss:
//     - on a good day, you may need to hit "continue generating".
//     - > 60? you'll need to give it sections of the file.
//
//  4. Re-import with `go generate` again.
//
// To ADD a language:
//  1. Add it to the list in updateLang.sh
//  2. Run `go generate` in the cmd/curio/internal/translations/ folder.
//  3. Follow the "Update translations" steps here.
//  4. Code will auto-detect the new language and use it.
//
// FUTURE Reliability: OpenAPI automation.
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
		fmt.Println("Defaulting to English. Please reach out to the Curio team if you would like to have additional language support.")
	}
	return func(key message.Reference, a ...interface{}) string {
			return message.NewPrinter(lang).Sprintf(key, a...)
		}, func(sty lipgloss.Style, key message.Reference, a ...interface{}) {
			msg := message.NewPrinter(lang).Sprintf(key, a...)
			fmt.Println(sty.Render(msg))
		}
}
