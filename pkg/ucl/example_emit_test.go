package ucl_test

import (
	"fmt"
	"os"

	"github.com/go-freebsd/ucl/pkg/ucl"
)

const emitSource = `name = web
workers = 4
hosts = [alpha, beta]
database { port = 5432 }`

func ExampleObject_Emit() {
	obj, err := ucl.ParseString("section { key = value; list = [1, 2] }")
	if err != nil {
		panic(err)
	}
	out, err := obj.Emit(ucl.JSON)
	if err != nil {
		panic(err)
	}
	fmt.Print(string(out))
	// Output:
	// {
	//     "section": {
	//         "key": "value",
	//         "list": [
	//             1,
	//             2
	//         ]
	//     }
	// }
}

// Config is the syntax pkg.conf and uclcmd use, and the only format that
// parses back as UCL.
func ExampleObject_Emit_config() {
	obj, err := ucl.ParseString(emitSource)
	if err != nil {
		panic(err)
	}
	out, err := obj.Emit(ucl.Config)
	if err != nil {
		panic(err)
	}
	fmt.Print(string(out))
	// Output:
	// "name" = "web";
	// "workers" = 4;
	// "hosts" [
	//     "alpha",
	//     "beta",
	// ]
	// "database" {
	//     "port" = 5432;
	// }
}

// YAML output is block style. It is valid YAML but not a UCL document.
func ExampleObject_Emit_yaml() {
	obj, err := ucl.ParseString(emitSource)
	if err != nil {
		panic(err)
	}
	out, err := obj.Emit(ucl.YAML)
	if err != nil {
		panic(err)
	}
	fmt.Print(string(out))
	// Output:
	// name: web
	// workers: 4
	// hosts:
	//   - alpha
	//   - beta
	// database:
	//   port: 5432
}

// EmitTo writes straight to an io.Writer.
func ExampleObject_EmitTo() {
	obj, err := ucl.ParseString("name = web\nworkers = 4")
	if err != nil {
		panic(err)
	}
	if err := obj.EmitTo(os.Stdout, ucl.JSONCompact); err != nil {
		panic(err)
	}
	fmt.Println()
	// Output:
	// {"name":"web","workers":4}
}

// A document can be carried in msgpack and read back.
func ExampleParseMsgPack() {
	packed, err := ucl.Marshal(map[string]any{"name": "web", "workers": 4}, ucl.MsgPack)
	if err != nil {
		panic(err)
	}

	obj, err := ucl.ParseMsgPack(packed)
	if err != nil {
		panic(err)
	}
	fmt.Println(obj.ToString())
	// Output:
	// {"name":"web","workers":4}
}
