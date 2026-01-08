// Package rconfig implements a CLI configuration reader with struct-embedded
// defaults, environment variables and posix compatible flag parsing using
// the pflag library.
package rconfig

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"
	validator "github.com/go-playground/validator/v10"
)

type afterFunc func() error

var (
	autoEnv          bool
	fs               *pflag.FlagSet
	variableDefaults map[string]string

	timeParserFormats = []string{
		// Default constants
		time.RFC3339Nano, time.RFC3339,
		time.RFC1123Z, time.RFC1123,
		time.RFC822Z, time.RFC822,
		time.RFC850, time.RubyDate, time.UnixDate, time.ANSIC,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		// More uncommon time formats
		"2006-01-02 15:04:05", "2006-01-02 15:04:05Z07:00", // Simplified ISO time format
		"01/02/2006 15:04:05", "01/02/2006 15:04:05Z07:00", // US time format
		"02.01.2006 15:04:05", "02.01.2006 15:04:05Z07:00", // DE time format
	}
)

func init() {
	variableDefaults = make(map[string]string)
}

// RegisterFlags registers all flags from the config struct to the provided FlagSet.
// This is useful for integrating with Cobra or other CLI frameworks. The flags will
// be registered with their default values from the struct tags. After the framework
// parses the flags, call ApplyEnvAndDefaults to apply environment variables and
// vardefaults to flags that weren't explicitly set.
func RegisterFlags(config interface{}, flagSet *pflag.FlagSet) error {
	if reflect.TypeOf(config).Kind() != reflect.Ptr {
		return errors.New("RegisterFlags: config must be a pointer")
	}

	if reflect.ValueOf(config).Elem().Kind() != reflect.Struct {
		return errors.New("RegisterFlags: config must be a pointer to struct")
	}

	_, err := execTags(config, flagSet)
	return err
}

// ApplyEnvAndDefaults applies environment variables and vardefaults to a config struct
// whose flags have already been registered and parsed by an external FlagSet (e.g., Cobra).
// This maintains the precedence: flag (if changed) > env > vardefault > default.
// Only fields where the flag was NOT explicitly set by the user will be updated.
func ApplyEnvAndDefaults(config interface{}, flagSet *pflag.FlagSet) error {
	if reflect.TypeOf(config).Kind() != reflect.Ptr {
		return errors.New("ApplyEnvAndDefaults: config must be a pointer")
	}

	if reflect.ValueOf(config).Elem().Kind() != reflect.Struct {
		return errors.New("ApplyEnvAndDefaults: config must be a pointer to struct")
	}

	return applyEnvAndDefaults(reflect.ValueOf(config).Elem(), reflect.TypeOf(config).Elem(), flagSet)
}

func applyEnvAndDefaults(val reflect.Value, typ reflect.Type, flagSet *pflag.FlagSet) error {
	for i := 0; i < val.NumField(); i++ {
		valField := val.Field(i)
		typeField := typ.Field(i)

		// Handle nested structs recursively
		if typeField.Type.Kind() == reflect.Struct && typeField.Type != reflect.TypeOf(time.Time{}) {
			if err := applyEnvAndDefaults(valField, typeField.Type, flagSet); err != nil {
				return err
			}
			continue
		}

		// Get value from vardefault/env with fallback to default tag
		value := varDefault(typeField.Tag.Get("vardefault"), typeField.Tag.Get("default"))
		value = envDefault(typeField, value)

		// Check if this field has a flag
		flagName := typeField.Tag.Get("flag")
		if flagName != "" {
			parts := strings.Split(flagName, ",")
			flag := flagSet.Lookup(parts[0])

			// If flag was explicitly set by user, skip this field (maintain precedence)
			if flag != nil && flag.Changed {
				continue
			}

			// Flag exists but wasn't set by user - update it with env/vardefault
			if flag != nil {
				if err := flag.Value.Set(value); err != nil {
					return fmt.Errorf("setting flag %s: %w", parts[0], err)
				}
				continue
			}
		}

		// No flag or flag not registered - set field directly (for env/vardefault-only fields)
		if err := setFieldValue(valField, typeField.Type, value); err != nil {
			return fmt.Errorf("setting field %s: %w", typeField.Name, err)
		}
	}

	return nil
}

