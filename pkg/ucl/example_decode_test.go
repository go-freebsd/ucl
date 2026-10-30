package ucl_test

import (
	"fmt"
	"time"

	"github.com/go-freebsd/ucl/pkg/ucl"
)

// Decoding works like encoding/json. Field names come from "ucl" tags, then
// from "json" tags, then from the field name itself.
func ExampleUnmarshal() {
	var cfg struct {
		Repos   []string      `ucl:"REPOS_DIR"`
		Timeout time.Duration `ucl:"FETCH_TIMEOUT"`
		Debug   bool          `ucl:"DEBUG_LEVEL"`
	}
	src := []byte("REPOS_DIR = /etc/pkg\nFETCH_TIMEOUT = 2min\nDEBUG_LEVEL = off\n")
	if err := ucl.Unmarshal(src, &cfg); err != nil {
		panic(err)
	}
	fmt.Printf("%v %v %v\n", cfg.Repos, cfg.Timeout, cfg.Debug)
	// Output:
	// [/etc/pkg] 2m0s false
}

// Decode fills a value from a document that has already been parsed, which is
// what to use when the same document is also inspected or validated.
func ExampleObject_Decode() {
	type Database struct {
		Host string `ucl:"host"`
		Port int    `ucl:"port"`
	}
	type Config struct {
		Name     string            `ucl:"name"`
		Workers  int               `ucl:"workers"`
		Database Database          `ucl:"database"`
		Env      map[string]string `ucl:"env"`
	}

	obj, err := ucl.ParseString(`
		name = web
		workers = 4
		database { host = db.example.net, port = 5432 }
		env { TZ = UTC }
	`)
	if err != nil {
		panic(err)
	}

	// Fields the document does not mention keep the value they had.
	cfg := Config{Name: "unnamed"}
	if err := obj.Decode(&cfg); err != nil {
		panic(err)
	}

	fmt.Printf("%s %d %s:%d %v\n", cfg.Name, cfg.Workers,
		cfg.Database.Host, cfg.Database.Port, cfg.Env)
	// Output:
	// web 4 db.example.net:5432 map[TZ:UTC]
}

// Marshal writes a Go value out as a document in any of the formats.
func ExampleMarshal() {
	type Database struct {
		Host    string        `ucl:"host"`
		Port    int           `ucl:"port"`
		Timeout time.Duration `ucl:"timeout"`
		Comment string        `ucl:"comment,omitempty"`
	}

	out, err := ucl.Marshal(Database{Host: "db.example.net", Port: 5432, Timeout: time.Minute}, ucl.Config)
	if err != nil {
		panic(err)
	}
	fmt.Print(string(out))
	// Output:
	// "host" = "db.example.net";
	// "port" = 5432;
	// "timeout" = 60.0;
}

// FromInterface builds a document out of plain Go values. Map keys are
// written in sorted order, so the result is stable.
func ExampleFromInterface() {
	obj, err := ucl.FromInterface(map[string]any{
		"database": map[string]any{"host": "db.example.net", "port": 5432},
		"workers":  4,
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(obj.Lookup("database.port").Int())
	fmt.Println(obj.ToString())
	// Output:
	// 5432
	// {"database":{"host":"db.example.net","port":5432},"workers":4}
}

// A field tagged inline has its contents in the enclosing object rather than
// in one named after the field. Encoding, decoding and validation all agree
// on that, so a document this package writes is one it can read back.
func ExampleObject_Decode_inline() {
	type Credentials struct {
		User     string `ucl:"user,required"`
		Password string `ucl:"password"`
	}
	type Config struct {
		Credentials `ucl:",inline"`
		Host        string `ucl:"host"`
	}

	obj, err := ucl.ParseString("user = bob\npassword = s3cret\nhost = db.example.net")
	if err != nil {
		panic(err)
	}

	var cfg Config
	if err := obj.Decode(&cfg); err != nil {
		panic(err)
	}
	fmt.Printf("%s@%s\n", cfg.User, cfg.Host)

	// The same keys are what Validate expects and what Marshal writes.
	fmt.Println(ucl.Validate(obj, Config{}, ucl.RejectUnknownKeys()))
	out, err := ucl.Marshal(cfg, ucl.JSONCompact)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(out))
	// Output:
	// bob@db.example.net
	// <nil>
	// {"user":"bob","password":"s3cret","host":"db.example.net"}
}

// The default= tag option supplies a value for a key the document leaves out.
// A field the caller filled in beforehand keeps what it was given.
func ExampleObject_Decode_defaults() {
	type Config struct {
		Workers int           `ucl:"workers,default=4,min=1,max=64"`
		Timeout time.Duration `ucl:"timeout,default=30s"`
		MaxBody int64         `ucl:"max_body,default=8mb"`
		Mode    string        `ucl:"mode,default=safe,enum=fast|safe"`
	}

	obj, err := ucl.ParseString("workers = 16")
	if err != nil {
		panic(err)
	}

	// Mode is set by the caller, so the declared default does not apply.
	cfg := Config{Mode: "fast"}
	if err := obj.Decode(&cfg); err != nil {
		panic(err)
	}
	fmt.Printf("%+v\n", cfg)
	// Output:
	// {Workers:16 Timeout:30s MaxBody:8388608 Mode:fast}
}
