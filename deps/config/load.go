package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/BurntSushi/toml"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/kelseyhightower/envconfig"
	"github.com/samber/lo"
	"golang.org/x/xerrors"
)

// FromFile loads config from a specified file overriding defaults specified in
// the def parameter. If file does not exist or is empty defaults are assumed.
func FromFile(path string, opts ...LoadCfgOpt) (interface{}, error) {
	loadOpts, err := applyOpts(opts...)
	if err != nil {
		return nil, err
	}
	var def interface{}
	if loadOpts.defaultCfg != nil {
		def, err = loadOpts.defaultCfg()
		if err != nil {
			return nil, xerrors.Errorf("no config found")
		}
	}
	// check for loadability
	file, err := os.Open(path)
	switch {
	case os.IsNotExist(err):
		if loadOpts.canFallbackOnDefault != nil {
			if err := loadOpts.canFallbackOnDefault(); err != nil {
				return nil, err
			}
		}
		return def, nil
	case err != nil:
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()
	cfgBs, err := io.ReadAll(file)
	if err != nil {
		return nil, xerrors.Errorf("failed to read config for validation checks %w", err)
	}
	buf := bytes.NewBuffer(cfgBs)
	if loadOpts.validate != nil {
		if err := loadOpts.validate(buf.String()); err != nil {
			return nil, xerrors.Errorf("config failed validation: %w", err)
		}
	}
	return FromReader(buf, def, opts...)
}

// FromReader loads config from a reader instance.
func FromReader(reader io.Reader, def interface{}, opts ...LoadCfgOpt) (interface{}, error) {
	loadOpts, err := applyOpts(opts...)
	if err != nil {
		return nil, err
	}
	cfg := def
	var buf bytes.Buffer
	_, err = io.Copy(&buf, reader)
	if err != nil {
		return toml.MetaData{}, err
	}

	if ccfg, ok := cfg.(*CurioConfig); ok {
		err = FixTOML(buf.String(), ccfg)
		if err != nil {
			return nil, err
		}
		cfg = ccfg
	}

	md, err := toml.Decode(buf.String(), cfg)
	if err != nil {
		return nil, err
	}

	// find any fields with a tag: `moved:"New.Config.Location"` and move any set values there over to
	// the new location if they are not already set there.
	movedFields := findMovedFields(nil, cfg)
	var warningOut io.Writer = os.Stderr
	if loadOpts.warningWriter != nil {
		warningOut = loadOpts.warningWriter
	}
	for _, d := range movedFields {
		if md.IsDefined(d.Field...) {
			_, _ = fmt.Fprintf(
				warningOut,
				"WARNING: Use of deprecated configuration option '%s' will be removed in a future release, use '%s' instead\n",
				strings.Join(d.Field, "."),
				strings.Join(d.NewField, "."))
			if !md.IsDefined(d.NewField...) {
				// new value isn't set but old is, we should move what the user set there
				if err := moveFieldValue(cfg, d.Field, d.NewField); err != nil {
					return nil, fmt.Errorf("failed to move field value: %w", err)
				}
			}
		}
	}

	err = envconfig.Process("LOTUS", cfg)
	if err != nil {
		return nil, fmt.Errorf("processing env vars overrides: %s", err)
	}

	return cfg, nil
}

// move a value from the location in the valPtr struct specified by oldPath, to the location
// specified by newPath; where the path is an array of nested field names.
func moveFieldValue(valPtr interface{}, oldPath []string, newPath []string) error {
	oldValue, err := getFieldValue(valPtr, oldPath)
	if err != nil {
		return err
	}
	val := reflect.ValueOf(valPtr).Elem()
	for {
		field := val.FieldByName(newPath[0])
		if !field.IsValid() {
			return fmt.Errorf("unexpected error fetching field value")
		}
		if len(newPath) == 1 {
			if field.Kind() != oldValue.Kind() {
				return fmt.Errorf("unexpected error, old kind != new kind")
			}
			// set field on val to be the new one, and we're done
			field.Set(oldValue)
			return nil
		}
		if field.Kind() != reflect.Struct {
			return fmt.Errorf("unexpected error fetching field value, is not a struct")
		}
		newPath = newPath[1:]
		val = field
	}
}

// recursively iterate into `path` to find the terminal value
func getFieldValue(val interface{}, path []string) (reflect.Value, error) {
	if reflect.ValueOf(val).Kind() == reflect.Ptr {
		val = reflect.ValueOf(val).Elem().Interface()
	}
	field := reflect.ValueOf(val).FieldByName(path[0])
	if !field.IsValid() {
		return reflect.Value{}, fmt.Errorf("unexpected error fetching field value")
	}
	if len(path) > 1 {
		if field.Kind() != reflect.Struct {
			return reflect.Value{}, fmt.Errorf("unexpected error fetching field value, is not a struct")
		}
		return getFieldValue(field.Interface(), path[1:])
	}
	return field, nil
}

type movedField struct {
	Field    []string
	NewField []string
}

// inspect the fields recursively within a struct and find any with "moved" tags
func findMovedFields(path []string, val interface{}) []movedField {
	dep := make([]movedField, 0)
	if reflect.ValueOf(val).Kind() == reflect.Ptr {
		val = reflect.ValueOf(val).Elem().Interface()
	}
	t := reflect.TypeOf(val)
	if t.Kind() != reflect.Struct {
		return nil
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		// could also do a "deprecated" in here
		if idx := field.Tag.Get("moved"); idx != "" && idx != "-" {
			dep = append(dep, movedField{
				Field:    append(path, field.Name),
				NewField: strings.Split(idx, "."),
			})
		}
		if field.Type.Kind() == reflect.Struct && reflect.ValueOf(val).FieldByName(field.Name).IsValid() {
			deps := findMovedFields(append(path, field.Name), reflect.ValueOf(val).FieldByName(field.Name).Interface())
			dep = append(dep, deps...)
		}
	}
	return dep
}

type cfgLoadOpts struct {
	defaultCfg           func() (interface{}, error)
	canFallbackOnDefault func() error
	validate             func(string) error
	warningWriter        io.Writer
}

type LoadCfgOpt func(opts *cfgLoadOpts) error

func applyOpts(opts ...LoadCfgOpt) (cfgLoadOpts, error) {
	var loadOpts cfgLoadOpts
	var err error
	for _, opt := range opts {
		if err = opt(&loadOpts); err != nil {
			return loadOpts, fmt.Errorf("failed to apply load cfg option: %w", err)
		}
	}
	return loadOpts, nil
}

func SetDefault(f func() (interface{}, error)) LoadCfgOpt {
	return func(opts *cfgLoadOpts) error {
		opts.defaultCfg = f
		return nil
	}
}

func SetCanFallbackOnDefault(f func() error) LoadCfgOpt {
	return func(opts *cfgLoadOpts) error {
		opts.canFallbackOnDefault = f
		return nil
	}
}

func SetValidate(f func(string) error) LoadCfgOpt {
	return func(opts *cfgLoadOpts) error {
		opts.validate = f
		return nil
	}
}

func SetWarningWriter(w io.Writer) LoadCfgOpt {
	return func(opts *cfgLoadOpts) error {
		opts.warningWriter = w
		return nil
	}
}

func NoDefaultForSplitstoreTransition() error {
	return xerrors.Errorf("FullNode config not found and fallback to default disallowed while we transition to splitstore discard default.  Use `lotus config default` to set this repo up with a default config.  Be sure to set `EnableSplitstore` to `false` if you are running a full archive node")
}

// Match the EnableSplitstore field
func MatchEnableSplitstoreField(s string) bool {
	enableSplitstoreRx := regexp.MustCompile(`(?m)^\s*EnableSplitstore\s*=`)
	return enableSplitstoreRx.MatchString(s)
}

func ValidateSplitstoreSet(cfgRaw string) error {
	if !MatchEnableSplitstoreField(cfgRaw) {
		return xerrors.Errorf("Config does not contain explicit set of EnableSplitstore field, refusing to load. Please explicitly set EnableSplitstore. Set it to false if you are running a full archival node")
	}
	return nil
}

type cfgUpdateOpts struct {
	comment         bool
	keepUncommented func(string) bool
	noEnv           bool
}

// UpdateCfgOpt is a functional option for updating the config
type UpdateCfgOpt func(opts *cfgUpdateOpts) error

// KeepUncommented sets a function for matching default valeus that should remain uncommented during
// a config update that comments out default values.
func KeepUncommented(f func(string) bool) UpdateCfgOpt {
	return func(opts *cfgUpdateOpts) error {
		opts.keepUncommented = f
		return nil
	}
}

func Commented(commented bool) UpdateCfgOpt {
	return func(opts *cfgUpdateOpts) error {
		opts.comment = commented
		return nil
	}
}

func DefaultKeepUncommented() UpdateCfgOpt {
	return KeepUncommented(MatchEnableSplitstoreField)
}

func NoEnv() UpdateCfgOpt {
	return func(opts *cfgUpdateOpts) error {
		opts.noEnv = true
		return nil
	}
}

// ConfigUpdate takes in a config and a default config and optionally comments out default values
func ConfigUpdate(cfgCur, cfgDef interface{}, opts ...UpdateCfgOpt) ([]byte, error) {
	var updateOpts cfgUpdateOpts
	for _, opt := range opts {
		if err := opt(&updateOpts); err != nil {
			return nil, xerrors.Errorf("failed to apply update cfg option to ConfigUpdate's config: %w", err)
		}
	}
	var nodeStr, defStr string
	if cfgDef != nil {
		buf := new(bytes.Buffer)
		e := toml.NewEncoder(buf)
		if err := e.Encode(cfgDef); err != nil {
			return nil, xerrors.Errorf("encoding default config: %w", err)
		}

		defStr = buf.String()
	}

	{
		buf := new(bytes.Buffer)
		e := toml.NewEncoder(buf)
		if err := e.Encode(cfgCur); err != nil {
			return nil, xerrors.Errorf("encoding node config: %w", err)
		}

		nodeStr = buf.String()
	}

	if updateOpts.comment {
		// create a map of default lines, so we can comment those out later
		defLines := strings.Split(defStr, "\n")
		defaults := map[string]struct{}{}
		currentSection := ""

		defSectionRx := regexp.MustCompile(`\[(.+)]`)

		for i := range defLines {
			l := strings.TrimSpace(defLines[i])
			if len(l) == 0 || l[0] == '#' {
				continue
			}
			if l[0] == '[' {
				m := defSectionRx.FindStringSubmatch(l)
				if len(m) == 2 {
					currentSection = m[1]
				}
				continue
			}

			qualifiedKey := currentSection + "." + l
			defaults[qualifiedKey] = struct{}{}
		}

		nodeLines := strings.Split(nodeStr, "\n")
		var outLines []string

		sectionRx := regexp.MustCompile(`\[(.+)]`)
		var section string

		for i, line := range nodeLines {
			// if this is a section, track it
			trimmed := strings.TrimSpace(line)
			if len(trimmed) > 0 {
				if trimmed[0] == '[' {
					m := sectionRx.FindSubmatch([]byte(trimmed))
					if len(m) != 2 {
						return nil, xerrors.Errorf("section didn't match (line %d)", i)
					}
					section = string(m[1])

					pad := strings.Repeat(" ", len(line)-len(strings.TrimLeftFunc(line, unicode.IsSpace)))

					// Find documentation for the section
					doc := findDoc(cfgCur, section, "")
					if doc != nil {
						// Add section documentation comments
						if len(doc.Comment) > 0 {
							for _, docLine := range strings.Split(doc.Comment, "\n") {
								outLines = append(outLines, pad+"# "+docLine)
							}
							outLines = append(outLines, pad+"#")
						}
						outLines = append(outLines, pad+"# type: "+doc.Type)
					}

					// never comment sections
					outLines = append(outLines, line)
					outLines = append(outLines, "")
					continue
				}
			}

			pad := strings.Repeat(" ", len(line)-len(strings.TrimLeftFunc(line, unicode.IsSpace)))

			// see if we have docs for this field
			{
				lf := strings.Fields(line)
				if len(lf) > 1 {
					doc := findDoc(cfgCur, section, lf[0])

					if doc != nil {
						// found docfield, emit doc comment
						if len(doc.Comment) > 0 {
							for _, docLine := range strings.Split(doc.Comment, "\n") {
								outLines = append(outLines, pad+"# "+docLine)
							}
							outLines = append(outLines, pad+"#")
						}

						outLines = append(outLines, pad+"# type: "+doc.Type)
					}

					if !updateOpts.noEnv {
						outLines = append(outLines, pad+"# env var: LOTUS_"+strings.ToUpper(strings.ReplaceAll(section, ".", "_"))+"_"+strings.ToUpper(lf[0]))
					}
				}
			}

			// filter lines from options
			optsFilter := updateOpts.keepUncommented != nil && updateOpts.keepUncommented(line)
			// if there is the same line in the default config, comment it out in output
			qualifiedKey := section + "." + strings.TrimSpace(line)
			if _, found := defaults[qualifiedKey]; (cfgDef == nil || found) && len(line) > 0 && !optsFilter {
				line = pad + "#" + line[len(pad):]
			}
			outLines = append(outLines, line)
			if len(line) > 0 {
				outLines = append(outLines, "")
			}
		}

		nodeStr = strings.Join(outLines, "\n")
	}

	// sanity-check that the updated config parses the same way as the current one

	if cfgDef != nil {
		cfgUpdated, err := FromReader(strings.NewReader(nodeStr), cfgDef)
		if err != nil {
			return nil, xerrors.Errorf("parsing updated config: %w", err)
		}

		opts := []cmp.Option{
			// This equality function compares big.Int
			cmpopts.IgnoreUnexported(big.Int{}),
			cmp.Comparer(func(x, y []string) bool {
				tx, ty := reflect.TypeOf(x), reflect.TypeOf(y)
				if tx.Kind() == reflect.Slice && ty.Kind() == reflect.Slice && tx.Elem().Kind() == reflect.String && ty.Elem().Kind() == reflect.String {
					sort.Strings(x)
					sort.Strings(y)
					return strings.Join(x, "\n") == strings.Join(y, "\n")
				}
				return false
			}),
			cmp.Comparer(func(x, y time.Duration) bool {
				tx, ty := reflect.TypeOf(x), reflect.TypeOf(y)
				return tx.Kind() == ty.Kind()
			}),
		}

		if !cmp.Equal(cfgUpdated, cfgCur, opts...) {
			return nil, xerrors.Errorf("updated config didn't match current config")
		}
	}

	return []byte(nodeStr), nil
}

func ConfigComment(t interface{}) ([]byte, error) {
	return ConfigUpdate(t, nil, Commented(true), DefaultKeepUncommented())
}

func findDoc(root interface{}, section, name string) *DocField {
	rt := fmt.Sprintf("%T", root)[len("*config."):]

	doc := findDocSect(rt, section, name)
	if doc != nil {
		return doc
	}

	return findDocSect("Common", section, name)
}

func findDocSect(root, section, name string) *DocField {
	path := strings.Split(section, ".")

	docSection, exists := Doc[root]
	if !exists {
		return nil
	}

	var lastField *DocField

	for _, e := range path {
		if docSection == nil {
			return nil
		}

		// Remove [e] brackets from e if required
		e = strings.Trim(e, "[]")

		found := false
		for _, field := range docSection {
			if field.Name == e {
				lastField = &field                               // Store reference to the section field
				docSection = Doc[strings.Trim(field.Type, "[]")] // Move to the next section
				found = true
				break
			}
		}

		if !found {
			return nil
		}
	}

	// If name is empty, return the last field (which represents the section itself)
	if name == "" {
		return lastField
	}

	// Otherwise, return the specific field
	for _, df := range docSection {
		if df.Name == name {
			return &df
		}
	}

	return nil
}

func FixTOML(newText string, cfg *CurioConfig) error {
	// This is a workaround to set the length of [[Addresses]] correctly before we do toml.Decode.
	// The reason this is required is that toml libraries create nil pointer to uninitialized structs.
	// This in turn causes failure to decode types like types.FIL which are struct with unexported pointer inside
	type AddressLengthDetector struct {
		Addresses []struct{} `toml:"Addresses"`
	}

	var lengthDetector AddressLengthDetector
	_, err := toml.Decode(newText, &lengthDetector)
	if err != nil {
		return xerrors.Errorf("Error decoding TOML for length detection: %w", err)
	}

	l := len(lengthDetector.Addresses)
	il := len(cfg.Addresses)

	for l > il {
		cfg.Addresses = append(cfg.Addresses, CurioAddresses{
			PreCommitControl:      []string{},
			CommitControl:         []string{},
			DealPublishControl:    []string{},
			TerminateControl:      []string{},
			DisableOwnerFallback:  false,
			DisableWorkerFallback: false,
			MinerAddresses:        []string{},
			BalanceManager:        DefaultBalanceManager(),
		})
		il++
	}
	return nil
}

func LoadConfigWithUpgrades(text string, curioConfigWithDefaults *CurioConfig) (toml.MetaData, error) {
	// allow migration from old config format that was limited to 1 wallet setup.
	newText := strings.Join(lo.Map(strings.Split(text, "\n"), func(line string, _ int) string {
		if strings.EqualFold(line, "[addresses]") {
			return "[[addresses]]"
		}
		return line
	}), "\n")

	err := FixTOML(newText, curioConfigWithDefaults)
	if err != nil {
		return toml.MetaData{}, err
	}

	return toml.Decode(newText, &curioConfigWithDefaults)
}

type ConfigText struct {
	Title  string
	Config string
}

// GetConfigs returns the configs in the order of the layers
func GetConfigs(ctx context.Context, db *harmonydb.DB, layers []string) ([]ConfigText, error) {
	inputMap := map[string]int{}
	for i, layer := range layers {
		inputMap[layer] = i
	}

	layers = append([]string{"base"}, layers...) // Always stack on top of "base" layer

	var configs []ConfigText
	err := db.Select(ctx, &configs, `SELECT title, config FROM harmony_config WHERE title IN ($1)`, strings.Join(layers, ","))
	if err != nil {
		return nil, err
	}
	result := make([]ConfigText, len(layers))
	for _, config := range configs {
		index, ok := inputMap[config.Title]
		if !ok {
			if config.Title == "base" {
				return nil, errors.New(`curio defaults to a layer named 'base'. 
				Either use 'migrate' command or edit a base.toml and upload it with: curio config set base.toml`)

			}
			return nil, fmt.Errorf("missing layer %s", config.Title)
		}
		result[index] = config
	}
	return result, nil
}

func ApplyLayers(ctx context.Context, curioConfig *CurioConfig, layers []ConfigText) error {
	have := []string{}
	for _, layer := range layers {
		meta, err := LoadConfigWithUpgrades(layer.Config, curioConfig)
		if err != nil {
			return fmt.Errorf("could not read layer, bad toml %s: %w", layer, err)
		}
		for _, k := range meta.Keys() {
			have = append(have, strings.Join(k, " "))
		}
		logger.Debugf("Using layer %s, config %v", layer, curioConfig)
	}
	_ = have // FUTURE: verify that required fields are here.
	// If config includes 3rd-party config, consider JSONSchema as a way that
	// 3rd-parties can dynamically include config requirements and we can
	// validate the config. Because of layering, we must validate @ startup.
	return nil
}