func setFieldValue(field reflect.Value, fieldType reflect.Type, value string) error {
	// Handle special types first
	switch fieldType {
	case reflect.TypeOf(time.Duration(0)):
		v, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		field.Set(reflect.ValueOf(v))
		return nil

	case reflect.TypeOf(time.Time{}):
		// Try parsing as timestamp first
		if ts, err := strconv.ParseInt(value, 10, 64); err == nil {
			field.Set(reflect.ValueOf(time.Unix(ts, 0)))
			return nil
		}
		// Try time format parsing
		for _, format := range timeParserFormats {
			if t, err := time.Parse(format, value); err == nil {
				field.Set(reflect.ValueOf(t))
				return nil
			}
		}
		return fmt.Errorf("unable to parse time: %s", value)
	}

	// Handle basic types
	switch fieldType.Kind() {
	case reflect.String:
		field.SetString(value)

	case reflect.Bool:
		field.SetBool(value == "true")

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, err := parseIntForType(value, 10, fieldType.Kind())
		if err != nil {
			return err
		}
		field.SetInt(v)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v, err := parseUintForType(value, 10, fieldType.Kind())
		if err != nil {
			return err
		}
		field.SetUint(v)

	case reflect.Float32, reflect.Float64:
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return err
		}
		field.SetFloat(v)
	}

	return nil
}

// Parse takes the pointer to a struct filled with variables which should be read
// from ENV, default or flag. The precedence in this is flag > ENV > default. So
// if a flag is specified on the CLI it will overwrite the ENV and otherwise ENV
// overwrites the default specified.
//
// For your configuration struct you can use the following struct-tags to control
// the behavior of rconfig:
//
//	default: Set a default value
//	vardefault: Read the default value from the variable defaults
//	env: Read the value from this environment variable
//	flag: Flag to read in format "long,short" (for example "listen,l")
//	description: A help text for Usage output to guide your users
//
// The format you need to specify those values you can see in the example to this
// function.
func Parse(config interface{}) error {
	return parse(config, nil)
}

// ParseAndValidate works exactly like Parse but implements an additional run of
// the go-validator package on the configuration struct. Therefore additional struct
// tags are supported like described in the readme file of the go-validator package:
//
// https://github.com/go-validator/validator/tree/v2#usage
func ParseAndValidate(config interface{}) error {
	return parseAndValidate(config, nil)
}

// Args returns the non-flag command-line arguments.
func Args() []string {
	return fs.Args()
}

// AddTimeParserFormats adds custom formats to parse time.Time fields
func AddTimeParserFormats(f ...string) {
	timeParserFormats = append(timeParserFormats, f...)
}

// AutoEnv enables or disables automated env variable guessing. If no `env` struct
// tag was set and AutoEnv is enabled the env variable name is derived from the
// name of the field: `MyFieldName` will get `MY_FIELD_NAME`
func AutoEnv(enable bool) {
	autoEnv = enable
}

// Usage prints a basic usage with the corresponding defaults for the flags to
// os.Stdout. The defaults are derived from the `default` struct-tag and the ENV.
func Usage() {
	if fs != nil && fs.Parsed() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		fs.PrintDefaults()
	}
}

// SetVariableDefaults presets the parser with a map of default values to be used
// when specifying the vardefault tag
func SetVariableDefaults(defaults map[string]string) {
	variableDefaults = defaults
}

//revive:disable-next-line:confusing-naming // The public function is only a wrapper with less args
func parseAndValidate(in interface{}, args []string) (err error) {
	if err = parse(in, args); err != nil {
		return err
	}

	if err = validator.New().Struct(in); err != nil {
		return fmt.Errorf("validating values: %w", err)
	}

	return nil
}

//revive:disable-next-line:confusing-naming // The public function is only a wrapper with less args
func parse(in interface{}, args []string) error {
	if args == nil {
		args = os.Args
	}

	fs = pflag.NewFlagSet(os.Args[0], pflag.ExitOnError)
	afterFuncs, err := execTags(in, fs)
	if err != nil {
		return err
	}

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parsing flag-set: %w", err)
	}

	for _, f := range afterFuncs {
		if err := f(); err != nil {
			return err
		}
	}

	return nil
}

