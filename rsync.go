package main

import (
	"log"
	"os/exec"
)

const (
	RsyncCommand = "rsync"
)

type RsyncOptions struct {
	Src       string
	Pull      bool
	Dest      string
	Archive   bool
	Debug     bool
	ExtraArgs []string
	Rsh       string
}

func Rsync(options *RsyncOptions) error {

	args := parseOptions(options)

	cmd := exec.Command(RsyncCommand, args...)
	if options.Debug {
		log.Println(cmd.Args)
	}
	stdoutStderr, err := cmd.CombinedOutput()
	if err != nil {
		log.Println(string(stdoutStderr))
		return err
	}
	if options.Debug {
		log.Println(string(stdoutStderr))
	}
	return nil
}

func parseOptions(option *RsyncOptions) []string {
	var options []string
	options = append(options, "-azvh")
	if len(option.Rsh) > 0 {
		options = append(options, "-e")
		options = append(options, option.Rsh)
	}
	options = append(options, option.ExtraArgs...)
	options = append(options, option.Src)
	options = append(options, option.Dest)
	return options
}

// func main() {
// 	err := Rsync(&RsyncOptions{
// 		Src:   "/tmp/pull/",
// 		Rsh:   "ssh -p 1023",
// 		Dest:  "root@localhost:/tmp/rsync/test123",
// 		Debug: true,
// 	})
// 	if err != nil {
// 		panic(err)
// 	}
// 	log.Println("done")
// }
