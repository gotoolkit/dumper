package rsync

import (
	"log"
	"os/exec"
)

var rsyncMode = false

// Options representation rsync option
type Options struct {
	Src       string
	Pull      bool
	Dest      string
	Archive   bool
	ExtraArgs []string
	Rsh       string
}

// Command representation rsync command
type Command struct {
	cmd     string
	options *Options
	Debug   bool
}

// DebugMode enable logging
func DebugMode() {
	rsyncMode = true
}

// NewCommand returns Command new rsync command.
func NewCommand(opt *Options) *Command {
	return &Command{
		cmd:     "rsync",
		options: opt,
	}
}

// Output rsync command
func (c *Command) Output() ([]byte, error) {
	args := c.parseOptions()
	cmd := exec.Command(c.cmd, args...)

	if rsyncMode {
		log.Println(cmd.Args)
	}

	return cmd.CombinedOutput()
}

func (c *Command) parseOptions() []string {
	var options []string
	options = append(options, "-azvh")
	if len(c.options.Rsh) > 0 {
		options = append(options, "-e")
		options = append(options, c.options.Rsh)
	}
	options = append(options, c.options.ExtraArgs...)
	options = append(options, c.options.Src)
	options = append(options, c.options.Dest)
	return options
}
