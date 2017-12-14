package main

import (
	"fmt"
	"log"
	"os/exec"
)

const (
	MysqlDumpCommand = "mysqldump"
)

func MysqlDumpe() {
	var options []string
	cmd := exec.Command(MysqlDumpCommand, options...)
	stdoutStderr, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s\n", stdoutStderr)
}