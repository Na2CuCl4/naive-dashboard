// naive-node is the fixed, stdin-only SSH management endpoint.
package main

import (
	"encoding/json"
	"flag"
	"os"

	"github.com/Na2CuCl4/naive-dashboard/internal/node"
)

func main() {
	configPath := flag.String("config", "/etc/naive-node/config.json", "administrator-owned local configuration")
	flag.Parse()
	response := node.Response{OK: false, Error: "invalid_request"}
	if flag.NArg() == 1 && flag.Arg(0) == "rpc" {
		cfg, err := node.LoadConfig(*configPath)
		if err != nil {
			response.Error = "invalid_node_config"
		} else {
			request, err := node.DecodeRequest(os.Stdin)
			if err == nil {
				response = node.New(cfg).Handle(request)
			}
		}
	}
	// Diagnostics and Caddy output must never leak passwords into the RPC stream.
	_ = json.NewEncoder(os.Stdout).Encode(response)
	if !response.OK {
		os.Exit(1)
	}
}
