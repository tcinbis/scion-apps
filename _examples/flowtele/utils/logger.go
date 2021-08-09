package utils

import (
	"fmt"
	"github.com/scionproto/scion/go/lib/log"
	"gopkg.in/alecthomas/kingpin.v2"
	"os"
)

func SetupLogger() {
	logCfg := log.Config{Console: log.ConsoleConfig{Level: "debug"}}
	if err := log.Setup(logCfg); err != nil {
		kingpin.Usage()
		fmt.Fprintf(os.Stderr, "Error configuring logger. Exiting due to:%s\n", err)
		os.Exit(-1)
	}
}
