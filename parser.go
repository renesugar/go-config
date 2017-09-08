package config

import (
	"flag"
	"fmt"
	"io/ioutil"
	"strings"
	"unicode"
)

// Parser is an parser interface.
type Parser interface {
	// Name returns the name of the parser to identify it.
	Name() string

	// If the parser needs some configurations, it can return all those names
	// by the method, and mark whether they must be required.
	//
	// For example, the method returns {"ip": true, "port": false},
	// which indicates the configuration must pass the parser the value of the
	// option 'ip' when calling the method Parse, but the value of 'port' is
	// optional. These option values will be acquired from the default group.
	GetKeys() map[string]bool

	// Parse the value of the registered options.
	//
	// The first argument, defaultGroupName, is the name of the default group.
	//
	// The second argument, opts, is the parsed option information. The key is
	// the group name, and the value is the parsed option list.
	//
	// The third argument, conf, is the configuration information. The key is
	// the key of the map value returned by the method GetKeys(), and the value
	// is pulled from the default group which has just been parsed.
	//
	// For example, for the redis parser, its the method GetKeys() maybe
	// returns {"connection": false}, then there may be a key-value pair,
	// {"connection": "redis://1.2.3.4:6379/1"} in the argument conf of the
	// method Parse, but there may be not. Ff so, the redis parser maybe use
	/// the default value, "redis://127.0.0.1:6379/0". For the builtin ini
	// parser, NewSimpleIniParser, it's "config-file" by default, but you can
	// change it when a new ini parser is created.
	//
	// For the first result, a map, the key is the group name, and the value is
	// the key-value pairs about the options defined in that group, which the
	// option key is the name of the registered option.
	//
	// If a certain option has no value, the parser should not return a default
	// one instead.
	Parse(defaultGroupName string, opts map[string][]Opt, conf map[string]string) (
		results map[string]map[string]string, err error)
}

// CliParser is an interface to parse the CLI arguments.
type CliParser interface {
	// Name returns the name of the CLI parser to identify a CLI parser.
	Name() string

	// The argument is the CLI arguments, but it may be nil.
	//
	// The key of the result map is the group name, and the value of that is
	// the key-value pairs, which the key is the name of the registered option.
	//
	// If a certain option has no value, the CLI parser should not return a
	// default one.

	// Parse the value of the registered CLI options.
	//
	// The first argument, defaultGroupName, is the name of the default group.
	//
	// The second argument, opts, is the parsed option information. The key is
	// the group name, and the value is the parsed option list.
	//
	// The third argument, arguments, is the CLI arguments, which must be
	// a string slice, not nil, but it maybe have no elements. The parser
	// implementor should use os.Args[1:] when it's nil or empty, because
	// it has been confirmed.
	//
	// For the first result, a map, the key is the group name, and the value
	// is the key-value pairs about the options defined in that group, which
	// the option key is the name of the registered option.
	//
	// The second result is the rest of the CLI arguments, which are not the
	// options starting with the prefix "-", "--" or others, etc.
	//
	// If a certain option has no value, the parser should not return a default
	// one instead.
	Parse(defaultGroupName string, opts map[string][]Opt, arguments []string) (
		results map[string]map[string]string, args []string, err error)
}

type flagParser struct {
	name       string
	errhandler flag.ErrorHandling
}

// NewFlagCliParser returns a new CLI parser based on flag.FlagSet.
//
// The arguments is the same as that of flag.NewFlagSet(), but if the name is
// "", it will be filepath.Base(os.Args[0]).
func NewFlagCliParser(appName string, errhandler flag.ErrorHandling) CliParser {
	return flagParser{
		name:       appName,
		errhandler: errhandler,
	}
}

func (f flagParser) Name() string {
	return "flag"
}

func (f flagParser) Parse(_default string, opts map[string][]Opt, as []string) (
	results map[string]map[string]string, args []string, err error) {
	// Register the options into flag.FlagSet.
	flagSet := flag.NewFlagSet(f.name, f.errhandler)
	name2group := make(map[string]string, 8)
	name2opt := make(map[string]string, 8)
	for group, _opts := range opts {
		for _, opt := range _opts {
			name := opt.GetName()
			if group != _default {
				name = fmt.Sprintf("%s_%s", group, name)
			}
			name2group[name] = group
			name2opt[name] = opt.GetName()

			if opt.IsBool() {
				var _default bool
				if v := opt.GetDefault(); v != nil {
					_default = v.(bool)
				}
				flagSet.Bool(name, _default, opt.GetHelp())
			} else {
				flagSet.String(name, "", opt.GetHelp())
			}
		}
	}

	// Parse the CLI arguments.
	if err = flagSet.Parse(as); err != nil {
		return
	}

	// Acquire the result.
	args = flagSet.Args()
	results = make(map[string]map[string]string, len(name2group))
	flagSet.Visit(func(fg *flag.Flag) {
		if group, ok := results[name2group[fg.Name]]; ok {
			group[name2opt[fg.Name]] = fg.Value.String()
		} else {
			results[name2group[fg.Name]] = map[string]string{
				name2opt[fg.Name]: fg.Value.String(),
			}
		}
	})
	return
}

type iniParser struct {
	sep     string
	optName string
}

// NewSimpleIniParser returns a new ini parser based on the file.
//
// The argument is the option name which the parser needs. It should be
// registered, and parsed before this parser runs.
//
// The ini parser supports the line comments starting with "#" or "//".
// The key and the value is separated by an equal sign, that's =.
//
// Notice: the options that have not been assigned to a certain group will be
// divided into the default group.
func NewSimpleIniParser(optName string) Parser {
	return iniParser{optName: optName, sep: "="}
}

func (p iniParser) Name() string {
	return "ini"
}

func (p iniParser) GetKeys() map[string]bool {
	return map[string]bool{
		p.optName: false,
	}
}

func (p iniParser) Parse(_default string, opts map[string][]Opt,
	conf map[string]string) (results map[string]map[string]string, err error) {
	// Read the content of the config file.
	filename, ok := conf[p.optName]
	if !ok || len(filename) == 0 {
		return
	}
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return
	}

	// Convert the format of the optons.
	options := make(map[string]map[string]struct{}, len(opts))
	for group, _opts := range opts {
		g, ok := options[group]
		if !ok {
			g = make(map[string]struct{}, len(_opts))
			options[group] = g
		}
		for _, opt := range _opts {
			g[opt.GetName()] = struct{}{}
		}
	}

	// Parse the config file.
	group := make(map[string]string, 8)
	results = make(map[string]map[string]string, len(options))
	results[_default] = group
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Ignore the empty line.
		if len(line) == 0 {
			continue
		}

		// Ignore the line comments starting with "#" or "//".
		if (line[0] == '#') || (len(line) > 1 && line[0] == '/' && line[1] == '/') {
			continue
		}

		// Start a new group
		if line[0] == '[' && line[len(line)-1] == ']' {
			gname := strings.TrimSpace(line[1 : len(line)-1])
			if gname == "" {
				return nil, fmt.Errorf("the group is empty")
			}
			if group = results[gname]; group == nil {
				group = make(map[string]string, 4)
				results[gname] = group
			}
			continue
		}

		n := strings.Index(line, p.sep)
		if n == -1 {
			err = fmt.Errorf("the line misses the separator %s", p.sep)
			return
		}

		key := strings.TrimSpace(line[0:n])
		for _, r := range key {
			if !unicode.IsNumber(r) && !unicode.IsLetter(r) {
				err = fmt.Errorf("the key is not an valid identifier")
				return
			}
		}
		group[key] = strings.TrimSpace(line[n+len(p.sep) : len(line)])
	}
	return
}
