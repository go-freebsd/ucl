package ucl_test

import (
	"fmt"
	"time"

	"github.com/go-freebsd/ucl/pkg/ucl"
)

// A document can also be built in memory. String renders it in configuration
// syntax.
func ExampleObject() {
	cfg := ucl.NewObject()
	cfg.Set("name", ucl.NewString("web"))
	cfg.Set("workers", ucl.NewInt(4))

	hosts := ucl.NewArray()
	hosts.AppendElem(ucl.NewString("a"))
	hosts.AppendElem(ucl.NewString("b"))
	cfg.Set("hosts", hosts)

	db := ucl.NewObject()
	db.Set("port", ucl.NewInt(5432))
	cfg.Set("database", db)

	fmt.Print(cfg)
	// Output:
	// "name" = "web";
	// "workers" = 4;
	// "hosts" [
	//     "a",
	//     "b",
	// ]
	// "database" {
	//     "port" = 5432;
	// }
}

// Every read accessor tolerates a nil receiver, so a missing key needs no
// special case.
func ExampleObject_Get() {
	obj, err := ucl.ParseString("server { port = 8080 }")
	if err != nil {
		panic(err)
	}

	fmt.Println(obj.Get("server").Get("port").Int())
	fmt.Println(obj.Has("server"), obj.Has("client"))

	// "client" is missing, so Get returns nil and the default is used.
	fmt.Println(obj.Get("client").Get("port").IntOr(80))
	// Output:
	// 8080
	// true false
	// 80
}

// Lookup walks a dotted path. A numeric component indexes into an array.
func ExampleObject_Lookup() {
	obj, err := ucl.ParseString(`servers = [ { host = alpha }, { host = beta } ]`)
	if err != nil {
		panic(err)
	}

	fmt.Println(obj.Lookup("servers.1.host").Str())
	fmt.Println(obj.Lookup("servers.9.host") == nil)
	// Output:
	// beta
	// true
}

// Repeating a key builds an implicit array, which is how UCL expresses a
// multi valued option.
func ExampleObject_Values() {
	obj, err := ucl.ParseString("listen = 127.0.0.1\nlisten = ::1\n")
	if err != nil {
		panic(err)
	}
	for _, v := range obj.Values("listen") {
		fmt.Println(v.Str())
	}
	// Output:
	// 127.0.0.1
	// ::1
}

// Each walks an object in document order.
func ExampleObject_Each() {
	obj, err := ucl.ParseString("name = web\nports = [80, 443]\ndatabase { port = 5432 }")
	if err != nil {
		panic(err)
	}
	obj.Each(func(key string, val *ucl.Object) bool {
		fmt.Println(key, val.Type())
		return true
	})
	// Output:
	// name string
	// ports array
	// database object
}

// The type of a value follows from how it is written.
func ExampleObject_Type() {
	obj, err := ucl.ParseString(`
		count   = 4
		ratio   = 0.5
		name    = web
		enabled = yes
		timeout = 30s
		nothing = null
		list    = []
		section = {}
	`)
	if err != nil {
		panic(err)
	}
	for _, key := range obj.Keys() {
		fmt.Println(key, obj.Get(key).Type())
	}
	// Output:
	// count int
	// ratio float
	// name string
	// enabled boolean
	// timeout time
	// nothing null
	// list array
	// section object
}

// A time value converts straight to a time.Duration. A plain number is read
// as a number of seconds.
func ExampleObject_Duration() {
	obj, err := ucl.ParseString("a = 30s\nb = 2min\nc = 1h\nd = 500ms\ne = 90")
	if err != nil {
		panic(err)
	}
	for _, key := range obj.Keys() {
		fmt.Println(key, obj.Get(key).Duration())
	}
	// Output:
	// a 30s
	// b 2m0s
	// c 1h0m0s
	// d 500ms
	// e 1m30s
}

// Interface converts a whole tree into plain Go values.
func ExampleObject_Interface() {
	obj, err := ucl.ParseString("name = web\nworkers = 4\ntimeout = 30s\ndebug = off")
	if err != nil {
		panic(err)
	}
	fmt.Printf("%v\n", obj.Interface())
	// Output:
	// map[debug:false name:web timeout:30s workers:4]
}

// The duplicate strategy decides what a repeated key does.
func ExampleObject_Insert() {
	for _, strategy := range []ucl.Duplicate{
		ucl.DuplicateAppend,
		ucl.DuplicateMerge,
		ucl.DuplicateRewrite,
		ucl.DuplicateError,
	} {
		obj := ucl.NewObject()
		obj.Insert("x", ucl.NewInt(1), strategy)
		err := obj.Insert("x", ucl.NewInt(2), strategy)
		fmt.Printf("%-8s %-6s %v\n", strategy, obj.Get("x").ToString(), err)
	}
	// Output:
	// append   [1,2]  <nil>
	// merge    2      <nil>
	// rewrite  2      <nil>
	// error    1      ucl: duplicate key "x"
}

// Merge layers one document over another, which is how a user file overrides
// the shipped defaults.
func ExampleObject_Merge() {
	defaults, err := ucl.ParseString("workers = 1\nmode = safe\ntimeout = 10s")
	if err != nil {
		panic(err)
	}
	user, err := ucl.ParseString("workers = 8")
	if err != nil {
		panic(err)
	}
	if err := defaults.Merge(user, ucl.DuplicateRewrite); err != nil {
		panic(err)
	}

	fmt.Println(defaults.Get("workers").Int())
	fmt.Println(defaults.Get("mode").Str())
	fmt.Println(defaults.Get("timeout").Duration())
	// Output:
	// 8
	// safe
	// 10s
}

// Objects can be assembled from Go values without going through a document.
func ExampleNewObject() {
	obj := ucl.NewObject()
	obj.Set("timeout", ucl.NewTime(90*time.Second))
	obj.Add("listen", ucl.NewString("127.0.0.1"))
	obj.Add("listen", ucl.NewString("::1")) // Add builds an implicit array

	fmt.Println(obj.Get("timeout").Duration())
	fmt.Println(obj.Get("listen").ToString())
	// Output:
	// 1m30s
	// ["127.0.0.1","::1"]
}