//nolint:funlen,gocognit,gocyclo // Hard to split
func execTags(in interface{}, fs *pflag.FlagSet) ([]afterFunc, error) {
	if reflect.TypeOf(in).Kind() != reflect.Ptr {
		return nil, errors.New("calling parser with non-pointer")
	}

	if reflect.ValueOf(in).Elem().Kind() != reflect.Struct {
		return nil, errors.New("calling parser with pointer to non-struct")
	}

	afterFuncs := []afterFunc{}

	st := reflect.ValueOf(in).Elem()
	for i := 0; i < st.NumField(); i++ {
		valField := st.Field(i)
		typeField := st.Type().Field(i)

		if typeField.Tag.Get("default") == "" && typeField.Tag.Get("env") == "" && typeField.Tag.Get("flag") == "" && typeField.Type.Kind() != reflect.Struct {
			// None of our supported tags is present and it's not a sub-struct
			continue
		}

		value := varDefault(typeField.Tag.Get("vardefault"), typeField.Tag.Get("default"))
		value = envDefault(typeField, value)
		parts := strings.Split(typeField.Tag.Get("flag"), ",")

		switch typeField.Type {
		case reflect.TypeOf(time.Duration(0)):
			v, err := time.ParseDuration(value)
			if err != nil {
				if value != "" {
					return nil, fmt.Errorf("parsing time.Duration: %w", err)
				}
				v = time.Duration(0)
			}

			if typeField.Tag.Get("flag") != "" {
				desc := buildDescription(typeField)
				if len(parts) == 1 {
					fs.DurationVar(valField.Addr().Interface().(*time.Duration), parts[0], v, desc)
				} else {
					fs.DurationVarP(valField.Addr().Interface().(*time.Duration), parts[0], parts[1], v, desc)
				}
			} else {
				valField.Set(reflect.ValueOf(v))
			}
			continue

		case reflect.TypeOf(time.Time{}):
			var sVar string

			if typeField.Tag.Get("flag") != "" {
				desc := buildDescription(typeField)
				if len(parts) == 1 {
					fs.StringVar(&sVar, parts[0], value, desc)
				} else {
					fs.StringVarP(&sVar, parts[0], parts[1], value, desc)
				}
			} else {
				sVar = value
			}

			afterFuncs = append(afterFuncs, func(valField reflect.Value, sVar *string) func() error {
				return func() error {
					if *sVar == "" {
						// No time, no problem
						return nil
					}

					// Check whether we could have a timestamp
					if ts, err := strconv.ParseInt(*sVar, 10, 64); err == nil {
						t := time.Unix(ts, 0)
						valField.Set(reflect.ValueOf(t))
						return nil
					}

					// We haven't so lets walk through possible time formats
					matched := false
					for _, tf := range timeParserFormats {
						if t, err := time.Parse(tf, *sVar); err == nil {
							valField.Set(reflect.ValueOf(t))
							return nil
						}
					}

					if !matched {
						return fmt.Errorf("value %q did not match expected time formats", *sVar)
					}

					return nil
				}
			}(valField, &sVar))

			continue
		}

		switch typeField.Type.Kind() {
		case reflect.String:
			if typeField.Tag.Get("flag") != "" {
				desc := buildDescription(typeField)
				if len(parts) == 1 {
					fs.StringVar(valField.Addr().Interface().(*string), parts[0], value, desc)
				} else {
					fs.StringVarP(valField.Addr().Interface().(*string), parts[0], parts[1], value, desc)
				}
			} else {
				valField.SetString(value)
			}

		case reflect.Bool:
			v := value == "true"
			if typeField.Tag.Get("flag") != "" {
				desc := buildDescription(typeField)
				if len(parts) == 1 {
					fs.BoolVar(valField.Addr().Interface().(*bool), parts[0], v, desc)
				} else {
					fs.BoolVarP(valField.Addr().Interface().(*bool), parts[0], parts[1], v, desc)
				}
			} else {
				valField.SetBool(v)
			}

		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			vt, err := parseIntForType(value, 10, typeField.Type.Kind()) //nolint:mnd
			if err != nil {
				if value != "" {
					return nil, fmt.Errorf("parsing int: %w", err)
				}
				vt = 0
			}
			if typeField.Tag.Get("flag") != "" {
				registerFlagInt(typeField.Type.Kind(), fs, valField.Addr().Interface(), parts, vt, buildDescription(typeField))
			} else {
				valField.SetInt(vt)
			}

		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			vt, err := parseUintForType(value, 10, typeField.Type.Kind()) //nolint:mnd
			if err != nil {
				if value != "" {
					return nil, fmt.Errorf("parsing uint: %w", err)
				}
				vt = 0
			}
			if typeField.Tag.Get("flag") != "" {
				registerFlagUint(typeField.Type.Kind(), fs, valField.Addr().Interface(), parts, vt, buildDescription(typeField))
			} else {
				valField.SetUint(vt)
			}

		case reflect.Float32, reflect.Float64:
			vt, err := strconv.ParseFloat(value, 64)
			if err != nil {
				if value != "" {
					return nil, fmt.Errorf("parsing float: %w", err)
				}
				vt = 0.0
			}
			if typeField.Tag.Get("flag") != "" {
				registerFlagFloat(typeField.Type.Kind(), fs, valField.Addr().Interface(), parts, vt, buildDescription(typeField))
			} else {
				valField.SetFloat(vt)
			}

		case reflect.Struct:
			afs, err := execTags(valField.Addr().Interface(), fs)
			if err != nil {
				return nil, err
			}
			afterFuncs = append(afterFuncs, afs...)

		case reflect.Slice:
			switch typeField.Type.Elem().Kind() {
			case reflect.Int:
				def := []int{}
				for _, v := range strings.Split(value, ",") {
					it, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
					if err != nil {
						return nil, fmt.Errorf("parsing int: %w", err)
					}
					def = append(def, int(it))
				}
				desc := buildDescription(typeField)
				if len(parts) == 1 {
					fs.IntSliceVar(valField.Addr().Interface().(*[]int), parts[0], def, desc)
				} else {
					fs.IntSliceVarP(valField.Addr().Interface().(*[]int), parts[0], parts[1], def, desc)
				}
			case reflect.String:
				del := typeField.Tag.Get("delimiter")
				if len(del) == 0 {
					del = ","
				}
				def := []string{}
				if value != "" {
					def = strings.Split(value, del)
				}
				desc := buildDescription(typeField)
				if len(parts) == 1 {
					fs.StringSliceVar(valField.Addr().Interface().(*[]string), parts[0], def, desc)
				} else {
					fs.StringSliceVarP(valField.Addr().Interface().(*[]string), parts[0], parts[1], def, desc)
				}
			}
		}
	}

	return afterFuncs, nil
}

