package main

import (
	"os"

	"github.com/i500-p2p/cmd"
	log "github.com/sirupsen/logrus"
)

func init() {
	log.SetOutput(os.Stderr)
	log.SetLevel(log.WarnLevel)
	customFormatter := new(log.TextFormatter)
	customFormatter.TimestampFormat = "02-01-2006 15:04:05"
	customFormatter.FullTimestamp = true
	customFormatter.ForceColors = true
	log.SetFormatter(customFormatter)
}

func main() {
	cmd.Execute()
}
