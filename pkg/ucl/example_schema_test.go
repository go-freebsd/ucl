package ucl_test

import (
	"errors"
	"fmt"
	"time"

	"github.com/go-freebsd/ucl/pkg/ucl"
)

// The struct tags that name the fields also say what a valid document looks
// like, so there is no second schema to write.
func ExampleValidate() {
	type Database struct {
		Host string `ucl:"host,required,pattern=^[a-z0-9.-]+$"`
		Port int    `ucl:"port,min=1,max=65535"`
	}
	type Config struct {
		Workers  int           `ucl:"workers,min=1,max=64"`
		Timeout  time.Duration `ucl:"timeout,min=1s,max=5min"`
		Mode     string        `ucl:"mode,enum=fast|safe"`
		Database Database      `ucl:"database"`
	}

	obj, err := ucl.ParseString(`
		workers = 128
		timeout = 20min
		mode    = quick
		database { host = "DB Server" }
	`)
	if err != nil {
		panic(err)
	}
	fmt.Println(ucl.Validate(obj, Config{}))
	// Output:
	// ucl: 4 validation errors:
	// 	workers: must be at most 64, got 128
	// 	timeout: must be at most 5m0s, got 20m0s
	// 	mode: must be one of fast, safe, got "quick"
	// 	database.host: must match ^[a-z0-9.-]+$
}

// RejectUnknownKeys catches a misspelled option, which is otherwise ignored
// in silence.
func ExampleRejectUnknownKeys() {
	type Config struct {
		Workers int `ucl:"workers"`
	}

	obj, err := ucl.ParseString("wrokers = 4")
	if err != nil {
		panic(err)
	}

	fmt.Println(ucl.Validate(obj, Config{}))
	fmt.Println(ucl.Validate(obj, Config{}, ucl.RejectUnknownKeys()))
	// Output:
	// <nil>
	// ucl: wrokers: unknown key
}

// Each problem can be handled on its own rather than as one block of text.
func ExampleValidationErrors() {
	type Config struct {
		Workers int    `ucl:"workers,min=1,max=64"`
		Mode    string `ucl:"mode,enum=fast|safe"`
		Name    string `ucl:"name,required"`
	}

	obj, err := ucl.ParseString("workers = 128\nmode = quick")
	if err != nil {
		panic(err)
	}

	var verrs ucl.ValidationErrors
	if errors.As(ucl.Validate(obj, Config{}), &verrs) {
		fmt.Println(len(verrs), "problems")
		for _, e := range verrs {
			fmt.Printf("  %-8s %s\n", e.Path, e.Msg)
		}
	}
	// Output:
	// 3 problems
	//   workers  must be at most 64, got 128
	//   mode     must be one of fast, safe, got "quick"
	//   name     is required
}

// enum, pattern and multipleof describe a single value, so on a list they are
// applied to each element and a problem names the element it was found in.
// min, max and len go on counting the elements.
func ExampleValidate_lists() {
	type Config struct {
		Modes  []string          `ucl:"modes,min=1,enum=fast|safe"`
		Hosts  []string          `ucl:"hosts,pattern=^[a-z.]+$"`
		Levels map[string]string `ucl:"levels,enum=debug|info"`
	}

	obj, err := ucl.ParseString(`
		modes  = [fast, quick]
		hosts  = [db.example.net, "DB Server"]
		levels { app = debug, db = chatty }
	`)
	if err != nil {
		panic(err)
	}
	fmt.Println(ucl.Validate(obj, Config{}))
	// Output:
	// ucl: 3 validation errors:
	// 	modes[1]: must be one of fast, safe, got "quick"
	// 	hosts[1]: must match ^[a-z.]+$
	// 	levels.db: must be one of debug, info, got "chatty"
}

// A default= that the constraints on the same field reject is a contradiction
// in the struct, not in the document, so it is reported whatever the document
// contains. The mistake is then found by a test rather than by whatever reads
// the decoded value.
func ExampleValidate_defaults() {
	type Config struct {
		Workers int    `ucl:"workers,default=0,min=1,max=64"`
		Mode    string `ucl:"mode,default=turbo,enum=fast|safe"`
	}

	obj, err := ucl.ParseString("workers = 8\nmode = fast")
	if err != nil {
		panic(err)
	}
	fmt.Println(ucl.Validate(obj, Config{}))
	// Output:
	// ucl: 2 validation errors:
	// 	workers: invalid default: must be at least 1, got 0
	// 	mode: invalid default: must be one of fast, safe, got "turbo"
}