func registerFlagFloat(t reflect.Kind, fs *pflag.FlagSet, field interface{}, parts []string, vt float64, desc string) {
	switch t {
	case reflect.Float32:
		if len(parts) == 1 {
			fs.Float32Var(field.(*float32), parts[0], float32(vt), desc)
		} else {
			fs.Float32VarP(field.(*float32), parts[0], parts[1], float32(vt), desc)
		}
	case reflect.Float64:
		if len(parts) == 1 {
			fs.Float64Var(field.(*float64), parts[0], vt, desc)
		} else {
			fs.Float64VarP(field.(*float64), parts[0], parts[1], vt, desc)
		}
	}
}

func registerFlagInt(t reflect.Kind, fs *pflag.FlagSet, field interface{}, parts []string, vt int64, desc string) {
	switch t {
	case reflect.Int:
		if len(parts) == 1 {
			fs.IntVar(field.(*int), parts[0], int(vt), desc)
		} else {
			fs.IntVarP(field.(*int), parts[0], parts[1], int(vt), desc)
		}
	case reflect.Int8:
		if len(parts) == 1 {
			fs.Int8Var(field.(*int8), parts[0], int8(vt), desc) //#nosec:G115 - Variable is guaranteed to match by strconv
		} else {
			fs.Int8VarP(field.(*int8), parts[0], parts[1], int8(vt), desc) //#nosec:G115 - Variable is guaranteed to match by strconv
		}
	case reflect.Int16:
		if len(parts) == 1 {
			fs.Int16Var(field.(*int16), parts[0], int16(vt), desc) //#nosec:G115 - Variable is guaranteed to match by strconv
		} else {
			fs.Int16VarP(field.(*int16), parts[0], parts[1], int16(vt), desc) //#nosec:G115 - Variable is guaranteed to match by strconv
		}
	case reflect.Int32:
		if len(parts) == 1 {
			fs.Int32Var(field.(*int32), parts[0], int32(vt), desc) //#nosec:G115 - Variable is guaranteed to match by strconv
		} else {
			fs.Int32VarP(field.(*int32), parts[0], parts[1], int32(vt), desc) //#nosec:G115 - Variable is guaranteed to match by strconv
		}
	case reflect.Int64:
		if len(parts) == 1 {
			fs.Int64Var(field.(*int64), parts[0], vt, desc)
		} else {
			fs.Int64VarP(field.(*int64), parts[0], parts[1], vt, desc)
		}
	}
}

