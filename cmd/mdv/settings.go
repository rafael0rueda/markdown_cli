package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/rafael0rueda/markdown_cli/internal/settings"
)

// defaultConfigLabel is how the default configuration location is described in
// help text. The real path depends on the environment; see settings.Path.
const defaultConfigLabel = "~/.config/mdv/config"

// flagAliases maps a shorthand flag onto the flag it abbreviates. Both write
// the same variable, so "-w 70" on the command line has to count as having
// set width, or a width in the file would overwrite it.
var flagAliases = map[string]string{"w": "width"}

// commandLineOnly lists flags that make no sense as a standing default: they
// choose the file itself, or make mdv do something other than render a
// document. Setting one in the file is almost certainly a mistake, and saying
// so beats a configuration that makes every run print a version number.
var commandLineOnly = map[string]bool{
	"config":    true,
	"no-config": true,
	"version":   true,
	"caps":      true,
}

// applySettings fills in flags from the configuration file, leaving alone any
// that were given on the command line.
//
// It runs after the command line has been parsed rather than before, so that
// the command line can say which file to read, and so that precedence is a
// simple rule - explicit beats configured beats built in - rather than an
// effect of ordering.
func applySettings(fs *flag.FlagSet, cfg *config) error {
	if cfg.noConfig {
		return nil
	}
	path, explicit := cfg.configPath, cfg.configPath != ""
	if !explicit {
		path, explicit = settings.Path()
	}
	if path == "" {
		return nil
	}

	list, err := settings.Load(path)
	if err != nil {
		if !explicit && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("config: %w", err)
	}

	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[canonicalFlag(f.Name)] = true })

	for _, s := range list {
		fail := func(format string, args ...any) error {
			return &settings.Error{Path: path, Line: s.Line, Err: fmt.Errorf(format, args...)}
		}
		f := fs.Lookup(s.Key)
		if f == nil {
			return fail("unknown setting %q", s.Key)
		}
		if commandLineOnly[f.Name] {
			return fail("%q can only be given on the command line", s.Key)
		}
		if given[canonicalFlag(f.Name)] {
			continue
		}
		value := s.Value
		if s.Bare {
			if !isBoolFlag(f) {
				return fail("%q needs a value, as in %s = ...", s.Key, s.Key)
			}
			value = "true"
		}
		if err := fs.Set(f.Name, value); err != nil {
			// Set on its own says only "parse error"; the command line would
			// have named the value and the flag, so do the same here.
			return fail("invalid value %q for %s: %v", value, s.Key, err)
		}
	}
	return nil
}

func canonicalFlag(name string) string {
	if long, ok := flagAliases[name]; ok {
		return long
	}
	return name
}

func isBoolFlag(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

// buildVersion reports the version this binary was built as.
//
// Release builds have it stamped in with -ldflags. A plain "go install
// ...@v1.2.0" does not, but the go command records the module version in the
// binary, so that is used rather than reporting "dev" for a tagged release.
func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	return versionFrom(version, info.Main.Version)
}

func versionFrom(stamped, module string) string {
	if stamped != "dev" || module == "" || module == "(devel)" {
		return stamped
	}
	return module
}