func registerFlagUint(t reflect.Kind, fs *pflag.FlagSet, field interface{}, parts []string, vt uint64, desc string) {
	switch t {
	case reflect.Uint:
		if len(parts) == 1 {
			fs.UintVar(field.(*uint), parts[0], uint(vt), desc)
		} else {
			fs.UintVarP(field.(*uint), parts[0], parts[1], uint(vt), desc)
		}
	case reflect.Uint8:
		if len(parts) == 1 {
			fs.Uint8Var(field.(*uint8), parts[0], uint8(vt), desc) //#nosec:G115 - Variable is guaranteed to match by strconv
		} else {
			fs.Uint8VarP(field.(*uint8), parts[0], parts[1], uint8(vt), desc) //#nosec:G115 - Variable is guaranteed to match by strconv
		}
	case reflect.Uint16:
		if len(parts) == 1 {
			fs.Uint16Var(field.(*uint16), parts[0], uint16(vt), desc) //#nosec:G115 - Variable is guaranteed to match by strconv
		} else {
			fs.Uint16VarP(field.(*uint16), parts[0], parts[1], uint16(vt), desc) //#nosec:G115 - Variable is guaranteed to match by strconv
		}
	case reflect.Uint32:
		if len(parts) == 1 {
			fs.Uint32Var(field.(*uint32), parts[0], uint32(vt), desc) //#nosec:G115 - Variable is guaranteed to match by strconv
		} else {
			fs.Uint32VarP(field.(*uint32), parts[0], parts[1], uint32(vt), desc) //#nosec:G115 - Variable is guaranteed to match by strconv
		}
	case reflect.Uint64:
		if len(parts) == 1 {
			fs.Uint64Var(field.(*uint64), parts[0], vt, desc)
		} else {
			fs.Uint64VarP(field.(*uint64), parts[0], parts[1], vt, desc)
		}
	}
}

func envDefault(field reflect.StructField, def string) string {
	value := def

	env := field.Tag.Get("env")
	if env == "" && autoEnv {
		env = deriveEnvVarName(field.Name)
	}

	if env != "" {
		// Use LookupEnv to distinguish between unset and empty
		if e, ok := os.LookupEnv(env); ok {
			value = e
		}
	}

	return value
}

func varDefault(name, def string) string {
	value := def

	if name != "" {
		if v, ok := variableDefaults[name]; ok {
			value = v
		}
	}

	return value
}

func buildDescription(field reflect.StructField) string {
	desc := field.Tag.Get("description")
	env := field.Tag.Get("env")
	if env == "" && autoEnv {
		env = deriveEnvVarName(field.Name)
	}
	if env != "" {
		if desc != "" {
			desc += fmt.Sprintf(" (ENV: %s)", env)
		} else {
			desc = fmt.Sprintf("(ENV: %s)", env)
		}
	}
	return desc
}
